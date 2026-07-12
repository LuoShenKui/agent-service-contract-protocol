package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/billing"
)

// AuditStore is the task-event persistence boundary used by the protocol
// engine. Production implementations should commit audit intent atomically with
// task state and export immutable signed events.
type AuditStore interface {
	Append(chainID, eventType string, actor ascp.EntityRef, data any) (ascp.AuditEvent, error)
	List(chainID string, afterSequence int64) ([]ascp.AuditEvent, error)
	Root(chainID string) (string, error)
}

// ContractSigner signs quotes and receipts and publishes their verification
// keys. A production implementation can delegate signing to KMS or HSM custody.
type ContractSigner interface {
	SignJSON(value any) (ascp.Signature, error)
	JWKS() ascp.JWKSet
}

// Config contains protocol engine dependencies and operational limits.
type Config struct {
	Manifest             ascp.Manifest
	Authenticator        Authenticator
	Authorization        AuthorizationVerifier
	Service              Service
	Store                Store
	Idempotency          IdempotencyBackend
	Audit                AuditStore
	Billing              billing.Processor
	Files                FileStore
	Signer               ContractSigner
	Logger               *slog.Logger
	IdempotencyRetention time.Duration
	MaximumBodyBytes     int64
	Now                  func() time.Time
}

// Server implements the ASCP HTTP binding.
type Server struct {
	config Config
	mux    *http.ServeMux
}

// New validates configuration, applies conservative defaults, and registers
// all protocol routes.
func New(config Config) (*Server, error) {
	if config.Authenticator == nil || config.Authorization == nil || config.Service == nil || config.Store == nil ||
		config.Idempotency == nil || config.Audit == nil || config.Billing == nil || config.Signer == nil {
		return nil, errors.New("authenticator, authorization verifier, service, store, idempotency, audit, billing, and signer are required")
	}
	if strings.TrimSpace(config.Manifest.ServiceID) == "" || strings.TrimSpace(config.Manifest.ServiceName) == "" {
		return nil, errors.New("manifest service_id and service_name are required")
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if config.IdempotencyRetention <= 0 {
		config.IdempotencyRetention = 24 * time.Hour
	}
	if config.MaximumBodyBytes <= 0 {
		config.MaximumBodyBytes = 1 << 20 // One MiB; larger payloads should use scoped references.
	}
	if config.Now == nil {
		config.Now = func() time.Time { return time.Now().UTC() }
	}
	if config.Files == nil {
		config.Files = NewMemoryFileStore(10 << 20)
	}
	if len(config.Manifest.Versions) == 0 {
		config.Manifest.Versions = []string{ascp.ProtocolVersion}
	}
	if config.Manifest.Protocol == "" {
		config.Manifest.Protocol = ascp.ProtocolName
	}
	if config.Manifest.JWKSURI == "" {
		config.Manifest.JWKSURI = "/.well-known/jwks.json"
	}
	if config.Manifest.CapabilitiesURI == "" {
		config.Manifest.CapabilitiesURI = "/.well-known/ascp/capabilities"
	}
	if config.Manifest.OptionsURI == "" {
		config.Manifest.OptionsURI = "/v1/options"
	}
	if config.Manifest.InvokeURI == "" {
		config.Manifest.InvokeURI = "/v1/invoke"
	}
	if config.Manifest.FilesURI == "" {
		config.Manifest.FilesURI = "/v1/files"
	}
	if config.Manifest.Features == nil {
		config.Manifest.Features = map[string]bool{}
	}
	config.Manifest.Features["three_phase_contract"] = true
	config.Manifest.Features["direct_invocation"] = true
	config.Manifest.Features["optional_options_preflight"] = true
	config.Manifest.Features["compact_capability_catalog"] = true
	config.Manifest.Features["scoped_file_transfer"] = true
	config.Manifest.Features["mandatory_idempotency"] = true
	config.Manifest.Features["signed_quotes"] = true
	config.Manifest.Features["signed_audit_chain"] = true
	config.Manifest.Features["billing_abstraction"] = true
	for _, optionalFeature := range []string{"asynchronous_execution", "scheduled_execution", "callback_delivery"} {
		if _, declared := config.Manifest.Features[optionalFeature]; !declared {
			config.Manifest.Features[optionalFeature] = false
		}
	}

	server := &Server{config: config, mux: http.NewServeMux()}
	server.registerRoutes()
	return server, nil
}

// ServeHTTP adds baseline security headers and delegates to the protocol mux.
func (s *Server) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("X-Content-Type-Options", "nosniff")
	writer.Header().Set("Cache-Control", "no-store")
	writer.Header().Set("ASCP-Version", ascp.ProtocolVersion)
	s.mux.ServeHTTP(writer, request)
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET /.well-known/ascp", s.handleManifest)
	s.mux.HandleFunc("GET /.well-known/ascp/capabilities", s.handleCapabilities)
	s.mux.HandleFunc("GET /.well-known/jwks.json", s.handleJWKS)
	s.mux.HandleFunc("GET /healthz", s.handleHealth)
	s.mux.HandleFunc("OPTIONS /v1/invoke", s.handleInvokeOptions)
	s.mux.HandleFunc("POST /v1/options", s.handleOptions)
	s.mux.HandleFunc("POST /v1/invoke", s.handleInvoke)
	s.mux.HandleFunc("POST /v1/negotiate", s.handleNegotiate)
	s.mux.HandleFunc("POST /v1/prepare", s.handlePrepare)
	s.mux.HandleFunc("POST /v1/commit", s.handleCommit)
	s.mux.HandleFunc("/v1/tasks/", s.handleTaskRoutes)
	s.mux.HandleFunc("/v1/invocations/", s.handleInvocationRoutes)
	s.mux.HandleFunc("POST /v1/files/prepare-upload", s.handlePrepareUpload)
	s.mux.HandleFunc("/v1/files/", s.handleFileRoutes)
}

func (s *Server) handleManifest(writer http.ResponseWriter, _ *http.Request) {
	manifest := s.config.Manifest
	manifest.GeneratedAt = s.config.Now()
	manifest.ManifestExpiresAt = manifest.GeneratedAt.Add(15 * time.Minute)
	writeJSON(writer, http.StatusOK, manifest, http.Header{
		"Cache-Control": []string{"public, max-age=60, must-revalidate"},
	})
}

func (s *Server) handleJWKS(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, s.config.Signer.JWKS(), http.Header{
		"Cache-Control": []string{"public, max-age=300, must-revalidate"},
	})
}

func (s *Server) handleHealth(writer http.ResponseWriter, _ *http.Request) {
	writeJSON(writer, http.StatusOK, map[string]any{
		"status":   "ok",
		"protocol": ascp.ProtocolVersion,
		"time":     s.config.Now(),
	}, nil)
}

// operationResult assembles an HTTP response before it is written, allowing the
// exact bytes and headers to be retained for idempotent replay.
type operationResult struct {
	Status          int
	Headers         http.Header
	Body            []byte
	TaskID          string
	TaskState       ascp.TaskState
	InvocationID    string
	InvocationState ascp.InvocationState
	ReleaseClaim    bool
}

type operation func(context.Context, Identity, string, []byte, string) operationResult

