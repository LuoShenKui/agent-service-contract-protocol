package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
)

// handleCapabilities exposes a compact, cacheable list of task-level abilities.
// It intentionally does not publish every platform-internal API or JSON Schema.
func (s *Server) handleCapabilities(writer http.ResponseWriter, request *http.Request) {
	requestID, identity, ok := s.authenticateRead(writer, request)
	if !ok {
		return
	}

	limit := 0
	if rawLimit := request.URL.Query().Get("limit"); rawLimit != "" {
		parsed, err := strconv.Atoi(rawLimit)
		if err != nil || parsed < 1 || parsed > 100 {
			writeProblem(writer, requestID, problem(http.StatusBadRequest, ascp.ErrInvalidRequest, "Invalid capability limit", "limit must be an integer between 1 and 100.", false))
			return
		}
		limit = parsed
	}
	query := ascp.CapabilityQuery{
		Query:  request.URL.Query().Get("q"),
		Intent: request.URL.Query().Get("intent"),
		Cursor: request.URL.Query().Get("cursor"),
		Limit:  limit,
	}
	catalog, providerProblem := s.config.Service.Capabilities(request.Context(), identity, query)
	if providerProblem != nil {
		writeProblem(writer, requestID, *providerProblem)
		return
	}
	catalog.ServiceID = s.config.Manifest.ServiceID
	if catalog.GeneratedAt.IsZero() {
		catalog.GeneratedAt = s.config.Now()
	}
	if catalog.ExpiresAt.IsZero() || !catalog.ExpiresAt.After(catalog.GeneratedAt) {
		catalog.ExpiresAt = catalog.GeneratedAt.Add(15 * time.Minute)
	}

	encoded, err := json.Marshal(catalog)
	if err != nil {
		writeProblem(writer, requestID, problem(http.StatusInternalServerError, ascp.ErrInternal, "Capability serialization failed", "The service could not serialize its capability catalog.", true))
		return
	}
	etag := `"` + ascp.SHA256Digest(encoded) + `"`
	cacheControl := "private, max-age=300, must-revalidate"
	if request.Header.Get("If-None-Match") == etag {
		writer.Header().Set("ETag", etag)
		writer.Header().Set("Cache-Control", cacheControl)
		writer.Header().Set("X-Request-ID", requestID)
		writer.WriteHeader(http.StatusNotModified)
		return
	}
	writeJSON(writer, http.StatusOK, catalog, http.Header{
		"ETag":          []string{etag},
		"Cache-Control": []string{cacheControl},
		"X-Request-ID":  []string{requestID},
	})
}

// handleInvokeOptions implements ordinary HTTP OPTIONS. Semantic preflight is
// POST /v1/options because HTTP OPTIONS has no portable request-body semantics.
func (s *Server) handleInvokeOptions(writer http.ResponseWriter, _ *http.Request) {
	writer.Header().Set("Allow", "OPTIONS, POST")
	writer.Header().Set("Accept-Post", ascp.MediaType+", application/json")
	writer.Header().Set("ASCP-Options-URI", s.config.Manifest.OptionsURI)
	writer.Header().Set("ASCP-Capabilities-URI", s.config.Manifest.CapabilitiesURI)
	writer.WriteHeader(http.StatusNoContent)
}

// handleOptions performs an optional, side-effect-free preflight. It is useful
// only when the caller does not already know the intent or required parameters.
func (s *Server) handleOptions(writer http.ResponseWriter, request *http.Request) {
	requestID, identity, body, ok := s.readAuthenticatedJSON(writer, request)
	if !ok {
		return
	}
	var input ascp.OptionsRequest
	if p := decodeStrict(body, &input); p != nil {
		writeProblem(writer, requestID, *p)
		return
	}
	if strings.TrimSpace(input.Intent) == "" && strings.TrimSpace(input.Goal) == "" {
		writeProblem(writer, requestID, problem(http.StatusUnprocessableEntity, ascp.ErrValidationFailed, "Intent or goal required", "Provide intent or goal for semantic preflight.", false))
		return
	}
	if p := s.validateReferences(identity, input.ContextRefs, input.InputFiles); p != nil {
		writeProblem(writer, requestID, *p)
		return
	}
	response, providerProblem := s.config.Service.Options(request.Context(), identity, input)
	if providerProblem != nil {
		writeProblem(writer, requestID, *providerProblem)
		return
	}
	response.ServerRequestID = requestID
	writeJSON(writer, http.StatusOK, response, http.Header{"X-Request-ID": []string{requestID}})
}

