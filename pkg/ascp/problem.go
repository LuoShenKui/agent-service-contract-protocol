package ascp

import "encoding/json"

// FieldError provides machine-readable validation detail for one input field.
// Pointer uses JSON Pointer syntax so nested fields can be addressed precisely.
type FieldError struct {
	Pointer string `json:"pointer"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Problem follows RFC 9457 and adds ASCP-specific fields needed by autonomous
// clients: a stable error code, retry guidance, request correlation, and field
// validation details. Human-readable Detail must never be parsed as control data.
type Problem struct {
	Type            string                 `json:"type"`
	Title           string                 `json:"title"`
	Status          int                    `json:"status"`
	Detail          string                 `json:"detail,omitempty"`
	Instance        string                 `json:"instance,omitempty"`
	Code            string                 `json:"code"`
	Category        string                 `json:"category"`
	Retryable       bool                   `json:"retryable"`
	RetryAfterMS    int64                  `json:"retry_after_ms,omitempty"`
	RequestID       string                 `json:"request_id,omitempty"`
	TaskID          string                 `json:"task_id,omitempty"`
	TaskState       TaskState              `json:"task_state,omitempty"`
	InvocationID    string                 `json:"invocation_id,omitempty"`
	InvocationState InvocationState        `json:"invocation_state,omitempty"`
	FieldErrors     []FieldError           `json:"field_errors,omitempty"`
	Extensions      map[string]interface{} `json:"extensions,omitempty"`
}

// Error allows Problem to satisfy the error interface in clients and tests.
func (p Problem) Error() string {
	if p.Detail != "" {
		return p.Code + ": " + p.Detail
	}
	return p.Code + ": " + p.Title
}

// DecodeProblem attempts to parse an RFC 9457 response body. It returns the
// original JSON error when the remote response is malformed.
func DecodeProblem(data []byte) (*Problem, error) {
	var problem Problem
	if err := json.Unmarshal(data, &problem); err != nil {
		return nil, err
	}
	return &problem, nil
}

const (
	ErrInvalidRequest         = "invalid_request"
	ErrUnsupportedMediaType   = "unsupported_media_type"
	ErrValidationFailed       = "validation_failed"
	ErrUnauthenticated        = "unauthenticated"
	ErrForbidden              = "forbidden"
	ErrNotFound               = "not_found"
	ErrUnsupportedIntent      = "unsupported_intent"
	ErrCapabilityNotFound     = "capability_not_found"
	ErrContractRequired       = "contract_required"
	ErrDirectNotAllowed       = "direct_execution_not_allowed"
	ErrAdditionalInput        = "additional_input_required"
	ErrOfferExpired           = "offer_expired"
	ErrQuoteExpired           = "quote_expired"
	ErrQuoteMismatch          = "quote_mismatch"
	ErrBudgetExceeded         = "budget_exceeded"
	ErrDeadlineExpired        = "deadline_expired"
	ErrSchedulingUnsupported  = "scheduling_unsupported"
	ErrCallbackUnsupported    = "callback_unsupported"
	ErrAuthorizationRequired  = "authorization_required"
	ErrAuthorizationInvalid   = "authorization_invalid"
	ErrBillingRequired        = "billing_required"
	ErrBillingDeclined        = "billing_declined"
	ErrBillingUnavailable     = "billing_unavailable"
	ErrBillingOutcomeUnknown  = "billing_outcome_unknown"
	ErrBillingModeUnsupported = "billing_mode_unsupported"
	ErrSettlementPending      = "settlement_pending"
	ErrSettlementFailed       = "settlement_failed"
	ErrIdempotencyConflict    = "idempotency_conflict"
	ErrRequestInProgress      = "request_in_progress"
	ErrOutcomeUnknown         = "outcome_unknown"
	ErrPreconditionFailed     = "precondition_failed"
	ErrTaskNotCancellable     = "task_not_cancellable"
	ErrFileNotReady           = "file_not_ready"
	ErrFileRejected           = "file_rejected"
	ErrFileTooLarge           = "file_too_large"
	ErrUploadExpired          = "upload_expired"
	ErrDigestMismatch         = "digest_mismatch"
	ErrRateLimited            = "rate_limited"
	ErrServiceUnavailable     = "service_unavailable"
	ErrInternal               = "internal_error"
)

// Deprecated payment-named aliases preserve source compatibility with the 0.1
// reference implementation. Their wire values are the 0.2 billing codes.
const (
	ErrPaymentRequired    = ErrBillingRequired
	ErrPaymentDeclined    = ErrBillingDeclined
	ErrPaymentUnavailable = ErrBillingUnavailable
)