func (s *Server) handleMutating(writer http.ResponseWriter, request *http.Request, operation operation) {
	requestID := request.Header.Get("X-Request-ID")
	if requestID == "" || len(requestID) > 128 {
		requestID = ascp.MustNewID("req")
	}

	if suppliedVersion := request.Header.Get("ASCP-Version"); suppliedVersion != "" && suppliedVersion != ascp.ProtocolVersion {
		writeProblem(writer, requestID, problem(http.StatusBadRequest, "unsupported_version", "Unsupported ASCP version", "The requested protocol version is not supported.", false))
		return
	}

	identity, err := s.config.Authenticator.Authenticate(request)
	if err != nil {
		writer.Header().Set("WWW-Authenticate", `Bearer realm="ascp"`)
		writeProblem(writer, requestID, problem(http.StatusUnauthorized, ascp.ErrUnauthenticated, "Authentication required", "The supplied access token is missing, invalid, expired, or not authorized for this service.", false))
		return
	}

	if request.Body != nil && (request.ContentLength != 0 || request.Header.Get("Transfer-Encoding") != "") {
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
		if err != nil || (mediaType != ascp.MediaType && mediaType != "application/json") {
			writeProblem(writer, requestID, problem(http.StatusUnsupportedMediaType, ascp.ErrUnsupportedMediaType, "Unsupported media type", "Use application/ascp+json or application/json for ASCP mutation bodies.", false))
			return
		}
	}

	key := request.Header.Get("Idempotency-Key")
	if !validIdempotencyKey(key) {
		p := problem(http.StatusBadRequest, ascp.ErrInvalidRequest, "Invalid Idempotency-Key", "Mutating ASCP requests require an opaque visible-ASCII Idempotency-Key between 16 and 255 bytes, without whitespace.", false)
		p.FieldErrors = []ascp.FieldError{{Pointer: "/headers/Idempotency-Key", Code: "invalid_format", Message: "must contain 16 to 255 visible ASCII bytes and no whitespace"}}
		writeProblem(writer, requestID, p)
		return
	}

	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, s.config.MaximumBodyBytes))
	if err != nil {
		writeProblem(writer, requestID, problem(http.StatusRequestEntityTooLarge, ascp.ErrInvalidRequest, "Request body too large", "The request exceeds the service's published maximum body size.", false))
		return
	}

	scope := identity.Actor.Type + "/" + identity.Actor.ID + "|" + identity.Principal.Type + "/" + identity.Principal.ID + "|" + request.Method + "|" + request.URL.Path
	digest := ascp.SHA256Digest(append([]byte(request.Method+"\n"+request.URL.Path+"\n"), body...))
	state, replay, backendErr := s.config.Idempotency.Begin(scope, key, digest, s.config.IdempotencyRetention)
	if backendErr != nil {
		s.config.Logger.Error("idempotency claim failed", "request_id", requestID, "error", backendErr)
		p := problem(http.StatusServiceUnavailable, ascp.ErrServiceUnavailable, "Idempotency service unavailable", "The service could not durably determine whether this mutation may begin. Retry with the same idempotency key.", true)
		p.RetryAfterMS = 1000
		writeProblem(writer, requestID, p)
		return
	}

	switch state {
	case IdempotencyReplay:
		for name, values := range replay.Headers {
			for _, value := range values {
				writer.Header().Add(name, value)
			}
		}
		writer.Header().Set("Idempotency-Replayed", "true")
		writer.Header().Set("X-Request-ID", requestID)
		writer.WriteHeader(replay.StatusCode)
		_, _ = writer.Write(replay.Body)
		return
	case IdempotencyConflict:
		writeProblem(writer, requestID, problem(http.StatusConflict, ascp.ErrIdempotencyConflict, "Idempotency conflict", "The key was already used with a different request payload.", false))
		return
	case IdempotencyInProgress:
		p := problem(http.StatusConflict, ascp.ErrRequestInProgress, "Request is already in progress", "Retry the same request after the indicated delay.", true)
		p.RetryAfterMS = 1000
		writer.Header().Set("Retry-After", "1")
		writeProblem(writer, requestID, p)
		return
	}

	completed := false
	defer func() {
		if recovered := recover(); recovered != nil {
			// Do not release the idempotency claim here. A panic may occur after an
			// external effect or billing operation, and releasing the key would permit
			// a retry to create a second effect. Durable deployments reconcile stale
			// in-progress claims and then complete the original record explicitly.
			s.config.Logger.Error("ASCP handler panic", "request_id", requestID, "panic", recovered)
			if !completed {
				writeProblem(writer, requestID, problem(http.StatusInternalServerError, ascp.ErrOutcomeUnknown, "Operation outcome is unknown", "The request stopped unexpectedly. Retry only with the same idempotency key while the provider reconciles the original operation.", false))
			}
		}
	}()

	result := operation(request.Context(), identity, requestID, body, key)
	if result.Headers == nil {
		result.Headers = make(http.Header)
	}
	result.Headers.Set("ASCP-Version", ascp.ProtocolVersion)
	result.Headers.Set("X-Request-ID", requestID)
	if result.Headers.Get("Content-Type") == "" {
		result.Headers.Set("Content-Type", ascp.MediaType)
	}

	// A retryable response may release its claim only when the operation has
	// positively established that no contract, billing record, task, or provider side
	// effect crossed the execution boundary. This prevents a temporary upstream
	// outage from poisoning the key for the full retention period while keeping
	// ambiguous and post-effect outcomes locked for reconciliation.
	if result.ReleaseClaim {
		if err := s.config.Idempotency.Release(scope, key, digest); err != nil {
			s.config.Logger.Error("idempotency release requires reconciliation", "request_id", requestID, "error", err)
			p := problem(http.StatusInternalServerError, ascp.ErrOutcomeUnknown, "Idempotency release is uncertain", "The operation reported a pre-effect retryable failure, but the service could not durably release the request claim. Retry only with the same idempotency key while the provider reconciles it.", false)
			writeProblem(writer, requestID, p)
			return
		}
		completed = true
		result.Headers.Set("Idempotency-Released", "true")
		for name, values := range result.Headers {
			for _, value := range values {
				writer.Header().Add(name, value)
			}
		}
		writer.WriteHeader(result.Status)
		_, _ = writer.Write(result.Body)
		return
	}

	result.Headers.Set("Idempotency-Retention-Until", s.config.Now().Add(s.config.IdempotencyRetention).Format(time.RFC3339))
	stored := StoredResponse{
		StatusCode: result.Status,
		Headers:    result.Headers,
		Body:       result.Body,
		ExpiresAt:  s.config.Now().Add(s.config.IdempotencyRetention),
	}
	if err := s.config.Idempotency.Complete(scope, key, stored); err != nil {
		s.config.Logger.Error("idempotency completion requires reconciliation", "request_id", requestID, "task_id", result.TaskID, "error", err)
		p := problem(http.StatusInternalServerError, ascp.ErrOutcomeUnknown, "Response durability is unknown", "The operation finished, but the service could not durably store its replayable response. Retry only with the same idempotency key while the provider reconciles the original request.", false)
		p.TaskID = result.TaskID
		p.TaskState = result.TaskState
		writeProblem(writer, requestID, p)
		return
	}
	completed = true

	for name, values := range result.Headers {
		for _, value := range values {
			writer.Header().Add(name, value)
		}
	}
	writer.WriteHeader(result.Status)
	_, _ = writer.Write(result.Body)
}

func (s *Server) handleNegotiate(writer http.ResponseWriter, request *http.Request) {
	s.handleMutating(writer, request, s.negotiate)
}

func (s *Server) negotiate(ctx context.Context, identity Identity, requestID string, body []byte, _ string) operationResult {
	var input ascp.NegotiationRequest
	if p := decodeStrict(body, &input); p != nil {
		return problemResult(requestID, *p)
	}
	if input.Goal == "" {
		p := problem(http.StatusUnprocessableEntity, ascp.ErrValidationFailed, "Validation failed", "The goal field is required.", false)
		p.FieldErrors = []ascp.FieldError{{Pointer: "/goal", Code: "required", Message: "goal is required"}}
		return problemResult(requestID, p)
	}
	if input.Budget != nil {
		if err := ascp.ValidateMoney(*input.Budget); err != nil {
			p := problem(http.StatusUnprocessableEntity, ascp.ErrValidationFailed, "Invalid budget", "The budget must use an exact non-negative decimal amount and a valid currency code.", false)
			p.FieldErrors = []ascp.FieldError{{Pointer: "/budget", Code: "invalid_money", Message: err.Error()}}
			return problemResult(requestID, p)
		}
	}
	if input.Actor != identity.Actor || input.Principal != identity.Principal {
		return problemResult(requestID, problem(http.StatusForbidden, ascp.ErrForbidden, "Identity mismatch", "The actor and principal must match the authenticated delegation.", false))
	}
	if p := s.validateReferences(identity, input.ContextRefs, input.InputFiles); p != nil {
		return problemResult(requestID, *p)
	}

	offer, p := s.config.Service.Negotiate(ctx, identity, input)
	if p != nil {
		return problemResult(requestID, *p)
	}
	if offer.EstimatedPrice != nil {
		if err := validatePriceEstimate(*offer.EstimatedPrice); err != nil {
			s.config.Logger.Error("service returned invalid price estimate", "request_id", requestID, "error", err)
			return problemResult(requestID, problem(http.StatusInternalServerError, ascp.ErrInternal, "Invalid service estimate", "The service produced an invalid non-binding price estimate.", false))
		}
	}

	negotiationID := ascp.MustNewID("neg")
	response := ascp.NegotiationResponse{
		NegotiationID:     negotiationID,
		Supported:         offer.Supported,
		Conditional:       offer.Conditional,
		ReasonCode:        offer.ReasonCode,
		Reason:            offer.Reason,
		ResolvedIntent:    offer.ResolvedIntent,
		Parameters:        offer.Parameters,
		RequiredScopes:    offer.RequiredScopes,
		BillingOptions:    offer.BillingOptions,
		InputFilePolicy:   offer.InputFilePolicy,
		EstimatedPrice:    offer.EstimatedPrice,
		OfferExpiresAt:    offer.ExpiresAt,
		SchemaVersion:     offer.SchemaVersion,
		ServerRequestID:   requestID,
		IdempotencyExpiry: s.config.Now().Add(s.config.IdempotencyRetention),
	}

	if offer.Supported {
		if offer.ExpiresAt.IsZero() || !offer.ExpiresAt.After(s.config.Now()) {
			return problemResult(requestID, problem(http.StatusInternalServerError, ascp.ErrInternal, "Invalid service offer", "The service returned an already-expired capability offer.", false))
		}
		for _, scope := range offer.RequiredScopes {
			if !identity.HasScope(scope) {
				return problemResult(requestID, problem(http.StatusForbidden, ascp.ErrForbidden, "Missing delegated scope", "The authenticated agent lacks required scope: "+scope, false))
			}
		}

		response.OfferID = ascp.MustNewID("off")
		if err := s.config.Store.PutOffer(StoredOffer{
			OfferID:        response.OfferID,
			NegotiationID:  negotiationID,
			Actor:          identity.Actor,
			Principal:      identity.Principal,
			ResolvedIntent: offer.ResolvedIntent,
			SchemaVersion:  offer.SchemaVersion,
			RequiredScopes: offer.RequiredScopes,
			Budget:         input.Budget,
			ExpiresAt:      offer.ExpiresAt,
		}); err != nil {
			s.config.Logger.Error("offer persistence failed", "request_id", requestID, "offer_id", response.OfferID, "error", err)
			return problemResult(requestID, problem(http.StatusServiceUnavailable, ascp.ErrServiceUnavailable, "Offer persistence unavailable", "The service could not durably store the negotiated capability offer.", true))
		}
	}

	s.appendAudit(negotiationID, "ascp.negotiation.completed", identity.Actor, map[string]any{
		"supported":       response.Supported,
		"offer_id":        response.OfferID,
		"resolved_intent": response.ResolvedIntent,
	})
	return jsonResult(http.StatusOK, response)
}