// handleInvoke executes the compact ask-and-answer path. The idempotency key is
// optional for read-only operations and mandatory whenever the provider plan
// says a retry could repeat a side effect or billing action.
func (s *Server) handleInvoke(writer http.ResponseWriter, request *http.Request) {
	requestID, identity, body, ok := s.readAuthenticatedJSON(writer, request)
	if !ok {
		return
	}

	var input ascp.DirectInvocationRequest
	if p := decodeStrict(body, &input); p != nil {
		writeProblem(writer, requestID, *p)
		return
	}
	if strings.TrimSpace(input.Intent) == "" && strings.TrimSpace(input.Goal) == "" {
		writeProblem(writer, requestID, problem(http.StatusUnprocessableEntity, ascp.ErrValidationFailed, "Intent or goal required", "Provide intent or goal for direct invocation.", false))
		return
	}
	if p := s.validateReferences(identity, input.ContextRefs, input.InputFiles); p != nil {
		writeProblem(writer, requestID, *p)
		return
	}

	plan, providerProblem := s.config.Service.PlanDirect(request.Context(), identity, input)
	if providerProblem != nil {
		writeProblem(writer, requestID, *providerProblem)
		return
	}
	if !plan.Eligible {
		code := plan.ReasonCode
		if code == "" {
			code = ascp.ErrContractRequired
		}
		p := problem(http.StatusConflict, code, "Full contract flow required", plan.Reason, false)
		p.Extensions = map[string]any{
			"resolved_intent": plan.ResolvedIntent,
			"options_uri":     s.config.Manifest.OptionsURI,
			"negotiate_uri":   "/v1/negotiate",
		}
		writeProblem(writer, requestID, p)
		return
	}
	if err := validateDirectPlan(plan); err != nil {
		s.config.Logger.Error("service returned invalid direct plan", "request_id", requestID, "error", err)
		writeProblem(writer, requestID, problem(http.StatusInternalServerError, ascp.ErrInternal, "Invalid direct plan", "The platform agent produced a direct plan that violates ASCP invariants.", false))
		return
	}
	for _, scope := range plan.RequiredScopes {
		if !identity.HasScope(scope) {
			writeProblem(writer, requestID, problem(http.StatusForbidden, ascp.ErrForbidden, "Missing delegated scope", "The authenticated agent lacks required scope: "+scope, false))
			return
		}
	}

	requestDigest := ascp.SHA256Digest(body)
	if plan.AuthorizationRequired {
		if input.Authorization == nil {
			writeProblem(writer, requestID, problem(http.StatusForbidden, ascp.ErrAuthorizationRequired, "Authorization required", "This direct invocation requires independent approval bound to the request digest.", false))
			return
		}
		if p := validateDirectAuthorization(*input.Authorization, identity, s.config.Manifest.ServiceID, requestDigest, s.config.Now()); p != nil {
			writeProblem(writer, requestID, *p)
			return
		}
	}

	idempotencyKey := request.Header.Get("Idempotency-Key")
	if plan.IdempotencyRequired && !validIdempotencyKey(idempotencyKey) {
		writeProblem(writer, requestID, problem(http.StatusBadRequest, ascp.ErrInvalidRequest, "Idempotency-Key required", "This direct invocation can create a side effect or billing record and requires a 16-255 byte visible-ASCII Idempotency-Key.", false))
		return
	}
	if idempotencyKey != "" && !validIdempotencyKey(idempotencyKey) {
		writeProblem(writer, requestID, problem(http.StatusBadRequest, ascp.ErrInvalidRequest, "Invalid Idempotency-Key", "Idempotency-Key must contain 16 to 255 visible ASCII bytes without whitespace.", false))
		return
	}

	var scope string
	if idempotencyKey != "" {
		scope = identity.Actor.Type + "/" + identity.Actor.ID + "|" + identity.Principal.Type + "/" + identity.Principal.ID + "|POST|/v1/invoke"
		state, replay, err := s.config.Idempotency.Begin(scope, idempotencyKey, requestDigest, s.config.IdempotencyRetention)
		if err != nil {
			writeProblem(writer, requestID, problem(http.StatusServiceUnavailable, ascp.ErrServiceUnavailable, "Idempotency service unavailable", "The service could not safely determine whether this invocation may begin.", true))
			return
		}
		switch state {
		case IdempotencyReplay:
			writeStoredResponse(writer, replay, requestID)
			return
		case IdempotencyConflict:
			writeProblem(writer, requestID, problem(http.StatusConflict, ascp.ErrIdempotencyConflict, "Idempotency conflict", "The same key was already used with a different invocation request.", false))
			return
		case IdempotencyInProgress:
			p := problem(http.StatusConflict, ascp.ErrRequestInProgress, "Invocation already in progress", "Retry the identical request with the same idempotency key after the indicated delay.", true)
			p.RetryAfterMS = 1000
			writeProblem(writer, requestID, p)
			return
		}
	}

	result := s.executeDirect(request.Context(), identity, requestID, input, plan, requestDigest, idempotencyKey)
	if idempotencyKey != "" {
		if result.ReleaseClaim {
			_ = s.config.Idempotency.Release(scope, idempotencyKey, requestDigest)
		} else {
			stored := StoredResponse{
				StatusCode: result.Status,
				Headers:    result.Headers,
				Body:       result.Body,
				ExpiresAt:  s.config.Now().Add(s.config.IdempotencyRetention),
			}
			if err := s.config.Idempotency.Complete(scope, idempotencyKey, stored); err != nil {
				s.config.Logger.Error("direct idempotency completion failed", "request_id", requestID, "invocation_id", result.InvocationID, "error", err)
				p := problem(http.StatusInternalServerError, ascp.ErrOutcomeUnknown, "Invocation replay state is unknown", "The invocation may have completed, but its replay record could not be persisted. Do not create a new request; reconcile by invocation ID.", false)
				p.InvocationID = result.InvocationID
				p.InvocationState = result.InvocationState
				writeProblem(writer, requestID, p)
				return
			}
		}
	}
	writeOperationResult(writer, result, requestID)
}

