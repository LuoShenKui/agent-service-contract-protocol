// Package email implements a platform-owned reference agent. It demonstrates
// two distinct ASCP paths without publishing dozens of low-level mailbox APIs:
//
//   - email.latest.read uses the direct ask-and-answer flow and is free.
//   - email.send uses the signed contract flow, supports attachments, and can be
//     funded through several billing arrangements.
package email

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/mail"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/server"
)

// Message is the demo provider's internal record. A real Gmail-like provider
// would map this structure to its existing message, thread, attachment, and
// delivery systems.
type Message struct {
	ID          string
	TaskID      string
	From        string
	To          string
	Subject     string
	Body        string
	Attachments []ascp.FileRef
	OccurredAt  time.Time
}

// Service keeps messages by provider deduplication key. Execute is idempotent by
// task ID and ExecuteDirect is read-only, so network retries cannot send a second
// email or mutate the mailbox.
type Service struct {
	mu       sync.Mutex
	messages map[string]Message
	inbox    []Message
	now      func() time.Time
}

// NewService creates a demo mailbox with one readable message.
func NewService() *Service {
	now := time.Now().UTC()
	return &Service{
		messages: make(map[string]Message),
		inbox: []Message{{
			ID:         "msg_reference_welcome",
			From:       "service@example.test",
			To:         "demo-user@example.test",
			Subject:    "Welcome to the ASCP reference mailbox",
			Body:       "This message is returned by the one-call email.latest.read capability.",
			OccurredAt: now.Add(-time.Hour),
		}},
		now: func() time.Time { return time.Now().UTC() },
	}
}

// Capabilities returns a compact list. Parameter schemas are intentionally
// absent; a caller requests them only through Options or Negotiate.
func (s *Service) Capabilities(_ context.Context, _ server.Identity, query ascp.CapabilityQuery) (ascp.CapabilityCatalog, *ascp.Problem) {
	all := []ascp.CapabilityDescriptor{
		{
			Intent:             "email.latest.read",
			Version:            "email.latest.read/1",
			Summary:            "Read the newest visible email",
			Description:        "Returns the newest message as a compact inline result and attachment references.",
			Category:           "email",
			DefaultFlow:        ascp.FlowDirect,
			SupportedFlows:     []ascp.Flow{ascp.FlowDirect},
			SideEffectClass:    "read_only",
			RiskClass:          "low_private_data_access",
			RequiredScopes:     []string{"email.read"},
			BillingModes:       []ascp.BillingMode{ascp.BillingFree},
			ParameterNames:     []string{"include_body"},
			OptionsSupported:   true,
			InputFilePolicy:    ascp.FilePolicy{Accepted: false},
			OutputModes:        []string{"inline", "artifact_ref"},
			MaximumInlineBytes: 32 << 10,
		},
		{
			Intent:           "email.send",
			Version:          "email.send/2",
			Summary:          "Send one email with optional attachments",
			Description:      "Creates a signed service contract before irreversible delivery.",
			Category:         "email",
			DefaultFlow:      ascp.FlowContract,
			SupportedFlows:   []ascp.Flow{ascp.FlowContract},
			SideEffectClass:  "irreversible_external_communication",
			RiskClass:        "medium_irreversible_communication",
			RequiredScopes:   []string{"email.send"},
			BillingModes:     supportedSendBillingModes(),
			ParameterNames:   []string{"recipient", "subject", "body"},
			OptionsSupported: true,
			InputFilePolicy:  sendFilePolicy(),
			OutputModes:      []string{"artifact_ref"},
		},
	}

	needle := strings.ToLower(strings.TrimSpace(query.Query))
	intent := strings.ToLower(strings.TrimSpace(query.Intent))
	filtered := make([]ascp.CapabilityDescriptor, 0, len(all))
	for _, capability := range all {
		if intent != "" && !strings.HasPrefix(strings.ToLower(capability.Intent), intent) {
			continue
		}
		if needle != "" {
			haystack := strings.ToLower(capability.Intent + " " + capability.Summary + " " + capability.Description)
			if !strings.Contains(haystack, needle) {
				continue
			}
		}
		filtered = append(filtered, capability)
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].Intent < filtered[j].Intent })

	limit := query.Limit
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	if len(filtered) > limit {
		filtered = filtered[:limit]
	}
	now := s.now()
	return ascp.CapabilityCatalog{
		ServiceID:    "urn:ascp:service:reference-email",
		Revision:     "email-capabilities/2",
		Capabilities: filtered,
		GeneratedAt:  now,
		ExpiresAt:    now.Add(15 * time.Minute),
	}, nil
}