func (s *Server) handlePrepare(writer http.ResponseWriter, request *http.Request) {
	s.handleMutating(writer, request, s.prepare)
}

func (s *Server) prepare(ctx context.Context, identity Identity, requestID string, body []byte, _ string) operationResult {
	var input ascp.PrepareRequest
	if p := decodeStrict(body, &input); p != nil {
		return problemResult(requestID, *p)
	}
	if input.OfferID == "" || input.SchemaVersion == "" {
		p := problem(http.StatusUnprocessableEntity, ascp.ErrValidationFailed, "Validation failed", "offer_id and schema_version are required.", false)
		return problemResult(requestID, p)
	}

	offer, ok, storeErr := s.config.Store.GetOffer(input.OfferID)
	if storeErr != nil {
		s.config.Logger.Error("offer lookup failed", "request_id", requestID, "offer_id", input.OfferID, "error", storeErr)
		return problemResult(requestID, problem(http.StatusServiceUnavailable, ascp.ErrServiceUnavailable, "Offer store unavailable", "The service could not determine the negotiated offer state.", true))
	}
	if !ok {
		return problemResult(requestID, problem(http.StatusNotFound, ascp.ErrNotFound, "Offer not found", "The capability offer does not exist.", false))
	}
	if offer.Actor != identity.Actor || offer.Principal != identity.Principal {
		return problemResult(requestID, problem(http.StatusForbidden, ascp.ErrForbidden, "Offer ownership mismatch", "The offer belongs to a different delegation.", false))
	}
	if s.config.Now().After(offer.ExpiresAt) {
		return problemResult(requestID, problem(http.StatusConflict, ascp.ErrOfferExpired, "Offer expired", "Negotiate the capability again.", false))
	}
	if input.SchemaVersion != offer.SchemaVersion {
		return problemResult(requestID, problem(http.StatusConflict, "schema_version_mismatch", "Schema version mismatch", "The prepare request does not match the negotiated parameter contract.", false))
	}
	now := s.config.Now()
	if input.Execution.Deadline != nil && !input.Execution.Deadline.After(now) {
		return problemResult(requestID, problem(http.StatusConflict, ascp.ErrDeadlineExpired, "Execution deadline expired", "The requested task deadline is already in the past.", false))
	}
	if input.Execution.NotBefore != nil {
		if input.Execution.Deadline != nil && input.Execution.NotBefore.After(*input.Execution.Deadline) {
			return problemResult(requestID, problem(http.StatusUnprocessableEntity, ascp.ErrValidationFailed, "Invalid execution window", "not_before must not be later than deadline.", false))
		}
		if input.Execution.NotBefore.After(now.Add(time.Second)) {
			return problemResult(requestID, problem(http.StatusUnprocessableEntity, ascp.ErrSchedulingUnsupported, "Scheduling is not enabled", "This reference service accepts immediate execution only.", false))
		}
	}
	if input.Callback != nil && !s.config.Manifest.Features["callback_delivery"] {
		return problemResult(requestID, problem(http.StatusUnprocessableEntity, ascp.ErrCallbackUnsupported, "Callback delivery is not enabled", "This service does not accept callback destinations. Use task polling or the signed event endpoint.", false))
	}
	if p := s.validateReferences(identity, input.ContextRefs, input.InputFiles); p != nil {
		return problemResult(requestID, *p)
	}

	contract, p := s.config.Service.Prepare(ctx, identity, offer, input)
	if p != nil {
		return problemResult(requestID, *p)
	}
	if contract.ExpiresAt.IsZero() || !contract.ExpiresAt.After(s.config.Now()) {
		return problemResult(requestID, problem(http.StatusInternalServerError, ascp.ErrInternal, "Invalid service quote", "The service returned an already-expired quote.", false))
	}
	if err := validatePreparedContract(contract); err != nil {
		s.config.Logger.Error("service returned invalid prepared contract", "request_id", requestID, "error", err)
		return problemResult(requestID, problem(http.StatusInternalServerError, ascp.ErrInternal, "Invalid service quote", "The service produced contract terms that violate ASCP invariants.", false))
	}
	for _, limit := range []*ascp.Money{offer.Budget, input.Execution.MaxPrice} {
		if limit == nil {
			continue
		}
		comparison, err := ascp.CompareMoney(*limit, contract.PriceCeiling)
		if err != nil {
			return problemResult(requestID, problem(http.StatusUnprocessableEntity, ascp.ErrValidationFailed, "Invalid budget", err.Error(), false))
		}
		if comparison < 0 {
			return problemResult(requestID, problem(http.StatusConflict, ascp.ErrBudgetExceeded, "Quote exceeds budget", "The binding price ceiling exceeds the client's authorized budget.", false))
		}
	}

	quote := ascp.Quote{
		ProtocolVersion: ascp.ProtocolVersion,
		ServiceID:       s.config.Manifest.ServiceID,
		QuoteID:         ascp.MustNewID("quo"),
		OfferID:         offer.OfferID,
		Intent:          offer.ResolvedIntent,
		Principal:       identity.Principal,
		Actor:           identity.Actor,
		NormalizedTask:  contract.NormalizedTask,
		ContextRefs:     append([]ascp.ContextRef(nil), input.ContextRefs...),
		InputFiles:      append([]ascp.FileRef(nil), input.InputFiles...),
		Callback:        input.Callback,
		Price:           contract.Price,
		PriceCeiling:    contract.PriceCeiling,
		PriceBreakdown:  contract.PriceBreakdown,
		BillingTerms:    contract.BillingTerms,
		Effects:         contract.Effects,
		Permissions:     contract.Permissions,
		DataUse:         contract.DataUse,
		RiskClass:       contract.RiskClass,
		Confirmation:    contract.Confirmation,
		SLA:             contract.SLA,
		Execution:       input.Execution,
		IssuedAt:        s.config.Now(),
		ExpiresAt:       contract.ExpiresAt,
		RevocableUntil:  contract.RevocableUntil,
	}
	if err := ascp.ValidateQuoteSemantics(quote); err != nil {
		s.config.Logger.Error("constructed quote violates protocol semantics", "request_id", requestID, "error", err)
		return problemResult(requestID, problem(http.StatusInternalServerError, ascp.ErrInternal, "Invalid service quote", "The service produced a quote that violates ASCP semantic invariants.", false))
	}
	unsigned, err := ascp.SigningProjection(quote)
	if err != nil {
		s.config.Logger.Error("quote projection failed", "request_id", requestID, "error", err)
		return problemResult(requestID, problem(http.StatusInternalServerError, ascp.ErrInternal, "Quote signing failed", "The service could not create the signed contract projection.", true))
	}
	signature, err := s.config.Signer.SignJSON(unsigned)
	if err != nil {
		s.config.Logger.Error("quote signing failed", "request_id", requestID, "error", err)
		return problemResult(requestID, problem(http.StatusInternalServerError, ascp.ErrInternal, "Quote signing failed", "The service could not sign the contract.", true))
	}
	quote.Signature = signature
	if err := s.config.Store.PutQuote(quote); err != nil {
		s.config.Logger.Error("quote persistence failed", "request_id", requestID, "quote_id", quote.QuoteID, "error", err)
		return problemResult(requestID, problem(http.StatusInternalServerError, ascp.ErrInternal, "Quote persistence failed", "The service could not persist the signed contract.", true))
	}
	s.appendAudit(quote.QuoteID, "ascp.quote.prepared", identity.Actor, map[string]any{
		"quote_id": quote.QuoteID,
		"digest":   quote.Signature.PayloadDigest,
		"price":    quote.Price,
		"expires":  quote.ExpiresAt,
	})
	return jsonResult(http.StatusCreated, quote)
}

func (s *Server) handleCommit(writer http.ResponseWriter, request *http.Request) {
	s.handleMutating(writer, request, s.commit)
}