func (s *Server) executeDirect(
	ctx context.Context,
	identity Identity,
	requestID string,
	input ascp.DirectInvocationRequest,
	plan DirectPlan,
	requestDigest string,
	idempotencyKey string,
) operationResult {
	invocationID := ascp.MustNewID("inv")
	s.appendAudit(invocationID, "ascp.invocation.accepted", identity.Actor, map[string]any{
		"intent":         plan.ResolvedIntent,
		"request_digest": requestDigest,
	})

	billingRecord, billingProblemValue := s.reserveBilling(ctx, identity, invocationID, requestDigest, plan.BillingTerms, input.BillingAuthorization, plan.PriceCeiling, directBillingKey(idempotencyKey, invocationID, "reserve"))
	if billingProblemValue != nil {
		result := problemResult(requestID, *billingProblemValue)
		result.InvocationID = invocationID
		result.InvocationState = ascp.InvocationFailed
		return result
	}
	if billingRecord != nil {
		s.appendAudit(invocationID, "ascp.billing.reserved", identity.Actor, map[string]any{
			"mode":            billingRecord.Mode,
			"reservation_ref": billingRecord.ReservationRef,
			"amount":          billingRecord.Amount,
		})
	}

	executionResult, providerProblem := s.config.Service.ExecuteDirect(ctx, identity, invocationID, input, plan)
	if providerProblem != nil {
		if billingRecord != nil && billingRecord.ReservationRef != "" {
			if err := s.config.Billing.Release(ctx, billingRecord.ReservationRef, directBillingKey(idempotencyKey, invocationID, "release")); err != nil {
				p := billingProblem(err)
				p.InvocationID = invocationID
				p.InvocationState = ascp.InvocationAccepted
				result := problemResult(requestID, p)
				result.InvocationID = invocationID
				result.InvocationState = ascp.InvocationAccepted
				return result
			}
			billingRecord.State = "released"
		}
		s.appendAudit(invocationID, "ascp.invocation.failed", identity.Actor, map[string]any{"code": providerProblem.Code})
		providerProblem.InvocationID = invocationID
		providerProblem.InvocationState = ascp.InvocationFailed
		result := problemResult(requestID, *providerProblem)
		result.InvocationID = invocationID
		result.InvocationState = ascp.InvocationFailed
		return result
	}

	finalPrice, finalBreakdown, err := resolveDirectPrice(plan, executionResult)
	if err != nil {
		if billingRecord != nil && billingRecord.ReservationRef != "" {
			_ = s.config.Billing.Release(ctx, billingRecord.ReservationRef, directBillingKey(idempotencyKey, invocationID, "release-invalid-price"))
		}
		p := problem(http.StatusInternalServerError, ascp.ErrSettlementFailed, "Invalid direct billing result", err.Error(), false)
		p.InvocationID = invocationID
		p.InvocationState = ascp.InvocationFailed
		result := problemResult(requestID, p)
		result.InvocationID = invocationID
		result.InvocationState = ascp.InvocationFailed
		return result
	}
	_ = finalBreakdown // Direct receipts carry the compact provider-neutral billing record.

	state := ascp.InvocationSucceeded
	status := http.StatusOK
	if billingRecord != nil && billingRecord.ReservationRef != "" {
		settlement, settleErr := s.config.Billing.Settle(ctx, billingRecord.ReservationRef, finalPrice, directBillingKey(idempotencyKey, invocationID, "settle"))
		if settleErr != nil {
			billingRecord.State = ascp.ErrSettlementPending
			state = ascp.InvocationAccepted
			status = http.StatusAccepted
			s.appendAudit(invocationID, "ascp.billing.settlement_pending", identity.Actor, map[string]any{"error": settleErr.Error()})
		} else {
			amount := settlement.Amount
			billingRecord.Mode = settlement.Mode
			billingRecord.ArrangementRef = settlement.ArrangementRef
			billingRecord.SettlementRef = settlement.Reference
			billingRecord.InvoiceRef = settlement.InvoiceRef
			billingRecord.PeriodRef = settlement.PeriodRef
			billingRecord.Amount = &amount
			billingRecord.State = settlement.State
			s.appendAudit(invocationID, "ascp.billing.settled", identity.Actor, map[string]any{
				"settlement_ref": settlement.Reference,
				"state":          settlement.State,
				"amount":         settlement.Amount,
			})
		}
	}

	s.appendAudit(invocationID, "ascp.invocation."+string(state), identity.Actor, map[string]any{
		"artifact_count": len(executionResult.Artifacts),
	})
	root, err := s.config.Audit.Root(invocationID)
	if err != nil {
		p := problem(http.StatusInternalServerError, ascp.ErrOutcomeUnknown, "Invocation audit outcome is unknown", "The provider completed the invocation but could not load its audit root.", false)
		p.InvocationID = invocationID
		p.InvocationState = state
		result := problemResult(requestID, p)
		result.InvocationID = invocationID
		result.InvocationState = state
		return result
	}
	receipt := ascp.InvocationReceipt{
		ProtocolVersion: ascp.ProtocolVersion,
		ServiceID:       s.config.Manifest.ServiceID,
		ReceiptID:       ascp.MustNewID("irc"),
		InvocationID:    invocationID,
		Intent:          plan.ResolvedIntent,
		RequestDigest:   requestDigest,
		Outcome:         state,
		Artifacts:       executionResult.Artifacts,
		Billing:         billingRecord,
		AuditRoot:       root,
		CompletedAt:     s.config.Now(),
	}
	unsigned, err := ascp.SigningProjection(receipt)
	if err != nil {
		return directUnknownResult(requestID, invocationID, state, "Invocation receipt projection failed", err.Error())
	}
	receipt.Signature, err = s.config.Signer.SignJSON(unsigned)
	if err != nil {
		return directUnknownResult(requestID, invocationID, state, "Invocation receipt signing failed", err.Error())
	}
	response := ascp.DirectInvocationResponse{
		InvocationID:      invocationID,
		Flow:              ascp.FlowDirect,
		Intent:            plan.ResolvedIntent,
		State:             state,
		Result:            executionResult.Result,
		Artifacts:         executionResult.Artifacts,
		Billing:           billingRecord,
		Receipt:           receipt,
		ServerRequestID:   requestID,
		IdempotencyExpiry: idempotencyExpiry(s.config.Now(), s.config.IdempotencyRetention, idempotencyKey),
	}
	if err := s.config.Store.PutInvocation(InvocationRecord{
		InvocationID:  invocationID,
		Actor:         identity.Actor,
		Principal:     identity.Principal,
		RequestDigest: requestDigest,
		Response:      response,
	}); err != nil {
		return directUnknownResult(requestID, invocationID, state, "Invocation persistence outcome is unknown", err.Error())
	}
	s.appendAudit(invocationID, "ascp.invocation.receipt.issued", identity.Actor, map[string]any{"receipt_id": receipt.ReceiptID, "audit_root": root})
	result := jsonResult(status, response)
	result.InvocationID = invocationID
	result.InvocationState = state
	return result
}