// Options is the optional semantic preflight. It is useful when the caller has
// a goal but does not know whether one-call direct execution is permitted.
func (s *Service) Options(_ context.Context, identity server.Identity, request ascp.OptionsRequest) (ascp.OptionsResponse, *ascp.Problem) {
	intent := resolveIntent(request.Intent, request.Goal)
	now := s.now()
	switch intent {
	case "email.latest.read":
		missing := []string{}
		if !identity.HasScope("email.read") {
			missing = append(missing, "delegated_scope:email.read")
		}
		return ascp.OptionsResponse{
			Supported:         true,
			ResolvedIntent:    intent,
			RecommendedFlow:   ascp.FlowDirect,
			DirectEligible:    len(missing) == 0,
			MissingParameters: missing,
			Parameters: []ascp.ParameterDefinition{{
				Name:        "include_body",
				Description: "Whether to include the small message body inline",
				Required:    false,
				Schema:      json.RawMessage(`{"type":"boolean","default":true}`),
			}},
			RequiredScopes:  []string{"email.read"},
			BillingOptions:  []ascp.BillingOption{{Mode: ascp.BillingFree, SettlementTiming: "none", Description: "No service charge"}},
			InputFilePolicy: ascp.FilePolicy{Accepted: false},
			SchemaVersion:   "email.latest.read/1",
			ExpiresAt:       now.Add(10 * time.Minute),
		}, nil
	case "email.send":
		return ascp.OptionsResponse{
			Supported:       true,
			ResolvedIntent:  intent,
			RecommendedFlow: ascp.FlowContract,
			DirectEligible:  false,
			ReasonCode:      ascp.ErrContractRequired,
			Reason:          "Sending external email is irreversible and requires a signed quote and independent approval.",
			Parameters:      sendParameters(),
			RequiredScopes:  []string{"email.send"},
			BillingOptions:  sendBillingOptions(),
			InputFilePolicy: sendFilePolicy(),
			EstimatedPrice: &ascp.PriceEstimate{
				Minimum: ascp.Money{Currency: "USD", Amount: "0.00"},
				Maximum: ascp.Money{Currency: "USD", Amount: "0.01"},
				Basis:   "one message; subscription or sponsor arrangements may include the fee",
			},
			SchemaVersion: "email.send/2",
			ExpiresAt:     now.Add(10 * time.Minute),
		}, nil
	default:
		return ascp.OptionsResponse{
			Supported:      false,
			DirectEligible: false,
			ReasonCode:     ascp.ErrUnsupportedIntent,
			Reason:         "The reference service supports email.latest.read and email.send.",
		}, nil
	}
}

// PlanDirect proves that the concrete request is safe for one-call execution.
// The reference service permits only a free, read-only mailbox query.
func (s *Service) PlanDirect(_ context.Context, _ server.Identity, request ascp.DirectInvocationRequest) (server.DirectPlan, *ascp.Problem) {
	intent := resolveIntent(request.Intent, request.Goal)
	if intent != "email.latest.read" {
		return server.DirectPlan{
			Eligible:       false,
			ReasonCode:     ascp.ErrContractRequired,
			Reason:         "This intent requires full contract flow.",
			ResolvedIntent: intent,
		}, nil
	}
	if len(request.InputFiles) > 0 {
		return server.DirectPlan{}, validationProblem("/input_files", "email.latest.read does not accept input files")
	}
	includeBody := true
	if value, exists := request.Parameters["include_body"]; exists {
		boolean, ok := value.(bool)
		if !ok {
			return server.DirectPlan{}, validationProblem("/parameters/include_body", "include_body must be a boolean")
		}
		includeBody = boolean
	}
	if len(request.Parameters) > 1 {
		return server.DirectPlan{}, validationProblem("/parameters", "only include_body is accepted")
	}
	zero := ascp.Money{Currency: "USD", Amount: "0.00"}
	return server.DirectPlan{
		Eligible:       true,
		ResolvedIntent: intent,
		NormalizedTask: map[string]any{
			"intent":       intent,
			"include_body": includeBody,
		},
		RequiredScopes: []string{"email.read"},
		BillingTerms: ascp.BillingTerms{
			Mode:             ascp.BillingFree,
			SettlementTiming: "none",
		},
		Price:        zero,
		PriceCeiling: zero,
		Effects: []ascp.Effect{{
			Type:       "email.read",
			Target:     "latest-visible-message",
			Summary:    "Read the newest email visible to the authenticated principal",
			Reversible: true,
		}},
		Permissions: []ascp.PermissionUse{{
			Scope:   "email.read",
			Purpose: "Read the newest visible message",
		}},
		DataUse: ascp.DataUse{
			Categories:        []string{"mailbox_metadata", "message_content", "attachment_metadata"},
			Purposes:          []string{"answer_user_request", "security_audit"},
			RetentionSeconds:  0,
			TrainingAllowed:   false,
			ThirdPartySharing: false,
			Region:            "service-configured",
		},
		RiskClass:           "low_private_data_access",
		IdempotencyRequired: false,
		SLA: ascp.ServiceLevel{
			ExpectedCompletionSeconds: 1,
			MaximumCompletionSeconds:  5,
		},
	}, nil
}