func (s *Server) commit(ctx context.Context, identity Identity, requestID string, body []byte, idempotencyKey string) operationResult {
	var input ascp.CommitRequest
	if p := decodeStrict(body, &input); p != nil {
		return problemResult(requestID, *p)
	}
	quote, ok, storeErr := s.config.Store.GetQuote(input.QuoteID)
	if storeErr != nil {
		s.config.Logger.Error("quote lookup failed", "request_id", requestID, "quote_id", input.QuoteID, "error", storeErr)
		return problemResult(requestID, problem(http.StatusServiceUnavailable, ascp.ErrServiceUnavailable, "Quote store unavailable", "The service could not determine the signed quote state.", true))
	}
	if !ok {
		return problemResult(requestID, problem(http.StatusNotFound, ascp.ErrNotFound, "Quote not found", "The signed quote does not exist.", false))
	}
	if quote.Actor != identity.Actor || quote.Principal != identity.Principal {
		return problemResult(requestID, problem(http.StatusForbidden, ascp.ErrForbidden, "Quote ownership mismatch", "The quote belongs to a different delegation.", false))
	}
	if s.config.Now().After(quote.ExpiresAt) {
		return problemResult(requestID, problem(http.StatusConflict, ascp.ErrQuoteExpired, "Quote expired", "Prepare a new quote before committing.", false))
	}
	if quote.Execution.Deadline != nil && !quote.Execution.Deadline.After(s.config.Now()) {
		return problemResult(requestID, problem(http.StatusConflict, ascp.ErrDeadlineExpired, "Execution deadline expired", "Prepare a new quote with a valid execution deadline.", false))
	}
	if input.QuoteDigest != quote.Signature.PayloadDigest {
		return problemResult(requestID, problem(http.StatusConflict, ascp.ErrQuoteMismatch, "Quote digest mismatch", "The commit is not bound to the stored signed quote.", false))
	}
	if p := validateAuthorization(input.Authorization, quote, identity, s.config.Now()); p != nil {
		return problemResult(requestID, *p)
	}
	if p := s.config.Authorization.Verify(ctx, identity, quote, input.Authorization); p != nil {
		if p.Retryable {
			return safeRetryProblemResult(requestID, *p)
		}
		return problemResult(requestID, *p)
	}
	for _, permission := range quote.Permissions {
		if !identity.HasScope(permission.Scope) {
			return problemResult(requestID, problem(http.StatusForbidden, ascp.ErrForbidden, "Delegated scope is no longer available", "The active delegation no longer includes required scope: "+permission.Scope, false))
		}
	}

	billingRecord, billingValidationProblem := s.reserveBilling(
		ctx,
		identity,
		quote.QuoteID,
		quote.Signature.PayloadDigest,
		quote.BillingTerms,
		input.BillingAuthorization,
		quote.PriceCeiling,
		idempotencyKey+":reserve",
	)
	if billingValidationProblem != nil {
		// A temporary adapter error is safe to retry only when the adapter's
		// contract guarantees that no reservation or accounting record was made.
		if billingValidationProblem.Retryable {
			return safeRetryProblemResult(requestID, *billingValidationProblem)
		}
		return problemResult(requestID, *billingValidationProblem)
	}

	now := s.config.Now()
	task := ascp.Task{
		TaskID:       ascp.MustNewID("tsk"),
		ClientTaskID: input.ClientTaskID,
		QuoteID:      quote.QuoteID,
		State:        ascp.TaskAccepted,
		CreatedAt:    now,
		UpdatedAt:    now,
		Billing:      billingRecord,
		Version:      1,
	}
	if err := s.config.Store.PutTask(task); err != nil {
		s.config.Logger.Error("task persistence failed", "request_id", requestID, "task_id", task.TaskID, "error", err)
		if billingRecord != nil && billingRecord.ReservationRef != "" {
			if voidErr := s.config.Billing.Release(ctx, billingRecord.ReservationRef, idempotencyKey+":void"); voidErr != nil {
				s.config.Logger.Error("orphaned billing reservation requires reconciliation", "request_id", requestID, "reservation_ref", billingRecord.ReservationRef, "error", voidErr)
			}
		}
		p := problem(http.StatusInternalServerError, ascp.ErrOutcomeUnknown, "Task persistence outcome is unknown", "A billing reservation or task persistence may have partially completed. Retry only with the same idempotency key while the provider reconciles the original commit.", false)
		p.TaskID = task.TaskID
		p.TaskState = ascp.TaskAccepted
		return problemResult(requestID, p)
	}
	s.appendAudit(task.TaskID, "ascp.task.accepted", identity.Actor, map[string]any{
		"task_id":  task.TaskID,
		"quote_id": quote.QuoteID,
	})
	if billingRecord != nil {
		s.appendAudit(task.TaskID, "ascp.billing.reserved", identity.Actor, map[string]any{
			"mode":            billingRecord.Mode,
			"reservation_ref": billingRecord.ReservationRef,
			"amount":          billingRecord.Amount,
			"state":           billingRecord.State,
		})
	}

	startedAt := s.config.Now()
	var err error
	task, err = s.config.Store.UpdateTask(task.TaskID, func(current *ascp.Task) error {
		current.State = ascp.TaskRunning
		current.StartedAt = &startedAt
		current.UpdatedAt = startedAt
		current.Progress = 1
		return nil
	})
	if err != nil {
		s.config.Logger.Error("task start transition requires reconciliation", "request_id", requestID, "task_id", task.TaskID, "error", err)
		if billingRecord != nil && billingRecord.ReservationRef != "" {
			if voidErr := s.config.Billing.Release(ctx, billingRecord.ReservationRef, idempotencyKey+":void-start-failed"); voidErr != nil {
				s.config.Logger.Error("billing reservation could not be released after task start failure", "request_id", requestID, "task_id", task.TaskID, "reservation_ref", billingRecord.ReservationRef, "error", voidErr)
			}
		}
		return taskOutcomeUnknownResult(requestID, task, "Task start outcome requires reconciliation", "The task was accepted, but the service could not durably record its start transition. Query the returned task ID; do not create a new commit.")
	}
	s.appendAudit(task.TaskID, "ascp.task.running", identity.Actor, map[string]any{"task_id": task.TaskID})

	result, executionProblem := s.config.Service.Execute(ctx, identity, task, quote)
	if executionProblem != nil {
		voided := billingRecord == nil || billingRecord.ReservationRef == ""
		if billingRecord != nil && billingRecord.ReservationRef != "" {
			if voidErr := s.config.Billing.Release(ctx, billingRecord.ReservationRef, idempotencyKey+":void"); voidErr != nil {
				s.config.Logger.Error("billing reservation requires reconciliation after provider failure", "request_id", requestID, "task_id", task.TaskID, "reservation_ref", billingRecord.ReservationRef, "error", voidErr)
				updatedAt := s.config.Now()
				task, err = s.config.Store.UpdateTask(task.TaskID, func(current *ascp.Task) error {
					current.State = ascp.TaskWaitingInput
					current.StatusReason = "billing_release_pending"
					current.UpdatedAt = updatedAt
					current.Progress = 99
					return nil
				})
				if err != nil {
					return taskOutcomeUnknownResult(requestID, task, "Task and billing outcome require reconciliation", "The provider reported failure, but the billing reservation could not be released and the pending state could not be durably recorded. Query the task ID; do not create a new commit.")
				}
				s.appendAudit(task.TaskID, "ascp.billing.void_pending", identity.Actor, map[string]any{"reservation_ref": billingRecord.ReservationRef, "provider_error": executionProblem.Code})
				return jsonResult(http.StatusAccepted, ascp.CommitResponse{
					Task:              task,
					AcceptedAt:        now,
					IdempotencyExpiry: s.config.Now().Add(s.config.IdempotencyRetention),
				})
			}
			voided = true
		}
		completedAt := s.config.Now()
		task, err = s.config.Store.UpdateTask(task.TaskID, func(current *ascp.Task) error {
			current.State = ascp.TaskFailed
			current.StatusReason = executionProblem.Code
			current.UpdatedAt = completedAt
			current.CompletedAt = &completedAt
			current.Progress = 100
			if current.Billing != nil && voided {
				current.Billing.State = "voided"
			}
			return nil
		})
		if err != nil {
			s.config.Logger.Error("provider failure transition requires reconciliation", "request_id", requestID, "task_id", task.TaskID, "error", err)
			return taskOutcomeUnknownResult(requestID, task, "Task failure outcome requires reconciliation", "The provider reported failure, but the service could not durably record the final task and billing state. Query the task ID; do not create a new commit.")
		}
		s.appendAudit(task.TaskID, "ascp.task.failed", identity.Actor, map[string]any{
			"code": executionProblem.Code,
		})
		task, err = s.attachReceipt(task, quote, identity.Actor, nil, nil)
		if err != nil {
			s.config.Logger.Error("failed task receipt requires reconciliation", "request_id", requestID, "task_id", task.TaskID, "error", err)
			task.StatusReason = "receipt_pending"
			return jsonResult(http.StatusAccepted, ascp.CommitResponse{
				Task:              task,
				AcceptedAt:        now,
				IdempotencyExpiry: s.config.Now().Add(s.config.IdempotencyRetention),
			})
		}
		return jsonResult(http.StatusCreated, ascp.CommitResponse{
			Task:              task,
			AcceptedAt:        now,
			IdempotencyExpiry: s.config.Now().Add(s.config.IdempotencyRetention),
		})
	}

	finalPrice, finalBreakdown, finalPriceErr := resolveFinalCharge(quote, result)
	if finalPriceErr != nil {
		voided := false
		if billingRecord != nil && billingRecord.ReservationRef != "" {
			if voidErr := s.config.Billing.Release(ctx, billingRecord.ReservationRef, idempotencyKey+":void-invalid-final-price"); voidErr != nil {
				s.config.Logger.Error("invalid final price left billing reservation for reconciliation", "request_id", requestID, "task_id", task.TaskID, "reservation_ref", billingRecord.ReservationRef, "error", voidErr)
			} else {
				voided = true
			}
		}
		updatedAt := s.config.Now()
		task, err = s.config.Store.UpdateTask(task.TaskID, func(current *ascp.Task) error {
			current.State = ascp.TaskDisputed
			current.StatusReason = "invalid_final_price"
			current.UpdatedAt = updatedAt
			current.Artifacts = result.Artifacts
			current.Progress = 100
			if current.Billing != nil && voided {
				current.Billing.State = "voided"
			}
			return nil
		})
		if err != nil {
			s.config.Logger.Error("invalid final price and task state require reconciliation", "request_id", requestID, "task_id", task.TaskID, "price_error", finalPriceErr, "store_error", err)
			return taskOutcomeUnknownResult(requestID, task, "Billing outcome requires reconciliation", "The provider effect completed, but the reported final price violated the signed contract and the dispute state could not be durably recorded.")
		}
		s.config.Logger.Error("provider reported final price outside signed contract", "request_id", requestID, "task_id", task.TaskID, "error", finalPriceErr)
		s.appendAudit(task.TaskID, "ascp.billing.disputed", identity.Actor, map[string]any{"reason": "invalid_final_price"})
		return jsonResult(http.StatusAccepted, ascp.CommitResponse{
			Task:              task,
			AcceptedAt:        now,
			IdempotencyExpiry: s.config.Now().Add(s.config.IdempotencyRetention),
		})
	}

	if billingRecord != nil && billingRecord.ReservationRef != "" {
		settlement, settlementErr := s.config.Billing.Settle(ctx, billingRecord.ReservationRef, finalPrice, idempotencyKey+":settlement")
		if settlementErr != nil {
			// The external side effect may already have occurred. The protocol does
			// not lie about success; it exposes a recoverable billing-pending state.
			updatedAt := s.config.Now()
			task, err = s.config.Store.UpdateTask(task.TaskID, func(current *ascp.Task) error {
				current.State = ascp.TaskWaitingInput
				current.StatusReason = ascp.ErrBillingUnavailable
				current.UpdatedAt = updatedAt
				current.Artifacts = result.Artifacts
				current.Progress = 99
				return nil
			})
			if err != nil {
				s.config.Logger.Error("billing settlement and task state both require reconciliation", "request_id", requestID, "task_id", task.TaskID, "settlement_error", settlementErr, "store_error", err)
				return taskOutcomeUnknownResult(requestID, task, "Billing outcome requires reconciliation", "The provider effect completed, but billing settlement and durable task state could not be confirmed. Query the task ID; do not repeat the provider operation.")
			}
			s.config.Logger.Warn("billing settlement requires reconciliation", "task_id", task.TaskID, "error", settlementErr)
			s.appendAudit(task.TaskID, "ascp.billing.settlement_pending", identity.Actor, map[string]any{"code": ascp.ErrBillingUnavailable})
			return jsonResult(http.StatusAccepted, ascp.CommitResponse{
				Task:              task,
				AcceptedAt:        now,
				IdempotencyExpiry: s.config.Now().Add(s.config.IdempotencyRetention),
			})
		}
		task, err = s.config.Store.UpdateTask(task.TaskID, func(current *ascp.Task) error {
			if current.Billing == nil {
				return errors.New("task billing record is missing")
			}
			settledAmount := settlement.Amount
			current.Billing.Mode = settlement.Mode
			current.Billing.ArrangementRef = settlement.ArrangementRef
			current.Billing.SettlementRef = settlement.Reference
			current.Billing.InvoiceRef = settlement.InvoiceRef
			current.Billing.PeriodRef = settlement.PeriodRef
			current.Billing.Amount = &settledAmount
			current.Billing.State = settlement.State
			return nil
		})
		if err != nil {
			s.config.Logger.Error("settled billing state requires reconciliation", "request_id", requestID, "task_id", task.TaskID, "settlement_ref", settlement.Reference, "error", err)
			return taskOutcomeUnknownResult(requestID, task, "Settled billing requires reconciliation", "The provider effect and billing settlement completed, but the service could not durably bind the settlement to the task. Query the task ID; do not create a new commit.")
		}
		s.appendAudit(task.TaskID, "ascp.billing.settled", identity.Actor, map[string]any{
			"settlement_ref": settlement.Reference,
			"amount":         settlement.Amount,
		})
	}

	completedAt := s.config.Now()
	task, err = s.config.Store.UpdateTask(task.TaskID, func(current *ascp.Task) error {
		current.State = ascp.TaskSucceeded
		current.UpdatedAt = completedAt
		current.CompletedAt = &completedAt
		current.Progress = 100
		current.Artifacts = result.Artifacts
		return nil
	})
	if err != nil {
		s.config.Logger.Error("successful provider outcome requires task reconciliation", "request_id", requestID, "task_id", task.TaskID, "error", err)
		return taskOutcomeUnknownResult(requestID, task, "Successful task outcome requires reconciliation", "The provider effect completed, but the service could not durably record the successful task state. Query the task ID; do not repeat the provider operation.")
	}
	s.appendAudit(task.TaskID, "ascp.task.succeeded", identity.Actor, map[string]any{
		"artifact_count": len(result.Artifacts),
	})
	task, err = s.attachReceipt(task, quote, identity.Actor, result.Artifacts, finalBreakdown)
	if err != nil {
		s.config.Logger.Error("successful task receipt requires reconciliation", "request_id", requestID, "task_id", task.TaskID, "error", err)
		task.StatusReason = "receipt_pending"
		return jsonResult(http.StatusAccepted, ascp.CommitResponse{
			Task:              task,
			AcceptedAt:        now,
			IdempotencyExpiry: s.config.Now().Add(s.config.IdempotencyRetention),
		})
	}

	return jsonResult(http.StatusCreated, ascp.CommitResponse{
		Task:              task,
		AcceptedAt:        now,
		IdempotencyExpiry: s.config.Now().Add(s.config.IdempotencyRetention),
	})
}