func (s *Server) reserveBilling(
	ctx context.Context,
	identity Identity,
	contractID string,
	bindingDigest string,
	terms ascp.BillingTerms,
	authorization *ascp.BillingAuthorization,
	amount ascp.Money,
	idempotencyKey string,
) (*ascp.BillingRecord, *ascp.Problem) {
	if terms.Mode == ascp.BillingFree {
		zero := amount
		return &ascp.BillingRecord{Mode: ascp.BillingFree, Amount: &zero, State: "not_billed"}, nil
	}
	if p := validateBillingAuthorization(authorization, identity.Principal, s.config.Manifest.ServiceID, bindingDigest, terms, amount, s.config.Now()); p != nil {
		return nil, p
	}
	reservation, err := s.config.Billing.Reserve(ctx, s.config.Manifest.ServiceID, contractID, bindingDigest, terms, authorization, amount, idempotencyKey)
	if err != nil {
		p := billingProblem(err)
		return nil, &p
	}
	reservedAmount := reservation.Amount
	return &ascp.BillingRecord{
		Mode:           reservation.Mode,
		ArrangementRef: reservation.ArrangementRef,
		ReservationRef: reservation.Reference,
		InvoiceRef:     reservation.InvoiceRef,
		PeriodRef:      reservation.PeriodRef,
		Amount:         &reservedAmount,
		State:          reservation.State,
	}, nil
}