// ExecuteDirect returns one compact answer. Attachment bytes remain behind
// references instead of being copied through the model context.
func (s *Service) ExecuteDirect(_ context.Context, _ server.Identity, _ string, request ascp.DirectInvocationRequest, plan server.DirectPlan) (server.DirectExecutionResult, *ascp.Problem) {
	if plan.ResolvedIntent != "email.latest.read" {
		p := providerProblem(http.StatusPreconditionFailed, ascp.ErrDirectNotAllowed, "direct execution plan is not an email read", false)
		return server.DirectExecutionResult{}, &p
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.inbox) == 0 {
		return server.DirectExecutionResult{Result: map[string]any{"found": false}}, nil
	}
	latest := s.inbox[0]
	for _, candidate := range s.inbox[1:] {
		if candidate.OccurredAt.After(latest.OccurredAt) {
			latest = candidate
		}
	}
	includeBody, _ := plan.NormalizedTask["include_body"].(bool)
	result := map[string]any{
		"found":       true,
		"message_id":  latest.ID,
		"from":        latest.From,
		"to":          latest.To,
		"subject":     latest.Subject,
		"received_at": latest.OccurredAt,
	}
	if includeBody {
		result["body"] = latest.Body
	}
	artifacts := attachmentArtifacts(latest.Attachments)
	return server.DirectExecutionResult{Result: result, Artifacts: artifacts}, nil
}

// Negotiate recognizes email.send for the full contract flow.
func (s *Service) Negotiate(_ context.Context, _ server.Identity, request ascp.NegotiationRequest) (server.CapabilityOffer, *ascp.Problem) {
	intent := resolveIntent(request.Intent, request.Goal)
	if intent != "email.send" {
		return server.CapabilityOffer{
			Supported:  false,
			ReasonCode: "intent_not_supported",
			Reason:     "The contract profile supports email.send; use direct invocation for email.latest.read.",
		}, nil
	}
	return server.CapabilityOffer{
		Supported:       true,
		ResolvedIntent:  "email.send",
		Parameters:      sendParameters(),
		RequiredScopes:  []string{"email.send"},
		BillingOptions:  sendBillingOptions(),
		InputFilePolicy: sendFilePolicy(),
		EstimatedPrice: &ascp.PriceEstimate{
			Minimum: ascp.Money{Currency: "USD", Amount: "0.00"},
			Maximum: ascp.Money{Currency: "USD", Amount: "0.01"},
			Basis:   "one message",
		},
		SchemaVersion: "email.send/2",
		ExpiresAt:     s.now().Add(10 * time.Minute),
	}, nil
}