func (s *Server) appendAudit(chainID, eventType string, actor ascp.EntityRef, data any) {
	if _, err := s.config.Audit.Append(chainID, eventType, actor, data); err != nil {
		// The in-memory reference log fails only on serialization or signing. A
		// production implementation must place audit intent in the same durable
		// transaction as task state and reconcile any export/signing failure.
		s.config.Logger.Error("audit append failed", "chain_id", chainID, "event_type", eventType, "error", err)
	}
}

func (s *Server) attachReceipt(task ascp.Task, quote ascp.Quote, actor ascp.EntityRef, artifacts []ascp.ArtifactRef, billedBreakdown []ascp.PriceComponent) (ascp.Task, error) {
	root, err := s.config.Audit.Root(task.TaskID)
	if err != nil {
		return task, fmt.Errorf("load audit root: %w", err)
	}
	receipt := ascp.Receipt{
		ProtocolVersion: ascp.ProtocolVersion,
		ServiceID:       quote.ServiceID,
		ReceiptID:       ascp.MustNewID("rcp"),
		TaskID:          task.TaskID,
		QuoteID:         quote.QuoteID,
		Outcome:         task.State,
		Artifacts:       artifacts,
		Billing:         task.Billing,
		BilledBreakdown: billedBreakdown,
		AuditRoot:       root,
		CompletedAt:     s.config.Now(),
	}
	if task.Billing != nil && task.Billing.Amount != nil {
		billed := *task.Billing.Amount
		receipt.BilledAmount = &billed
	}
	unsigned, err := ascp.SigningProjection(receipt)
	if err != nil {
		return task, fmt.Errorf("create receipt signing projection: %w", err)
	}
	signature, err := s.config.Signer.SignJSON(unsigned)
	if err != nil {
		return task, fmt.Errorf("sign receipt: %w", err)
	}
	receipt.Signature = signature
	updated, err := s.config.Store.UpdateTask(task.TaskID, func(current *ascp.Task) error {
		current.Receipt = &receipt
		current.UpdatedAt = s.config.Now()
		return nil
	})
	if err != nil {
		return task, fmt.Errorf("persist receipt: %w", err)
	}
	s.appendAudit(task.TaskID, "ascp.receipt.issued", actor, map[string]any{
		"receipt_id": receipt.ReceiptID,
		"audit_root": root,
	})
	return updated, nil
}