func validateDirectPlan(plan DirectPlan) error {
	if strings.TrimSpace(plan.ResolvedIntent) == "" {
		return errors.New("resolved intent is required")
	}
	if len(plan.NormalizedTask) == 0 {
		return errors.New("normalized task is required")
	}
	if len(plan.Effects) == 0 {
		return errors.New("at least one explicit effect or data-access description is required")
	}
	if strings.TrimSpace(plan.RiskClass) == "" {
		return errors.New("risk class is required")
	}
	if err := ascp.ValidateMoney(plan.Price); err != nil {
		return fmt.Errorf("price: %w", err)
	}
	if err := ascp.ValidateMoney(plan.PriceCeiling); err != nil {
		return fmt.Errorf("price ceiling: %w", err)
	}
	comparison, err := ascp.CompareMoney(plan.PriceCeiling, plan.Price)
	if err != nil || comparison < 0 {
		return errors.New("price ceiling is below price")
	}
	if !plan.BillingTerms.VariablePriceAllowed && comparison != 0 {
		return errors.New("fixed direct price and ceiling differ")
	}
	return ascp.ValidateBillingTerms(plan.BillingTerms, plan.Price)
}

func resolveDirectPrice(plan DirectPlan, result DirectExecutionResult) (ascp.Money, []ascp.PriceComponent, error) {
	quote := ascp.Quote{
		Price:          plan.Price,
		PriceCeiling:   plan.PriceCeiling,
		PriceBreakdown: plan.PriceBreakdown,
		BillingTerms:   plan.BillingTerms,
	}
	return resolveFinalCharge(quote, ExecutionResult{
		FinalPrice:          result.FinalPrice,
		FinalPriceBreakdown: result.FinalPriceBreakdown,
	})
}