// Prepare validates provider-specific semantics and returns a binding contract.
// It does not send the message or settle billing.
func (s *Service) Prepare(_ context.Context, _ server.Identity, offer server.StoredOffer, request ascp.PrepareRequest) (server.PreparedContract, *ascp.Problem) {
	recipient, ok := request.Parameters["recipient"].(string)
	if !ok || strings.TrimSpace(recipient) == "" {
		return server.PreparedContract{}, validationProblem("/parameters/recipient", "recipient is required")
	}
	parsed, err := mail.ParseAddress(recipient)
	if err != nil || parsed.Address != recipient {
		return server.PreparedContract{}, validationProblem("/parameters/recipient", "recipient must be a valid canonical email address")
	}

	subject, ok := request.Parameters["subject"].(string)
	if !ok || strings.TrimSpace(subject) == "" || len(subject) > 200 {
		return server.PreparedContract{}, validationProblem("/parameters/subject", "subject must contain 1 to 200 bytes")
	}
	body, ok := request.Parameters["body"].(string)
	if !ok || strings.TrimSpace(body) == "" || len(body) > 10000 {
		return server.PreparedContract{}, validationProblem("/parameters/body", "body must contain 1 to 10000 bytes")
	}
	if len(request.Parameters) != 3 {
		return server.PreparedContract{}, validationProblem("/parameters", "only recipient, subject, and body are accepted")
	}
	if problem := validateAttachmentPolicy(request.InputFiles); problem != nil {
		return server.PreparedContract{}, problem
	}

	mode := request.Billing.Mode
	if mode == "" {
		mode = ascp.BillingPayNow
	}
	terms, price, billingProblem := sendBillingTerms(mode, request.Billing.ArrangementRef)
	if billingProblem != nil {
		return server.PreparedContract{}, billingProblem
	}
	attachmentIDs := make([]string, 0, len(request.InputFiles))
	for _, file := range request.InputFiles {
		attachmentIDs = append(attachmentIDs, file.FileID)
	}

	breakdown := []ascp.PriceComponent{}
	if price.Amount != "0.00" && price.Amount != "0" {
		breakdown = append(breakdown, ascp.PriceComponent{
			Type:        "service_fee",
			Description: "Reference mail delivery fee",
			Amount:      price,
		})
	}
	return server.PreparedContract{
		NormalizedTask: map[string]any{
			"intent":         offer.ResolvedIntent,
			"recipient":      recipient,
			"subject":        subject,
			"body":           body,
			"attachment_ids": attachmentIDs,
			"provider_mode":  "demo",
		},
		Price:          price,
		PriceCeiling:   price,
		PriceBreakdown: breakdown,
		BillingTerms:   terms,
		Effects: []ascp.Effect{{
			Type:       "email.send",
			Target:     recipient,
			Summary:    fmt.Sprintf("Send one email with %d attachment(s) to %s", len(request.InputFiles), recipient),
			Reversible: false,
		}},
		Permissions: []ascp.PermissionUse{{
			Scope:   "email.send",
			Purpose: "Deliver the explicitly quoted message and attachments",
		}},
		DataUse: ascp.DataUse{
			Categories:        []string{"email_address", "message_content", "attachment_content"},
			Purposes:          []string{"message_delivery", "abuse_prevention", "security_audit"},
			RetentionSeconds:  86400,
			TrainingAllowed:   false,
			ThirdPartySharing: false,
			Region:            "service-configured",
		},
		RiskClass: "medium_irreversible_communication",
		Confirmation: ascp.ConfirmationRequirement{
			Required: true,
			Mode:     "explicit_user_or_enterprise_policy",
			Text:     "Send the quoted email. Delivery is irreversible.",
		},
		SLA: ascp.ServiceLevel{
			ExpectedCompletionSeconds: 2,
			MaximumCompletionSeconds:  30,
		},
		ExpiresAt: s.now().Add(5 * time.Minute),
	}, nil
}

// Execute performs the provider-side effect exactly once per stable task ID.
func (s *Service) Execute(_ context.Context, _ server.Identity, task ascp.Task, quote ascp.Quote) (server.ExecutionResult, *ascp.Problem) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if existing, ok := s.messages[task.TaskID]; ok {
		return executionResult(existing), nil
	}

	recipient, _ := quote.NormalizedTask["recipient"].(string)
	subject, _ := quote.NormalizedTask["subject"].(string)
	body, _ := quote.NormalizedTask["body"].(string)
	if recipient == "" || subject == "" || body == "" {
		p := providerProblem(http.StatusInternalServerError, ascp.ErrInternal, "normalized task is incomplete", true)
		return server.ExecutionResult{}, &p
	}

	message := Message{
		ID:          ascp.MustNewID("msg"),
		TaskID:      task.TaskID,
		From:        "demo-user@example.test",
		To:          recipient,
		Subject:     subject,
		Body:        body,
		Attachments: append([]ascp.FileRef(nil), quote.InputFiles...),
		OccurredAt:  s.now(),
	}
	s.messages[task.TaskID] = message
	s.inbox = append(s.inbox, message)
	return executionResult(message), nil
}

// Cancel permits cancellation only before execution starts.
func (s *Service) Cancel(_ context.Context, _ server.Identity, task ascp.Task, _ ascp.CancelRequest) *ascp.Problem {
	if task.State == ascp.TaskAccepted || task.State == ascp.TaskScheduled {
		return nil
	}
	p := providerProblem(http.StatusConflict, ascp.ErrTaskNotCancellable, "email delivery has already started", false)
	return &p
}

