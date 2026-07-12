// Package ascp contains the protocol data model shared by ASCP clients and
// services. The wire model is independent from any particular LLM, agent
// framework, billing rail, storage engine, or platform-specific API.
package ascp

import (
	"encoding/json"
	"time"
)

const (
	// ProtocolName is the stable human-readable protocol name.
	ProtocolName = "Agent Service Contract Protocol"

	// ProtocolVersion is the current wire-level protocol version implemented by
	// this repository. Version 0.2 adds direct invocation, compact capability
	// discovery, standing billing arrangements, and file-transfer references.
	ProtocolVersion = "0.2"

	// MediaType is the preferred JSON media type. Implementations should also
	// accept application/json for compatibility with ordinary HTTP clients.
	MediaType = "application/ascp+json"
)

// Flow identifies the interaction path selected for a task.
type Flow string

const (
	// FlowDirect is the compact request/response path. It is intended for tasks
	// whose parameters, authority, risk, and billing arrangement are already
	// sufficient to execute without a separately accepted quote.
	FlowDirect Flow = "direct"

	// FlowContract is the negotiate/prepare/commit path used when a binding quote,
	// additional confirmation, variable pricing, or stronger risk controls are
	// required before execution.
	FlowContract Flow = "contract"
)

// Money represents an exact monetary amount. Amount is a base-10 string rather
// than a floating-point number, preventing rounding errors in quotes, invoices,
// credit reservations, and settlement records.
type Money struct {
	Currency string `json:"currency"`
	Amount   string `json:"amount"`
}

// EntityRef identifies a user, organization, agent, service account, or other
// actor without exposing the provider's internal database representation.
type EntityRef struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