func validateDirectAuthorization(authorization ascp.AuthorizationEvidence, identity Identity, audience, bindingDigest string, now time.Time) *ascp.Problem {
	if authorization.Type == "" || authorization.Reference == "" || authorization.PrincipalID == "" || authorization.Audience == "" ||
		authorization.BindingDigest == "" || authorization.ApprovedAt.IsZero() || authorization.ExpiresAt.IsZero() {
		p := problem(http.StatusForbidden, ascp.ErrAuthorizationRequired, "Complete authorization evidence required", "Authorization type, reference, principal, audience, binding digest, approval time, and expiry are required.", false)
		return &p
	}
	if authorization.PrincipalID != identity.Principal.ID || authorization.Audience != audience || authorization.BindingDigest != bindingDigest {
		p := problem(http.StatusForbidden, ascp.ErrAuthorizationInvalid, "Authorization is not bound to this invocation", "Principal, service audience, or request digest mismatch.", false)
		return &p
	}
	if !authorization.ExpiresAt.After(authorization.ApprovedAt) || !authorization.ExpiresAt.After(now) || authorization.ApprovedAt.After(now.Add(5*time.Minute)) {
		p := problem(http.StatusForbidden, ascp.ErrAuthorizationInvalid, "Authorization timing is invalid", "Approval must be current and must expire after approval and after the current time.", false)
		return &p
	}
	return nil
}

func (s *Server) validateReferences(identity Identity, contextRefs []ascp.ContextRef, files []ascp.FileRef) *ascp.Problem {
	now := s.config.Now()
	for index, ref := range contextRefs {
		if err := validateContextRef(ref, now); err != nil {
			p := problem(http.StatusUnprocessableEntity, ascp.ErrValidationFailed, "Invalid context reference", "One or more context references are invalid.", false)
			p.FieldErrors = []ascp.FieldError{{Pointer: fmt.Sprintf("/context_refs/%d", index), Code: "invalid_context_ref", Message: err.Error()}}
			return &p
		}
	}
	return s.config.Files.Validate(identity, files, now)
}

func (s *Server) readAuthenticatedJSON(writer http.ResponseWriter, request *http.Request) (string, Identity, []byte, bool) {
	requestID, identity, ok := s.authenticateRead(writer, request)
	if !ok {
		return requestID, Identity{}, nil, false
	}
	if suppliedVersion := request.Header.Get("ASCP-Version"); suppliedVersion != "" && suppliedVersion != ascp.ProtocolVersion {
		writeProblem(writer, requestID, problem(http.StatusBadRequest, "unsupported_version", "Unsupported ASCP version", "The requested protocol version is not supported.", false))
		return requestID, Identity{}, nil, false
	}
	mediaType := request.Header.Get("Content-Type")
	if mediaType != ascp.MediaType && !strings.HasPrefix(mediaType, "application/json") {
		writeProblem(writer, requestID, problem(http.StatusUnsupportedMediaType, ascp.ErrUnsupportedMediaType, "Unsupported media type", "Use application/ascp+json or application/json.", false))
		return requestID, Identity{}, nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(writer, request.Body, s.config.MaximumBodyBytes))
	if err != nil {
		writeProblem(writer, requestID, problem(http.StatusRequestEntityTooLarge, ascp.ErrInvalidRequest, "Request body too large", "The request exceeds the service's maximum JSON body size.", false))
		return requestID, Identity{}, nil, false
	}
	return requestID, identity, body, true
}

func (s *Server) handleInvocationRoutes(writer http.ResponseWriter, request *http.Request) {
	trimmed := strings.Trim(strings.TrimPrefix(request.URL.Path, "/v1/invocations/"), "/")
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 || parts[0] == "" {
		writeProblem(writer, ascp.MustNewID("req"), problem(http.StatusNotFound, ascp.ErrNotFound, "Invocation not found", "An invocation ID is required.", false))
		return
	}
	invocationID := parts[0]
	if len(parts) == 1 && request.Method == http.MethodGet {
		s.handleGetInvocation(writer, request, invocationID)
		return
	}
	if len(parts) == 2 && parts[1] == "audit" && request.Method == http.MethodGet {
		s.handleInvocationAudit(writer, request, invocationID)
		return
	}
	writeProblem(writer, ascp.MustNewID("req"), problem(http.StatusNotFound, ascp.ErrNotFound, "Route not found", "The requested invocation operation is not available.", false))
}