func sendParameters() []ascp.ParameterDefinition {
	stringSchema := func(maxLength int) json.RawMessage {
		return json.RawMessage(fmt.Sprintf(`{"type":"string","minLength":1,"maxLength":%d}`, maxLength))
	}
	return []ascp.ParameterDefinition{
		{Name: "recipient", Description: "RFC 5322 recipient address", Required: true, Sensitive: true, Schema: stringSchema(320)},
		{Name: "subject", Description: "Email subject", Required: true, Schema: stringSchema(200)},
		{Name: "body", Description: "Plain-text email body", Required: true, Sensitive: true, Schema: stringSchema(10000)},
	}
}

func sendFilePolicy() ascp.FilePolicy {
	return ascp.FilePolicy{
		Accepted:            true,
		MaximumFiles:        10,
		MaximumFileBytes:    10 << 20,
		MaximumTotalBytes:   25 << 20,
		AllowedMediaTypes:   []string{"text/*", "image/*", "application/pdf", "application/zip", "application/octet-stream"},
		InlineMaximumBytes:  0,
		UploadSupported:     true,
		ReferenceSupported:  true,
		MalwareScanRequired: true,
	}
}

func supportedSendBillingModes() []ascp.BillingMode {
	return []ascp.BillingMode{
		ascp.BillingPayNow,
		ascp.BillingPrepaidBalance,
		ascp.BillingSubscription,
		ascp.BillingPostpaid,
		ascp.BillingMonthlyInvoice,
		ascp.BillingClearing,
		ascp.BillingSponsored,
		ascp.BillingExternal,
	}
}

func sendBillingOptions() []ascp.BillingOption {
	return []ascp.BillingOption{
		{Mode: ascp.BillingPayNow, AuthorizationRequired: true, SettlementTiming: "after_success", Description: "Reserve at commit and settle after delivery"},
		{Mode: ascp.BillingPrepaidBalance, ArrangementRequired: true, SettlementTiming: "after_success", Description: "Deduct from an existing prepaid balance"},
		{Mode: ascp.BillingSubscription, ArrangementRequired: true, SettlementTiming: "periodic", Description: "Record usage against an included subscription"},
		{Mode: ascp.BillingPostpaid, ArrangementRequired: true, SettlementTiming: "periodic", Description: "Record usage against a postpaid account"},
		{Mode: ascp.BillingMonthlyInvoice, ArrangementRequired: true, SettlementTiming: "invoice", Description: "Append a line item to the monthly invoice"},
		{Mode: ascp.BillingClearing, ArrangementRequired: true, SettlementTiming: "periodic", Description: "Settle through a unified platform clearing account"},
		{Mode: ascp.BillingSponsored, ArrangementRequired: true, SettlementTiming: "periodic", Description: "Charge a sponsor relationship"},
		{Mode: ascp.BillingExternal, ArrangementRequired: true, SettlementTiming: "external", Description: "The provider records service while settlement occurs outside ASCP"},
	}
}

func sendBillingTerms(mode ascp.BillingMode, arrangementRef string) (ascp.BillingTerms, ascp.Money, *ascp.Problem) {
	price := ascp.Money{Currency: "USD", Amount: "0.01"}
	terms := ascp.BillingTerms{Mode: mode, UsageUnit: "message"}
	switch mode {
	case ascp.BillingPayNow:
		terms.AuthorizationRequired = true
		terms.SettlementTiming = "after_success"
		terms.AcceptedSchemes = []string{"urn:ascp:billing:mock-pay-now"}
		terms.AuthorizationMode = "tokenized_per_call"
		terms.CaptureMode = "settle_on_success"
	case ascp.BillingPrepaidBalance:
		terms.ArrangementRequired = true
		terms.ArrangementRef = arrangementRef
		terms.SettlementTiming = "after_success"
	case ascp.BillingSubscription:
		terms.ArrangementRequired = true
		terms.ArrangementRef = arrangementRef
		terms.SettlementTiming = "periodic"
		terms.BillingPeriod = "provider_plan_period"
		price.Amount = "0.00"
	case ascp.BillingPostpaid:
		terms.ArrangementRequired = true
		terms.ArrangementRef = arrangementRef
		terms.SettlementTiming = "periodic"
		terms.BillingPeriod = "provider_account_period"
	case ascp.BillingMonthlyInvoice:
		terms.ArrangementRequired = true
		terms.ArrangementRef = arrangementRef
		terms.SettlementTiming = "invoice"
		terms.BillingPeriod = "P1M"
	case ascp.BillingClearing:
		terms.ArrangementRequired = true
		terms.ArrangementRef = arrangementRef
		terms.SettlementTiming = "periodic"
	case ascp.BillingSponsored:
		terms.ArrangementRequired = true
		terms.ArrangementRef = arrangementRef
		terms.SettlementTiming = "periodic"
		price.Amount = "0.00"
	case ascp.BillingExternal:
		terms.ArrangementRequired = true
		terms.ArrangementRef = arrangementRef
		terms.SettlementTiming = "external"
	default:
		p := providerProblem(http.StatusUnprocessableEntity, ascp.ErrBillingModeUnsupported, "selected billing mode is not supported for email.send", false)
		return ascp.BillingTerms{}, ascp.Money{}, &p
	}
	if terms.ArrangementRequired && strings.TrimSpace(arrangementRef) == "" {
		p := providerProblem(http.StatusPreconditionRequired, ascp.ErrBillingRequired, "the selected billing mode requires billing.arrangement_ref", false)
		p.FieldErrors = []ascp.FieldError{{Pointer: "/billing/arrangement_ref", Code: "required", Message: "arrangement_ref is required for this billing mode"}}
		return ascp.BillingTerms{}, ascp.Money{}, &p
	}
	return terms, price, nil
}