// ContextRef is an opaque, scoped reference to data that remains at its source.
// It prevents an entire mailbox thread, document, video, or database object from
// being copied through the calling model's context merely to use one fact.
type ContextRef struct {
	URI       string    `json:"uri"`
	MediaType string    `json:"media_type,omitempty"`
	Digest    string    `json:"digest,omitempty"`
	Size      int64     `json:"size,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
	Purpose   string    `json:"purpose,omitempty"`
}

// FileRef identifies an input file already staged for the service. Large bytes
// do not travel in ASCP JSON. The service resolves FileID or URI through its
// controlled file store and verifies owner, digest, size, readiness, and scan
// state before the file can influence an invocation or quote.
type FileRef struct {
	FileID      string    `json:"file_id"`
	URI         string    `json:"uri"`
	Name        string    `json:"name"`
	MediaType   string    `json:"media_type"`
	Size        int64     `json:"size"`
	Digest      string    `json:"digest"`
	Disposition string    `json:"disposition,omitempty"`
	Purpose     string    `json:"purpose,omitempty"`
	State       string    `json:"state"`
	ScanStatus  string    `json:"scan_status"`
	ExpiresAt   time.Time `json:"expires_at,omitempty"`
}

// FilePolicy advertises the bounded file behavior of one capability. It is a
// compact policy summary, not a generic object-storage API definition.
type FilePolicy struct {
	Accepted            bool     `json:"accepted"`
	MaximumFiles        int      `json:"maximum_files,omitempty"`
	MaximumFileBytes    int64    `json:"maximum_file_bytes,omitempty"`
	MaximumTotalBytes   int64    `json:"maximum_total_bytes,omitempty"`
	AllowedMediaTypes   []string `json:"allowed_media_types,omitempty"`
	InlineMaximumBytes  int64    `json:"inline_maximum_bytes,omitempty"`
	UploadSupported     bool     `json:"upload_supported,omitempty"`
	ReferenceSupported  bool     `json:"reference_supported,omitempty"`
	MalwareScanRequired bool     `json:"malware_scan_required,omitempty"`
}

// FileUploadRequest starts a scoped upload. Digest and size are declared before
// bytes are accepted so a service can reject oversized or disallowed content
// without first ingesting the entire body.
type FileUploadRequest struct {
	RequestID string    `json:"request_id,omitempty"`
	Name      string    `json:"name"`
	MediaType string    `json:"media_type"`
	Size      int64     `json:"size"`
	Digest    string    `json:"digest"`
	Purpose   string    `json:"purpose,omitempty"`
	ExpiresAt time.Time `json:"expires_at,omitempty"`
}

// FileUploadTicket authorizes exactly one bounded upload target. UploadToken is
// a short-lived bearer secret and must never be written to general application
// logs. RequiredHeaders are safe, non-secret upload requirements.
type FileUploadTicket struct {
	FileID          string            `json:"file_id"`
	UploadURL       string            `json:"upload_url"`
	UploadMethod    string            `json:"upload_method"`
	UploadToken     string            `json:"upload_token"`
	RequiredHeaders map[string]string `json:"required_headers,omitempty"`
	MaximumBytes    int64             `json:"maximum_bytes"`
	ExpiresAt       time.Time         `json:"expires_at"`
}

// ParameterDefinition is the small task-specific contract returned only when a
// client asks for options or enters the contract flow. Capability catalog pages
// deliberately avoid loading all parameter schemas into the model context.
type ParameterDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Required    bool            `json:"required"`
	Sensitive   bool            `json:"sensitive,omitempty"`
	Schema      json.RawMessage `json:"schema"`
}

// CapabilityDescriptor is a compact, cacheable description of what a platform
// agent can do. It contains routing and safety hints but not the provider's full
// internal API surface or every JSON Schema definition.
type CapabilityDescriptor struct {
	Intent             string        `json:"intent"`
	Version            string        `json:"version"`
	Summary            string        `json:"summary"`
	Description        string        `json:"description,omitempty"`
	Category           string        `json:"category,omitempty"`
	DefaultFlow        Flow          `json:"default_flow"`
	SupportedFlows     []Flow        `json:"supported_flows"`
	SideEffectClass    string        `json:"side_effect_class"`
	RiskClass          string        `json:"risk_class"`
	RequiredScopes     []string      `json:"required_scopes,omitempty"`
	BillingModes       []BillingMode `json:"billing_modes"`
	ParameterNames     []string      `json:"parameter_names,omitempty"`
	OptionsSupported   bool          `json:"options_supported"`
	InputFilePolicy    FilePolicy    `json:"input_file_policy"`
	OutputModes        []string      `json:"output_modes,omitempty"`
	MaximumInlineBytes int64         `json:"maximum_inline_bytes,omitempty"`
	Deprecated         bool          `json:"deprecated,omitempty"`
	DocumentationURI   string        `json:"documentation_uri,omitempty"`
}

// CapabilityQuery controls catalog filtering and pagination. Query is a short
// search phrase; Intent performs exact or prefix filtering depending on the
// provider's documented behavior.
type CapabilityQuery struct {
	Query  string `json:"query,omitempty"`
	Intent string `json:"intent,omitempty"`
	Cursor string `json:"cursor,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

// CapabilityCatalog is the paginated platform-owned list of supported tasks.
// Revision and ETag allow clients to cache it without repeatedly sending the
// same capability descriptions to a model.
type CapabilityCatalog struct {
	ServiceID    string                 `json:"service_id"`
	Revision     string                 `json:"revision"`
	Capabilities []CapabilityDescriptor `json:"capabilities"`
	NextCursor   string                 `json:"next_cursor,omitempty"`
	GeneratedAt  time.Time              `json:"generated_at"`
	ExpiresAt    time.Time              `json:"expires_at"`
}

// OptionsRequest is the optional preflight for callers that do not know whether
// an operation can use the direct path or needs a signed contract. It is
// side-effect-free and does not itself create authority or billing obligations.
type OptionsRequest struct {
	RequestID   string         `json:"request_id,omitempty"`
	Intent      string         `json:"intent,omitempty"`
	Goal        string         `json:"goal,omitempty"`
	Parameters  map[string]any `json:"parameters,omitempty"`
	ContextRefs []ContextRef   `json:"context_refs,omitempty"`
	InputFiles  []FileRef      `json:"input_files,omitempty"`
	Locale      string         `json:"locale,omitempty"`
}

// OptionsResponse tells the client which flow is currently appropriate and
// returns only the missing or relevant task-specific requirements.
type OptionsResponse struct {
	Supported         bool                  `json:"supported"`
	ResolvedIntent    string                `json:"resolved_intent,omitempty"`
	RecommendedFlow   Flow                  `json:"recommended_flow,omitempty"`
	DirectEligible    bool                  `json:"direct_eligible"`
	ReasonCode        string                `json:"reason_code,omitempty"`
	Reason            string                `json:"reason,omitempty"`
	Parameters        []ParameterDefinition `json:"parameters,omitempty"`
	MissingParameters []string              `json:"missing_parameters,omitempty"`
	RequiredScopes    []string              `json:"required_scopes,omitempty"`
	BillingOptions    []BillingOption       `json:"billing_options,omitempty"`
	InputFilePolicy   FilePolicy            `json:"input_file_policy"`
	EstimatedPrice    *PriceEstimate        `json:"estimated_price,omitempty"`
	SchemaVersion     string                `json:"schema_version,omitempty"`
	ExpiresAt         time.Time             `json:"expires_at,omitempty"`
	ServerRequestID   string                `json:"server_request_id"`
}

// BillingMode describes how service value is funded or accounted for. Pay-now
// is only one option; standing arrangements can settle before, after, or outside
// the individual service call.
type BillingMode string

const (
	BillingFree           BillingMode = "free"
	BillingPayNow         BillingMode = "pay_now"
	BillingPrepaidBalance BillingMode = "prepaid_balance"
	BillingSubscription   BillingMode = "subscription"
	BillingPostpaid       BillingMode = "postpaid_account"
	BillingMonthlyInvoice BillingMode = "monthly_invoice"
	BillingClearing       BillingMode = "clearing_account"
	BillingSponsored      BillingMode = "sponsored"
	BillingExternal       BillingMode = "external_settlement"
)

// BillingOption is a non-binding catalog or options response. The signed
// BillingTerms inside a quote remain authoritative for the contract flow.
type BillingOption struct {
	Mode                  BillingMode `json:"mode"`
	ArrangementRequired   bool        `json:"arrangement_required,omitempty"`
	AuthorizationRequired bool        `json:"authorization_required,omitempty"`
	SettlementTiming      string      `json:"settlement_timing,omitempty"`
	Description           string      `json:"description,omitempty"`
}

// BillingPreference lets the client select an existing commercial relationship
// without sending card, bank, wallet, or reusable credential material.
type BillingPreference struct {
	Mode           BillingMode `json:"mode,omitempty"`
	ArrangementRef string      `json:"arrangement_ref,omitempty"`
	MaximumAmount  *Money      `json:"maximum_amount,omitempty"`
}

// BillingTerms are the signed settlement conditions. ArrangementRef identifies
// a subscription, prepaid wallet, enterprise account, invoice agreement,
// sponsor, or clearing relationship known to the provider.
type BillingTerms struct {
	Mode                  BillingMode `json:"mode"`
	ArrangementRef        string      `json:"arrangement_ref,omitempty"`
	ArrangementRequired   bool        `json:"arrangement_required,omitempty"`
	AuthorizationRequired bool        `json:"authorization_required,omitempty"`
	SettlementTiming      string      `json:"settlement_timing"`
	AcceptedSchemes       []string    `json:"accepted_schemes,omitempty"`
	AuthorizationMode     string      `json:"authorization_mode,omitempty"`
	CaptureMode           string      `json:"capture_mode,omitempty"`
	RefundPolicyURI       string      `json:"refund_policy_uri,omitempty"`
	VariablePriceAllowed  bool        `json:"variable_price_allowed,omitempty"`
	BillingPeriod         string      `json:"billing_period,omitempty"`
	UsageUnit             string      `json:"usage_unit,omitempty"`
}

// BillingAuthorization is a tokenized authorization or standing mandate
// reference. MaximumAmount is optional for non-monetary subscription usage, but
// mandatory for pay-now and any arrangement that reserves a monetary ceiling.
type BillingAuthorization struct {
	Mode             BillingMode `json:"mode"`
	ArrangementRef   string      `json:"arrangement_ref,omitempty"`
	AuthorizationRef string      `json:"authorization_ref,omitempty"`
	Payer            EntityRef   `json:"payer"`
	Audience         string      `json:"audience"`
	MaximumAmount    *Money      `json:"maximum_amount,omitempty"`
	ExpiresAt        time.Time   `json:"expires_at,omitempty"`
	BindingDigest    string      `json:"binding_digest"`
	Usage            string      `json:"usage,omitempty"`
}

// BillingRecord is the provider-neutral result of reserving, recording, or
// settling value. InvoiceRef and PeriodRef are useful for postpaid and periodic
// arrangements where no immediate card capture exists.
type BillingRecord struct {
	Mode           BillingMode `json:"mode"`
	ArrangementRef string      `json:"arrangement_ref,omitempty"`
	ReservationRef string      `json:"reservation_ref,omitempty"`
	SettlementRef  string      `json:"settlement_ref,omitempty"`
	InvoiceRef     string      `json:"invoice_ref,omitempty"`
	PeriodRef      string      `json:"period_ref,omitempty"`
	Amount         *Money      `json:"amount,omitempty"`
	State          string      `json:"state"`
}

// NegotiationRequest begins the full contract flow. Actor and Principal remain
// explicit even though authentication also binds them, preventing a confused
// deputy from silently changing who requested or benefits from the operation.
type NegotiationRequest struct {
	RequestID   string                 `json:"request_id,omitempty"`
	Intent      string                 `json:"intent,omitempty"`
	Goal        string                 `json:"goal"`
	Actor       EntityRef              `json:"actor"`
	Principal   EntityRef              `json:"principal"`
	Constraints map[string]any         `json:"constraints,omitempty"`
	Budget      *Money                 `json:"budget,omitempty"`
	ContextRefs []ContextRef           `json:"context_refs,omitempty"`
	InputFiles  []FileRef              `json:"input_files,omitempty"`
	Locale      string                 `json:"locale,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

// NegotiationResponse is the first full-contract handshake result. When
// supported, OfferID and Parameters define the minimum information needed for
// Prepare; it never publishes the provider's complete internal API surface.
type NegotiationResponse struct {
	NegotiationID     string                `json:"negotiation_id"`
	Supported         bool                  `json:"supported"`
	Conditional       bool                  `json:"conditional,omitempty"`
	ReasonCode        string                `json:"reason_code,omitempty"`
	Reason            string                `json:"reason,omitempty"`
	OfferID           string                `json:"offer_id,omitempty"`
	ResolvedIntent    string                `json:"resolved_intent,omitempty"`
	Parameters        []ParameterDefinition `json:"parameters,omitempty"`
	RequiredScopes    []string              `json:"required_scopes,omitempty"`
	BillingOptions    []BillingOption       `json:"billing_options,omitempty"`
	InputFilePolicy   FilePolicy            `json:"input_file_policy"`
	EstimatedPrice    *PriceEstimate        `json:"estimated_price,omitempty"`
	OfferExpiresAt    time.Time             `json:"offer_expires_at,omitempty"`
	SchemaVersion     string                `json:"schema_version,omitempty"`
	ServerRequestID   string                `json:"server_request_id"`
	IdempotencyExpiry time.Time             `json:"idempotency_expires_at"`
}

// PriceEstimate is non-binding and intentionally distinct from a signed Quote.
type PriceEstimate struct {
	Minimum Money  `json:"minimum"`
	Maximum Money  `json:"maximum"`
	Basis   string `json:"basis,omitempty"`
}

// PrepareRequest provides the concrete task parameters. This phase MUST remain
// side-effect-free; it validates, prices, and creates a signed contract only.
type PrepareRequest struct {
	RequestID      string                 `json:"request_id,omitempty"`
	OfferID        string                 `json:"offer_id"`
	SchemaVersion  string                 `json:"schema_version"`
	Parameters     map[string]any         `json:"parameters"`
	ContextRefs    []ContextRef           `json:"context_refs,omitempty"`
	InputFiles     []FileRef              `json:"input_files,omitempty"`
	Execution      ExecutionPreferences   `json:"execution,omitempty"`
	Billing        BillingPreference      `json:"billing,omitempty"`
	Callback       *CallbackConfiguration `json:"callback,omitempty"`
	ClientMetadata map[string]any         `json:"client_metadata,omitempty"`
}

// ExecutionPreferences capture temporal, output, and cost constraints that are
// part of the contract or direct invocation plan.
type ExecutionPreferences struct {
	NotBefore          *time.Time `json:"not_before,omitempty"`
	Deadline           *time.Time `json:"deadline,omitempty"`
	MaxPrice           *Money     `json:"max_price,omitempty"`
	AllowPartial       bool       `json:"allow_partial,omitempty"`
	RequireHumanReview bool       `json:"require_human_review,omitempty"`
	MaximumInlineBytes int64      `json:"maximum_inline_bytes,omitempty"`
	PreferArtifactRefs bool       `json:"prefer_artifact_refs,omitempty"`
}

// CallbackConfiguration declares an optional event sink. Production services
// must validate ownership, sign events, prevent SSRF, and avoid embedding
// reusable callback credentials directly in this object.
type CallbackConfiguration struct {
	URL        string   `json:"url"`
	Events     []string `json:"events,omitempty"`
	SecretRef  string   `json:"secret_ref,omitempty"`
	AuthScheme string   `json:"auth_scheme,omitempty"`
}

// Effect describes a potential external side effect for human and policy review.
type Effect struct {
	Type       string `json:"type"`
	Target     string `json:"target,omitempty"`
	Summary    string `json:"summary"`
	Reversible bool   `json:"reversible"`
}

// PermissionUse explains each delegated permission that will be exercised.
type PermissionUse struct {
	Scope   string `json:"scope"`
	Purpose string `json:"purpose"`
}

// DataUse makes access, retention, training, and sharing expectations part of a
// machine-verifiable service contract or direct-invocation receipt.
type DataUse struct {
	Categories        []string `json:"categories,omitempty"`
	Purposes          []string `json:"purposes,omitempty"`
	RetentionSeconds  int64    `json:"retention_seconds,omitempty"`
	TrainingAllowed   bool     `json:"training_allowed"`
	ThirdPartySharing bool     `json:"third_party_sharing"`
	Region            string   `json:"region,omitempty"`
}

// ServiceLevel records binding or descriptive timing commitments.
type ServiceLevel struct {
	ExpectedCompletionSeconds int64 `json:"expected_completion_seconds,omitempty"`
	MaximumCompletionSeconds  int64 `json:"maximum_completion_seconds,omitempty"`
	AvailabilityBasisPoints   int64 `json:"availability_basis_points,omitempty"`
}

// ConfirmationRequirement specifies whether separate human or policy approval
// is required before contract commit. Direct flow is permitted only when this
// requirement is already satisfied by a standing mandate or is not required.
type ConfirmationRequirement struct {
	Required bool   `json:"required"`
	Mode     string `json:"mode,omitempty"`
	Text     string `json:"text,omitempty"`
}

// Signature is a compact JWS over the exact unsigned JSON document emitted by
// the service. The embedded JWS payload is authoritative.
type Signature struct {
	Algorithm     string    `json:"algorithm"`
	KeyID         string    `json:"key_id"`
	CreatedAt     time.Time `json:"created_at"`
	PayloadDigest string    `json:"payload_digest"`
	JWS           string    `json:"jws"`
}

// Quote is the immutable signed contract produced by Prepare. Commit binds to
// QuoteID and PayloadDigest to prevent price, permission, file, callback, or
// task-content changes between review and execution.
type Quote struct {
	ProtocolVersion string                  `json:"protocol_version"`
	ServiceID       string                  `json:"service_id"`
	QuoteID         string                  `json:"quote_id"`
	OfferID         string                  `json:"offer_id"`
	Intent          string                  `json:"intent"`
	Principal       EntityRef               `json:"principal"`
	Actor           EntityRef               `json:"actor"`
	NormalizedTask  map[string]any          `json:"normalized_task"`
	ContextRefs     []ContextRef            `json:"context_refs,omitempty"`
	InputFiles      []FileRef               `json:"input_files,omitempty"`
	Callback        *CallbackConfiguration  `json:"callback,omitempty"`
	Price           Money                   `json:"price"`
	PriceCeiling    Money                   `json:"price_ceiling"`
	PriceBreakdown  []PriceComponent        `json:"price_breakdown,omitempty"`
	BillingTerms    BillingTerms            `json:"billing_terms"`
	Effects         []Effect                `json:"effects"`
	Permissions     []PermissionUse         `json:"permissions,omitempty"`
	DataUse         DataUse                 `json:"data_use"`
	RiskClass       string                  `json:"risk_class"`
	Confirmation    ConfirmationRequirement `json:"confirmation"`
	SLA             ServiceLevel            `json:"sla,omitempty"`
	Execution       ExecutionPreferences    `json:"execution,omitempty"`
	IssuedAt        time.Time               `json:"issued_at"`
	ExpiresAt       time.Time               `json:"expires_at"`
	RevocableUntil  *time.Time              `json:"revocable_until,omitempty"`
	Signature       Signature               `json:"signature"`
}

// PriceComponent itemizes service fees, taxes, usage, credits, tips, or other
// monetary components without using binary floating-point arithmetic.
type PriceComponent struct {
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	Amount      Money  `json:"amount"`
}

// AuthorizationEvidence references independent human approval or an enterprise
// policy decision. Natural-language task content can never create authority.
type AuthorizationEvidence struct {
	Type          string    `json:"type"`
	Reference     string    `json:"reference"`
	PrincipalID   string    `json:"principal_id"`
	Audience      string    `json:"audience"`
	ApprovedAt    time.Time `json:"approved_at"`
	ExpiresAt     time.Time `json:"expires_at"`
	BindingDigest string    `json:"binding_digest"`
	PolicyVersion string    `json:"policy_version,omitempty"`
}

// CommitRequest accepts a signed quote. BillingAuthorization is optional when
// the signed BillingTerms select free service or a standing arrangement that
// does not require per-call authorization.
type CommitRequest struct {
	RequestID            string                 `json:"request_id,omitempty"`
	QuoteID              string                 `json:"quote_id"`
	QuoteDigest          string                 `json:"quote_digest"`
	Authorization        AuthorizationEvidence  `json:"authorization"`
	BillingAuthorization *BillingAuthorization  `json:"billing_authorization,omitempty"`
	ClientTaskID         string                 `json:"client_task_id,omitempty"`
	Metadata             map[string]interface{} `json:"metadata,omitempty"`
}

// DirectInvocationRequest is the compact ask-and-answer path. Actor and
// principal are taken from the authenticated delegation rather than repeated in
// every request. The service must switch to contract flow when terms are not
// fully determined or separate confirmation is required.
type DirectInvocationRequest struct {
	RequestID            string                 `json:"request_id,omitempty"`
	Intent               string                 `json:"intent,omitempty"`
	Goal                 string                 `json:"goal,omitempty"`
	Parameters           map[string]any         `json:"parameters,omitempty"`
	ContextRefs          []ContextRef           `json:"context_refs,omitempty"`
	InputFiles           []FileRef              `json:"input_files,omitempty"`
	Execution            ExecutionPreferences   `json:"execution,omitempty"`
	Billing              BillingPreference      `json:"billing,omitempty"`
	Authorization        *AuthorizationEvidence `json:"authorization,omitempty"`
	BillingAuthorization *BillingAuthorization  `json:"billing_authorization,omitempty"`
	ClientMetadata       map[string]any         `json:"client_metadata,omitempty"`
}

// InvocationState is intentionally smaller than the durable task state machine.
type InvocationState string

const (
	InvocationSucceeded InvocationState = "succeeded"
	InvocationAccepted  InvocationState = "accepted"
	InvocationFailed    InvocationState = "failed"
)

// DirectInvocationResponse is returned by the one-call path. Small results may
// be inline; large or sensitive data remains behind scoped artifact references.
type DirectInvocationResponse struct {
	InvocationID      string            `json:"invocation_id"`
	Flow              Flow              `json:"flow"`
	Intent            string            `json:"intent"`
	State             InvocationState   `json:"state"`
	Result            map[string]any    `json:"result,omitempty"`
	Artifacts         []ArtifactRef     `json:"artifacts,omitempty"`
	Billing           *BillingRecord    `json:"billing,omitempty"`
	Receipt           InvocationReceipt `json:"receipt"`
	ServerRequestID   string            `json:"server_request_id"`
	IdempotencyExpiry *time.Time        `json:"idempotency_expires_at,omitempty"`
}

// InvocationReceipt is the signed evidence for a direct invocation. It binds the
// exact request digest, outcome, artifacts, billing record, and audit root even
// though no separately accepted quote exists.
type InvocationReceipt struct {
	ProtocolVersion string          `json:"protocol_version"`
	ServiceID       string          `json:"service_id"`
	ReceiptID       string          `json:"receipt_id"`
	InvocationID    string          `json:"invocation_id"`
	Intent          string          `json:"intent"`
	RequestDigest   string          `json:"request_digest"`
	Outcome         InvocationState `json:"outcome"`
	Artifacts       []ArtifactRef   `json:"artifacts,omitempty"`
	Billing         *BillingRecord  `json:"billing,omitempty"`
	AuditRoot       string          `json:"audit_root"`
	CompletedAt     time.Time       `json:"completed_at"`
	Signature       Signature       `json:"signature"`
}

// TaskState is a closed set of durable contract-task lifecycle states.
type TaskState string

const (
	TaskAccepted       TaskState = "accepted"
	TaskScheduled      TaskState = "scheduled"
	TaskRunning        TaskState = "running"
	TaskWaitingInput   TaskState = "waiting_input"
	TaskWaitingRequote TaskState = "waiting_requote"
	TaskCancelling     TaskState = "cancelling"
	TaskCancelled      TaskState = "cancelled"
	TaskSucceeded      TaskState = "succeeded"
	TaskFailed         TaskState = "failed"
	TaskCompensating   TaskState = "compensating"
	TaskCompensated    TaskState = "compensated"
	TaskDisputed       TaskState = "disputed"
)

// ArtifactRef describes a result without forcing large content through the JSON
// response or model context. Retrieval uses a separate scoped authorization
// flow and may expire independently from the receipt.
type ArtifactRef struct {
	URI          string     `json:"uri"`
	MediaType    string     `json:"media_type,omitempty"`
	Digest       string     `json:"digest,omitempty"`
	Size         int64      `json:"size,omitempty"`
	Name         string     `json:"name,omitempty"`
	Disposition  string     `json:"disposition,omitempty"`
	Relationship string     `json:"relationship,omitempty"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
}

// Task is the durable resource created by contract commit.
type Task struct {
	TaskID       string         `json:"task_id"`
	ClientTaskID string         `json:"client_task_id,omitempty"`
	QuoteID      string         `json:"quote_id"`
	State        TaskState      `json:"state"`
	StatusReason string         `json:"status_reason,omitempty"`
	Progress     int            `json:"progress_percent,omitempty"`
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
	StartedAt    *time.Time     `json:"started_at,omitempty"`
	CompletedAt  *time.Time     `json:"completed_at,omitempty"`
	Artifacts    []ArtifactRef  `json:"artifacts,omitempty"`
	Billing      *BillingRecord `json:"billing,omitempty"`
	Receipt      *Receipt       `json:"receipt,omitempty"`
	Version      int64          `json:"version"`
}

// Receipt is the signed completion record for contract flow.
type Receipt struct {
	ProtocolVersion string           `json:"protocol_version"`
	ServiceID       string           `json:"service_id"`
	ReceiptID       string           `json:"receipt_id"`
	TaskID          string           `json:"task_id"`
	QuoteID         string           `json:"quote_id"`
	Outcome         TaskState        `json:"outcome"`
	Artifacts       []ArtifactRef    `json:"artifacts,omitempty"`
	Billing         *BillingRecord   `json:"billing,omitempty"`
	BilledAmount    *Money           `json:"billed_amount,omitempty"`
	BilledBreakdown []PriceComponent `json:"billed_breakdown,omitempty"`
	AuditRoot       string           `json:"audit_root"`
	CompletedAt     time.Time        `json:"completed_at"`
	Signature       Signature        `json:"signature"`
}

// CancelRequest supplies a reason and optional expected task version.
type CancelRequest struct {
	Reason          string `json:"reason,omitempty"`
	ExpectedVersion *int64 `json:"expected_version,omitempty"`
}

// CommitResponse returns the durable task created by contract acceptance.
type CommitResponse struct {
	Task              Task      `json:"task"`
	AcceptedAt        time.Time `json:"accepted_at"`
	IdempotencyExpiry time.Time `json:"idempotency_expires_at"`
}

// Manifest is the small cacheable discovery document. It lists protocol entry
// points and platform-level features, while the separate capability catalog
// lists concrete service intents.
type Manifest struct {
	Protocol          string           `json:"protocol"`
	Versions          []string         `json:"versions"`
	ServiceID         string           `json:"service_id"`
	ServiceName       string           `json:"service_name"`
	BaseURL           string           `json:"base_url"`
	JWKSURI           string           `json:"jwks_uri"`
	CapabilitiesURI   string           `json:"capabilities_uri"`
	OptionsURI        string           `json:"options_uri"`
	InvokeURI         string           `json:"invoke_uri"`
	FilesURI          string           `json:"files_uri,omitempty"`
	DocumentationURI  string           `json:"documentation_uri,omitempty"`
	PrivacyPolicyURI  string           `json:"privacy_policy_uri,omitempty"`
	TermsURI          string           `json:"terms_uri,omitempty"`
	AuthSchemes       []string         `json:"auth_schemes"`
	BillingModes      []BillingMode    `json:"billing_modes,omitempty"`
	PaymentSchemes    []string         `json:"payment_schemes,omitempty"`
	Features          map[string]bool  `json:"features"`
	Limits            map[string]int64 `json:"limits,omitempty"`
	GeneratedAt       time.Time        `json:"generated_at"`
	ManifestExpiresAt time.Time        `json:"manifest_expires_at"`
}

// AuditEvent is an append-only signed event. ResourceType and ResourceID allow
// the same verifiable chain format to cover tasks, direct invocations, uploads,
// and future protocol resources without pretending every chain is a task.
type AuditEvent struct {
	EventID      string          `json:"event_id"`
	ResourceType string          `json:"resource_type"`
	ResourceID   string          `json:"resource_id"`
	Sequence     int64           `json:"sequence"`
	Type         string          `json:"type"`
	OccurredAt   time.Time       `json:"occurred_at"`
	Actor        EntityRef       `json:"actor"`
	Data         json.RawMessage `json:"data,omitempty"`
	DataDigest   string          `json:"data_digest"`
	PreviousHash string          `json:"previous_hash,omitempty"`
	EventHash    string          `json:"event_hash"`
	Signature    Signature       `json:"signature"`
}