func (s *Server) handleGetInvocation(writer http.ResponseWriter, request *http.Request, invocationID string) {
	requestID, identity, ok := s.authenticateRead(writer, request)
	if !ok {
		return
	}
	record, p := s.authorizedInvocation(identity, invocationID)
	if p != nil {
		writeProblem(writer, requestID, *p)
		return
	}
	writeJSON(writer, http.StatusOK, record.Response, http.Header{"X-Request-ID": []string{requestID}})
}

func (s *Server) handleInvocationAudit(writer http.ResponseWriter, request *http.Request, invocationID string) {
	requestID, identity, ok := s.authenticateRead(writer, request)
	if !ok {
		return
	}
	if _, p := s.authorizedInvocation(identity, invocationID); p != nil {
		writeProblem(writer, requestID, *p)
		return
	}
	events, err := s.config.Audit.List(invocationID, 0)
	if err != nil {
		writeProblem(writer, requestID, problem(http.StatusServiceUnavailable, ascp.ErrServiceUnavailable, "Audit store unavailable", "The service could not load the invocation audit chain.", true))
		return
	}
	root, err := s.config.Audit.Root(invocationID)
	if err != nil {
		writeProblem(writer, requestID, problem(http.StatusServiceUnavailable, ascp.ErrServiceUnavailable, "Audit store unavailable", "The service could not load the invocation audit root.", true))
		return
	}
	writeJSON(writer, http.StatusOK, map[string]any{"invocation_id": invocationID, "events": events, "root": root}, http.Header{"X-Request-ID": []string{requestID}})
}

func (s *Server) authorizedInvocation(identity Identity, invocationID string) (InvocationRecord, *ascp.Problem) {
	record, ok, err := s.config.Store.GetInvocation(invocationID)
	if err != nil {
		p := problem(http.StatusServiceUnavailable, ascp.ErrServiceUnavailable, "Invocation store unavailable", "The service could not determine the invocation state.", true)
		return InvocationRecord{}, &p
	}
	if !ok {
		p := problem(http.StatusNotFound, ascp.ErrNotFound, "Invocation not found", "The invocation does not exist.", false)
		return InvocationRecord{}, &p
	}
	if record.Actor != identity.Actor || record.Principal != identity.Principal {
		p := problem(http.StatusForbidden, ascp.ErrForbidden, "Invocation ownership mismatch", "The invocation belongs to a different delegation.", false)
		return InvocationRecord{}, &p
	}
	return record, nil
}

func directUnknownResult(requestID, invocationID string, state ascp.InvocationState, title, detail string) operationResult {
	p := problem(http.StatusInternalServerError, ascp.ErrOutcomeUnknown, title, detail, false)
	p.InvocationID = invocationID
	p.InvocationState = state
	result := problemResult(requestID, p)
	result.InvocationID = invocationID
	result.InvocationState = state
	return result
}

func directBillingKey(clientKey, invocationID, suffix string) string {
	if clientKey == "" {
		return invocationID + ":" + suffix
	}
	return clientKey + ":" + suffix
}

func idempotencyExpiry(now time.Time, retention time.Duration, key string) *time.Time {
	if key == "" {
		return nil
	}
	expiresAt := now.Add(retention)
	return &expiresAt
}

func writeStoredResponse(writer http.ResponseWriter, response StoredResponse, requestID string) {
	for name, values := range response.Headers {
		for _, value := range values {
			writer.Header().Add(name, value)
		}
	}
	writer.Header().Set("Idempotency-Replayed", "true")
	writer.Header().Set("X-Request-ID", requestID)
	writer.WriteHeader(response.StatusCode)
	_, _ = writer.Write(response.Body)
}