func validateAttachmentPolicy(files []ascp.FileRef) *ascp.Problem {
	policy := sendFilePolicy()
	if len(files) > policy.MaximumFiles {
		return validationProblem("/input_files", "too many attachments")
	}
	var total int64
	for index, file := range files {
		if err := ascp.ValidateFileRef(file); err != nil {
			return validationProblem(fmt.Sprintf("/input_files/%d", index), err.Error())
		}
		if file.Size > policy.MaximumFileBytes {
			return validationProblem(fmt.Sprintf("/input_files/%d/size", index), "attachment exceeds the per-file limit")
		}
		total += file.Size
	}
	if total > policy.MaximumTotalBytes {
		return validationProblem("/input_files", "attachments exceed the total size limit")
	}
	return nil
}

func executionResult(message Message) server.ExecutionResult {
	artifacts := []ascp.ArtifactRef{{
		URI:          "email://messages/" + message.ID,
		MediaType:    "message/rfc822",
		Name:         "sent-message.eml",
		Disposition:  "inline",
		Relationship: "primary_result",
	}}
	artifacts = append(artifacts, attachmentArtifacts(message.Attachments)...)
	return server.ExecutionResult{
		Artifacts: artifacts,
		Metadata:  map[string]any{"sent_at": message.OccurredAt},
	}
}

func attachmentArtifacts(files []ascp.FileRef) []ascp.ArtifactRef {
	artifacts := make([]ascp.ArtifactRef, 0, len(files))
	for _, file := range files {
		artifacts = append(artifacts, ascp.ArtifactRef{
			URI:          file.URI,
			MediaType:    file.MediaType,
			Digest:       file.Digest,
			Size:         file.Size,
			Name:         file.Name,
			Disposition:  file.Disposition,
			Relationship: "attachment",
			ExpiresAt:    optionalTime(file.ExpiresAt),
		})
	}
	return artifacts
}

func optionalTime(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	copy := value
	return &copy
}

func resolveIntent(intent, goal string) string {
	if strings.TrimSpace(intent) != "" {
		return strings.TrimSpace(intent)
	}
	lower := strings.ToLower(goal)
	if strings.Contains(lower, "latest") || strings.Contains(lower, "newest") || strings.Contains(lower, "最新") {
		return "email.latest.read"
	}
	if strings.Contains(lower, "send") || strings.Contains(lower, "发送") || strings.Contains(lower, "发一封") {
		return "email.send"
	}
	return ""
}

func validationProblem(pointer, message string) *ascp.Problem {
	p := providerProblem(http.StatusUnprocessableEntity, ascp.ErrValidationFailed, message, false)
	p.FieldErrors = []ascp.FieldError{{Pointer: pointer, Code: "invalid", Message: message}}
	return &p
}

func providerProblem(status int, code, detail string, retryable bool) ascp.Problem {
	return ascp.Problem{
		Type:      "urn:ascp:problem:" + code,
		Title:     strings.ReplaceAll(code, "_", " "),
		Status:    status,
		Detail:    detail,
		Code:      code,
		Category:  "service",
		Retryable: retryable,
	}
}