func resolveFinalCharge(quote ascp.Quote, result ExecutionResult) (ascp.Money, []ascp.PriceComponent, error) {
	actual := quote.Price
	if quote.BillingTerms.VariablePriceAllowed && result.FinalPrice == nil {
		return ascp.Money{}, nil, errors.New("variable-price execution did not report a final price")
	}
	if result.FinalPrice != nil {
		actual = *result.FinalPrice
	}
	if err := ascp.ValidateMoney(actual); err != nil {
		return ascp.Money{}, nil, fmt.Errorf("final price: %w", err)
	}
	ceilingComparison, err := ascp.CompareMoney(quote.PriceCeiling, actual)
	if err != nil {
		return ascp.Money{}, nil, fmt.Errorf("compare final price with ceiling: %w", err)
	}
	if ceilingComparison < 0 {
		return ascp.Money{}, nil, errors.New("final price exceeds signed ceiling")
	}
	quotedComparison, err := ascp.CompareMoney(quote.Price, actual)
	if err != nil {
		return ascp.Money{}, nil, fmt.Errorf("compare final and quoted price: %w", err)
	}
	if !quote.BillingTerms.VariablePriceAllowed && quotedComparison != 0 {
		return ascp.Money{}, nil, errors.New("fixed-price execution changed the signed price")
	}

	breakdown := append([]ascp.PriceComponent(nil), result.FinalPriceBreakdown...)
	if len(breakdown) == 0 {
		if quotedComparison == 0 {
			breakdown = append([]ascp.PriceComponent(nil), quote.PriceBreakdown...)
		}
		return actual, breakdown, nil
	}

	total := ascp.Money{Currency: actual.Currency, Amount: "0"}
	for index, component := range breakdown {
		if strings.TrimSpace(component.Type) == "" {
			return ascp.Money{}, nil, fmt.Errorf("final price component %d has no type", index)
		}
		total, err = ascp.AddMoney(total, component.Amount)
		if err != nil {
			return ascp.Money{}, nil, fmt.Errorf("final price component %d: %w", index, err)
		}
	}
	comparison, err := ascp.CompareMoney(total, actual)
	if err != nil || comparison != 0 {
		return ascp.Money{}, nil, errors.New("final price breakdown does not sum to final price")
	}
	return actual, breakdown, nil
}

func taskOutcomeUnknownResult(requestID string, task ascp.Task, title, detail string) operationResult {
	p := problem(http.StatusInternalServerError, ascp.ErrOutcomeUnknown, title, detail, false)
	p.TaskID = task.TaskID
	p.TaskState = task.State
	return problemResult(requestID, p)
}

func validatePriceEstimate(estimate ascp.PriceEstimate) error {
	if err := ascp.ValidateMoney(estimate.Minimum); err != nil {
		return fmt.Errorf("minimum: %w", err)
	}
	if err := ascp.ValidateMoney(estimate.Maximum); err != nil {
		return fmt.Errorf("maximum: %w", err)
	}
	comparison, err := ascp.CompareMoney(estimate.Minimum, estimate.Maximum)
	if err != nil {
		return err
	}
	if comparison > 0 {
		return errors.New("estimated minimum exceeds maximum")
	}
	return nil
}

// validatePreparedContract is a deterministic guard between the provider-owned
// planning agent and the signed contract boundary. A domain implementation may
// use an LLM internally, but it cannot sign malformed money, an unexplained
// effect, an inconsistent price breakdown, or incomplete billing terms.
func validatePreparedContract(contract PreparedContract) error {
	if len(contract.NormalizedTask) == 0 {
		return errors.New("normalized task is empty")
	}
	if len(contract.Effects) == 0 {
		return errors.New("at least one explicit effect is required")
	}
	for index, effect := range contract.Effects {
		if strings.TrimSpace(effect.Type) == "" || strings.TrimSpace(effect.Summary) == "" {
			return fmt.Errorf("effect %d requires type and summary", index)
		}
	}
	if strings.TrimSpace(contract.RiskClass) == "" {
		return errors.New("risk class is required")
	}
	if contract.Confirmation.Required && strings.TrimSpace(contract.Confirmation.Mode) == "" {
		return errors.New("confirmation mode is required when confirmation is required")
	}
	if contract.DataUse.RetentionSeconds < 0 {
		return errors.New("data retention cannot be negative")
	}
	if contract.SLA.ExpectedCompletionSeconds < 0 || contract.SLA.MaximumCompletionSeconds < 0 {
		return errors.New("SLA durations cannot be negative")
	}
	if contract.SLA.MaximumCompletionSeconds > 0 && contract.SLA.ExpectedCompletionSeconds > contract.SLA.MaximumCompletionSeconds {
		return errors.New("expected completion exceeds maximum completion")
	}
	if contract.RevocableUntil != nil && contract.RevocableUntil.After(contract.ExpiresAt) {
		return errors.New("revocable_until exceeds quote expiry")
	}

	if err := ascp.ValidateMoney(contract.Price); err != nil {
		return fmt.Errorf("price: %w", err)
	}
	if err := ascp.ValidateMoney(contract.PriceCeiling); err != nil {
		return fmt.Errorf("price ceiling: %w", err)
	}
	ceilingComparison, err := ascp.CompareMoney(contract.PriceCeiling, contract.Price)
	if err != nil {
		return err
	}
	if ceilingComparison < 0 {
		return errors.New("price ceiling is below price")
	}
	if !contract.BillingTerms.VariablePriceAllowed && ceilingComparison != 0 {
		return errors.New("a non-variable quote must have equal price and price ceiling")
	}
	if err := ascp.ValidateBillingTerms(contract.BillingTerms, contract.Price); err != nil {
		return fmt.Errorf("billing terms: %w", err)
	}

	if len(contract.PriceBreakdown) > 0 {
		total := ascp.Money{Currency: contract.Price.Currency, Amount: "0"}
		for index, component := range contract.PriceBreakdown {
			if strings.TrimSpace(component.Type) == "" {
				return fmt.Errorf("price component %d has no type", index)
			}
			var addErr error
			total, addErr = ascp.AddMoney(total, component.Amount)
			if addErr != nil {
				return fmt.Errorf("price component %d: %w", index, addErr)
			}
		}
		comparison, compareErr := ascp.CompareMoney(total, contract.Price)
		if compareErr != nil || comparison != 0 {
			return errors.New("price breakdown does not sum to price")
		}
	}
	return nil
}

func validateContextRef(ref ascp.ContextRef, now time.Time) error {
	if strings.TrimSpace(ref.URI) == "" {
		return errors.New("uri is required")
	}
	if len(ref.URI) > 4096 {
		return errors.New("uri exceeds 4096 bytes")
	}
	if ref.Size < 0 {
		return errors.New("size must not be negative")
	}
	if !ref.ExpiresAt.IsZero() && !ref.ExpiresAt.After(now) {
		return errors.New("reference is already expired")
	}
	if strings.ContainsAny(ref.Digest, "\r\n\t ") {
		return errors.New("digest must not contain whitespace")
	}
	return nil
}

func validateAuthorization(authorization ascp.AuthorizationEvidence, quote ascp.Quote, identity Identity, now time.Time) *ascp.Problem {
	if authorization.Type == "" || authorization.Reference == "" || authorization.PrincipalID == "" || authorization.Audience == "" ||
		authorization.BindingDigest == "" || authorization.ApprovedAt.IsZero() || authorization.ExpiresAt.IsZero() {
		p := problem(http.StatusForbidden, ascp.ErrAuthorizationRequired, "Complete authorization evidence required", "Authorization type, reference, principal, audience, quote digest, approval time, and expiry are required.", false)
		return &p
	}
	if authorization.PrincipalID != identity.Principal.ID || authorization.Audience != quote.ServiceID || authorization.BindingDigest != quote.Signature.PayloadDigest {
		p := problem(http.StatusForbidden, ascp.ErrAuthorizationInvalid, "Authorization is not bound to this quote", "Principal, service audience, or quote digest mismatch.", false)
		return &p
	}
	if !authorization.ExpiresAt.After(authorization.ApprovedAt) || !authorization.ExpiresAt.After(now) ||
		authorization.ApprovedAt.After(now.Add(5*time.Minute)) || authorization.ApprovedAt.Before(quote.IssuedAt.Add(-5*time.Minute)) {
		p := problem(http.StatusForbidden, ascp.ErrAuthorizationInvalid, "Authorization timing is invalid", "The approval must be contemporaneous with the signed quote, must not be future-dated, and must expire after approval and after the current time.", false)
		return &p
	}
	return nil
}

func validateBillingAuthorization(
	authorization *ascp.BillingAuthorization,
	principal ascp.EntityRef,
	serviceID string,
	bindingDigest string,
	terms ascp.BillingTerms,
	amount ascp.Money,
	now time.Time,
) *ascp.Problem {
	if terms.Mode == ascp.BillingFree {
		if authorization != nil {
			p := problem(http.StatusUnprocessableEntity, ascp.ErrValidationFailed, "Billing authorization not applicable", "Free service does not accept a billing authorization.", false)
			return &p
		}
		return nil
	}
	if terms.ArrangementRequired && strings.TrimSpace(terms.ArrangementRef) == "" {
		p := problem(http.StatusPreconditionRequired, ascp.ErrBillingRequired, "Billing arrangement required", "The selected billing mode requires a signed arrangement reference.", false)
		return &p
	}
	if terms.AuthorizationRequired && authorization == nil {
		p := problem(http.StatusPaymentRequired, ascp.ErrBillingRequired, "Billing authorization required", "The selected billing mode requires a tokenized per-call authorization.", false)
		return &p
	}
	if authorization == nil {
		return nil
	}
	if authorization.Mode != terms.Mode {
		p := problem(http.StatusPaymentRequired, ascp.ErrBillingDeclined, "Billing mode mismatch", "The authorization mode does not match the signed billing terms.", false)
		return &p
	}
	if authorization.Payer != principal {
		p := problem(http.StatusPaymentRequired, ascp.ErrBillingDeclined, "Billing payer mismatch", "The billing mandate is not issued for the authenticated principal.", false)
		return &p
	}
	if authorization.Audience != serviceID || authorization.BindingDigest != bindingDigest {
		p := problem(http.StatusPaymentRequired, ascp.ErrBillingDeclined, "Billing authorization is not bound to this operation", "The service audience or binding digest does not match.", false)
		return &p
	}
	if terms.ArrangementRef != "" && authorization.ArrangementRef != "" && authorization.ArrangementRef != terms.ArrangementRef {
		p := problem(http.StatusPaymentRequired, ascp.ErrBillingDeclined, "Billing arrangement mismatch", "The authorization references a different billing arrangement.", false)
		return &p
	}
	if !authorization.ExpiresAt.IsZero() && !authorization.ExpiresAt.After(now) {
		p := problem(http.StatusPaymentRequired, ascp.ErrBillingDeclined, "Billing authorization expired", "The billing mandate is no longer valid.", false)
		return &p
	}
	if authorization.Usage != "" && authorization.Usage != "single_use" && authorization.Usage != "reusable" {
		p := problem(http.StatusPaymentRequired, ascp.ErrBillingDeclined, "Unsupported billing authorization usage", "usage must be single_use or reusable.", false)
		return &p
	}
	if terms.Mode == ascp.BillingPayNow {
		if strings.TrimSpace(authorization.AuthorizationRef) == "" {
			p := problem(http.StatusPaymentRequired, ascp.ErrBillingDeclined, "Billing authorization reference required", "Pay-now requires an opaque authorization reference.", false)
			return &p
		}
		if authorization.MaximumAmount == nil {
			p := problem(http.StatusPaymentRequired, ascp.ErrBillingDeclined, "Billing maximum required", "Pay-now requires a maximum authorized amount.", false)
			return &p
		}
	}
	if authorization.MaximumAmount != nil {
		if err := ascp.ValidateMoney(*authorization.MaximumAmount); err != nil {
			p := problem(http.StatusPaymentRequired, ascp.ErrBillingDeclined, "Invalid billing authorization amount", err.Error(), false)
			return &p
		}
		comparison, err := ascp.CompareMoney(*authorization.MaximumAmount, amount)
		if err != nil || comparison < 0 {
			detail := "The billing authorization maximum is below the operation ceiling."
			if err != nil {
				detail = err.Error()
			}
			p := problem(http.StatusPaymentRequired, ascp.ErrBillingDeclined, "Insufficient billing authorization", detail, false)
			return &p
		}
	}
	return nil
}

func (s *Server) handleTaskRoutes(writer http.ResponseWriter, request *http.Request) {
	trimmed := strings.TrimPrefix(request.URL.Path, "/v1/tasks/")
	parts := strings.Split(strings.Trim(trimmed, "/"), "/")
	if len(parts) == 0 || parts[0] == "" {
		writeProblem(writer, ascp.MustNewID("req"), problem(http.StatusNotFound, ascp.ErrNotFound, "Task not found", "A task ID is required.", false))
		return
	}
	taskID := parts[0]
	if len(parts) == 1 && request.Method == http.MethodGet {
		s.handleGetTask(writer, request, taskID)
		return
	}
	if len(parts) == 2 && parts[1] == "cancel" && request.Method == http.MethodPost {
		s.handleMutating(writer, request, func(ctx context.Context, identity Identity, requestID string, body []byte, key string) operationResult {
			return s.cancel(ctx, identity, requestID, body, key, taskID)
		})
		return
	}
	if len(parts) == 2 && parts[1] == "events" && request.Method == http.MethodGet {
		s.handleEvents(writer, request, taskID)
		return
	}
	if len(parts) == 2 && parts[1] == "audit" && request.Method == http.MethodGet {
		s.handleAudit(writer, request, taskID)
		return
	}
	writeProblem(writer, ascp.MustNewID("req"), problem(http.StatusNotFound, ascp.ErrNotFound, "Route not found", "The requested task operation is not available.", false))
}

func (s *Server) handleGetTask(writer http.ResponseWriter, request *http.Request, taskID string) {
	requestID, identity, ok := s.authenticateRead(writer, request)
	if !ok {
		return
	}
	task, quote, p := s.authorizedTask(identity, taskID)
	if p != nil {
		writeProblem(writer, requestID, *p)
		return
	}
	_ = quote
	writer.Header().Set("ETag", fmt.Sprintf("\"v%d\"", task.Version))
	writer.Header().Set("X-Request-ID", requestID)
	writeJSON(writer, http.StatusOK, task, nil)
}

func (s *Server) cancel(ctx context.Context, identity Identity, requestID string, body []byte, idempotencyKey, taskID string) operationResult {
	var input ascp.CancelRequest
	if len(bytes.TrimSpace(body)) > 0 {
		if p := decodeStrict(body, &input); p != nil {
			return problemResult(requestID, *p)
		}
	}
	task, _, p := s.authorizedTask(identity, taskID)
	if p != nil {
		return problemResult(requestID, *p)
	}
	if input.ExpectedVersion != nil && *input.ExpectedVersion != task.Version {
		return problemResult(requestID, problem(http.StatusPreconditionFailed, ascp.ErrPreconditionFailed, "Task version changed", "Read the current task before retrying cancellation.", true))
	}
	switch task.State {
	case ascp.TaskSucceeded, ascp.TaskFailed, ascp.TaskCancelled, ascp.TaskCompensated:
		return problemResult(requestID, problem(http.StatusConflict, ascp.ErrTaskNotCancellable, "Task cannot be cancelled", "The task is already in a terminal state.", false))
	}
	if p := s.config.Service.Cancel(ctx, identity, task, input); p != nil {
		return problemResult(requestID, *p)
	}
	if task.Billing != nil && task.Billing.ReservationRef != "" && task.Billing.SettlementRef == "" {
		if err := s.config.Billing.Release(ctx, task.Billing.ReservationRef, idempotencyKey+":cancel-void"); err != nil {
			return problemResult(requestID, billingProblem(err))
		}
	}
	now := s.config.Now()
	updated, err := s.config.Store.UpdateTask(taskID, func(current *ascp.Task) error {
		current.State = ascp.TaskCancelled
		current.StatusReason = input.Reason
		current.UpdatedAt = now
		current.CompletedAt = &now
		current.Progress = 100
		if current.Billing != nil && current.Billing.ReservationRef != "" && current.Billing.SettlementRef == "" {
			current.Billing.State = "released"
		}
		return nil
	})
	if err != nil {
		return problemResult(requestID, problem(http.StatusInternalServerError, ascp.ErrInternal, "Cancellation failed", "The service could not persist the cancellation transition.", true))
	}
	s.appendAudit(taskID, "ascp.task.cancelled", identity.Actor, map[string]any{"reason": input.Reason})
	return jsonResult(http.StatusOK, updated)
}

func (s *Server) handleEvents(writer http.ResponseWriter, request *http.Request, taskID string) {
	requestID, identity, ok := s.authenticateRead(writer, request)
	if !ok {
		return
	}
	if _, _, p := s.authorizedTask(identity, taskID); p != nil {
		writeProblem(writer, requestID, *p)
		return
	}
	after := int64(0)
	if last := request.Header.Get("Last-Event-ID"); last != "" {
		after, _ = strconv.ParseInt(last, 10, 64)
	}
	events, err := s.config.Audit.List(taskID, after)
	if err != nil {
		s.config.Logger.Error("audit event lookup failed", "task_id", taskID, "error", err)
		writeProblem(writer, requestID, problem(http.StatusServiceUnavailable, ascp.ErrServiceUnavailable, "Audit store unavailable", "The service could not load task events.", true))
		return
	}
	writer.Header().Set("Content-Type", "text/event-stream")
	writer.Header().Set("X-Request-ID", requestID)
	writer.WriteHeader(http.StatusOK)
	for _, event := range events {
		encoded, marshalErr := json.Marshal(event)
		if marshalErr != nil {
			s.config.Logger.Error("audit event serialization failed", "task_id", taskID, "sequence", event.Sequence, "error", marshalErr)
			continue
		}
		if _, writeErr := fmt.Fprintf(writer, "id: %d\nevent: %s\ndata: %s\n\n", event.Sequence, event.Type, encoded); writeErr != nil {
			return
		}
	}
}

func (s *Server) handleAudit(writer http.ResponseWriter, request *http.Request, taskID string) {
	requestID, identity, ok := s.authenticateRead(writer, request)
	if !ok {
		return
	}
	if _, _, p := s.authorizedTask(identity, taskID); p != nil {
		writeProblem(writer, requestID, *p)
		return
	}
	events, err := s.config.Audit.List(taskID, 0)
	if err != nil {
		s.config.Logger.Error("audit chain lookup failed", "task_id", taskID, "error", err)
		writeProblem(writer, requestID, problem(http.StatusServiceUnavailable, ascp.ErrServiceUnavailable, "Audit store unavailable", "The service could not load the task audit chain.", true))
		return
	}
	root, err := s.config.Audit.Root(taskID)
	if err != nil {
		s.config.Logger.Error("audit root lookup failed", "task_id", taskID, "error", err)
		writeProblem(writer, requestID, problem(http.StatusServiceUnavailable, ascp.ErrServiceUnavailable, "Audit store unavailable", "The service could not load the task audit root.", true))
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{
		"task_id": taskID,
		"events":  events,
		"root":    root,
	}, http.Header{"X-Request-ID": []string{requestID}})
}

func (s *Server) authenticateRead(writer http.ResponseWriter, request *http.Request) (string, Identity, bool) {
	requestID := request.Header.Get("X-Request-ID")
	if requestID == "" || len(requestID) > 128 {
		requestID = ascp.MustNewID("req")
	}
	identity, err := s.config.Authenticator.Authenticate(request)
	if err != nil {
		writer.Header().Set("WWW-Authenticate", `Bearer realm="ascp"`)
		writeProblem(writer, requestID, problem(http.StatusUnauthorized, ascp.ErrUnauthenticated, "Authentication required", "The supplied access token is missing, invalid, expired, or not authorized for this service.", false))
		return requestID, Identity{}, false
	}
	return requestID, identity, true
}

func (s *Server) authorizedTask(identity Identity, taskID string) (ascp.Task, ascp.Quote, *ascp.Problem) {
	task, ok, err := s.config.Store.GetTask(taskID)
	if err != nil {
		s.config.Logger.Error("task lookup failed", "task_id", taskID, "error", err)
		p := problem(http.StatusServiceUnavailable, ascp.ErrServiceUnavailable, "Task store unavailable", "The service could not determine the task state.", true)
		return ascp.Task{}, ascp.Quote{}, &p
	}
	if !ok {
		p := problem(http.StatusNotFound, ascp.ErrNotFound, "Task not found", "The task does not exist.", false)
		return ascp.Task{}, ascp.Quote{}, &p
	}
	quote, ok, err := s.config.Store.GetQuote(task.QuoteID)
	if err != nil {
		s.config.Logger.Error("task quote lookup failed", "task_id", taskID, "quote_id", task.QuoteID, "error", err)
		p := problem(http.StatusServiceUnavailable, ascp.ErrServiceUnavailable, "Quote store unavailable", "The service could not determine the task's signed quote state.", true)
		return ascp.Task{}, ascp.Quote{}, &p
	}
	if !ok {
		p := problem(http.StatusInternalServerError, ascp.ErrInternal, "Task quote missing", "The task's signed quote could not be loaded.", true)
		return ascp.Task{}, ascp.Quote{}, &p
	}
	if quote.Actor != identity.Actor || quote.Principal != identity.Principal {
		p := problem(http.StatusForbidden, ascp.ErrForbidden, "Task ownership mismatch", "The task belongs to a different delegation.", false)
		return ascp.Task{}, ascp.Quote{}, &p
	}
	return task, quote, nil
}

// validIdempotencyKey accepts opaque visible-ASCII values only. Restricting the
// header prevents delimiter ambiguity, invisible Unicode lookalikes, and control
// characters from reaching logs or persistence keys.
func validIdempotencyKey(key string) bool {
	if len(key) < 16 || len(key) > 255 {
		return false
	}
	for index := 0; index < len(key); index++ {
		if key[index] < 0x21 || key[index] > 0x7e {
			return false
		}
	}
	return true
}

func decodeStrict(body []byte, destination any) *ascp.Problem {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		p := problem(http.StatusBadRequest, ascp.ErrInvalidRequest, "Invalid JSON request", err.Error(), false)
		return &p
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		p := problem(http.StatusBadRequest, ascp.ErrInvalidRequest, "Invalid JSON request", "Only one JSON value is allowed.", false)
		return &p
	}
	return nil
}

func jsonResult(status int, value any) operationResult {
	encoded, err := json.Marshal(value)
	if err != nil {
		return problemResult("", problem(http.StatusInternalServerError, ascp.ErrInternal, "Response serialization failed", "The service could not serialize its response.", true))
	}
	result := operationResult{
		Status: status,
		Headers: http.Header{
			"Content-Type": []string{ascp.MediaType},
		},
		Body: encoded,
	}
	switch typed := value.(type) {
	case ascp.CommitResponse:
		result.TaskID = typed.Task.TaskID
		result.TaskState = typed.Task.State
	case ascp.Task:
		result.TaskID = typed.TaskID
		result.TaskState = typed.State
	case ascp.DirectInvocationResponse:
		result.InvocationID = typed.InvocationID
		result.InvocationState = typed.State
	}
	return result
}

func problemResult(requestID string, p ascp.Problem) operationResult {
	p.RequestID = requestID
	encoded, _ := json.Marshal(p)
	headers := http.Header{"Content-Type": []string{"application/problem+json"}}
	if p.RetryAfterMS > 0 {
		headers.Set("Retry-After", strconv.FormatInt((p.RetryAfterMS+999)/1000, 10))
	}
	return operationResult{
		Status:          p.Status,
		Headers:         headers,
		Body:            encoded,
		TaskID:          p.TaskID,
		TaskState:       p.TaskState,
		InvocationID:    p.InvocationID,
		InvocationState: p.InvocationState,
	}
}

func writeOperationResult(writer http.ResponseWriter, result operationResult, requestID string) {
	if result.Headers == nil {
		result.Headers = make(http.Header)
	}
	result.Headers.Set("ASCP-Version", ascp.ProtocolVersion)
	result.Headers.Set("X-Request-ID", requestID)
	if result.Headers.Get("Content-Type") == "" {
		result.Headers.Set("Content-Type", ascp.MediaType)
	}
	for name, values := range result.Headers {
		for _, value := range values {
			writer.Header().Add(name, value)
		}
	}
	writer.WriteHeader(result.Status)
	_, _ = writer.Write(result.Body)
}

// safeRetryProblemResult marks a transient response as eligible to release the
// current idempotency claim. Callers must use it only before any durable
// contract, billing operation, task creation, or provider side effect.
func safeRetryProblemResult(requestID string, p ascp.Problem) operationResult {
	result := problemResult(requestID, p)
	result.ReleaseClaim = true
	return result
}

func problem(status int, code, title, detail string, retryable bool) ascp.Problem {
	return ascp.Problem{
		Type:      "urn:ascp:problem:" + code,
		Title:     title,
		Status:    status,
		Detail:    detail,
		Code:      code,
		Category:  errorCategory(status, code),
		Retryable: retryable,
	}
}

func billingProblem(err error) ascp.Problem {
	var unknown billing.UnknownOutcomeError
	if errors.As(err, &unknown) {
		p := problem(http.StatusServiceUnavailable, ascp.ErrBillingOutcomeUnknown, "Billing outcome is unknown", unknown.Error(), false)
		if unknown.ReconciliationRef != "" {
			p.Extensions = map[string]interface{}{"billing_reconciliation_ref": unknown.ReconciliationRef}
		}
		return p
	}
	var declined billing.DeclinedError
	if errors.As(err, &declined) {
		return problem(http.StatusPaymentRequired, ascp.ErrBillingDeclined, "Billing declined", declined.Error(), false)
	}
	var temporary billing.TemporaryError
	if errors.As(err, &temporary) {
		p := problem(http.StatusServiceUnavailable, ascp.ErrBillingUnavailable, "Billing provider unavailable", temporary.Error(), true)
		p.RetryAfterMS = 2000
		return p
	}
	return problem(http.StatusServiceUnavailable, ascp.ErrBillingOutcomeUnknown, "Billing processing outcome is unknown", "The billing adapter returned an unclassified error. The provider must reconcile before allowing another attempt: "+err.Error(), false)
}

func errorCategory(status int, code string) string {
	// The stable protocol code is more informative than HTTP status alone. For
	// example, billing_unavailable is an upstream 503 but remains a billing error
	// for autonomous retry and reconciliation policy.
	switch {
	case strings.HasPrefix(code, "billing_") || strings.HasPrefix(code, "settlement_"):
		return "billing"
	case strings.HasPrefix(code, "file_") || code == ascp.ErrUploadExpired || code == ascp.ErrDigestMismatch:
		return "file"
	case strings.HasPrefix(code, "authorization_") || status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "authorization"
	case strings.HasPrefix(code, "idempotency_") || code == ascp.ErrRequestInProgress || code == ascp.ErrOutcomeUnknown:
		return "idempotency"
	case status == http.StatusTooManyRequests:
		return "rate_limit"
	case status >= 500:
		return "service"
	case status == http.StatusConflict || status == http.StatusPreconditionFailed:
		return "conflict"
	default:
		return "request"
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func writeProblem(writer http.ResponseWriter, requestID string, p ascp.Problem) {
	p.RequestID = requestID
	writeJSON(writer, p.Status, p, http.Header{"Content-Type": []string{"application/problem+json"}, "X-Request-ID": []string{requestID}})
}

func writeJSON(writer http.ResponseWriter, status int, value any, headers http.Header) {
	if headers != nil {
		for name, values := range headers {
			for _, value := range values {
				writer.Header().Add(name, value)
			}
		}
	}
	if writer.Header().Get("Content-Type") == "" {
		writer.Header().Set("Content-Type", ascp.MediaType)
	}
	writer.WriteHeader(status)
	_ = json.NewEncoder(writer).Encode(value)
}
