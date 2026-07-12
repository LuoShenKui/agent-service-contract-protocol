// Package server provides the ASCP HTTP binding and lifecycle engine. Platform
// implementations plug in through Service and keep their internal APIs, schemas,
// business rules, and data semantics private.
package server

import (
	"context"
	"time"

	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
)

// CapabilityOffer is the domain service's answer to the first full-contract
// handshake. It returns only requirements relevant to the requested intent.
type CapabilityOffer struct {
	Supported       bool
	Conditional     bool
	ReasonCode      string
	Reason          string
	ResolvedIntent  string
	Parameters      []ascp.ParameterDefinition
	RequiredScopes  []string
	BillingOptions  []ascp.BillingOption
	InputFilePolicy ascp.FilePolicy
	EstimatedPrice  *ascp.PriceEstimate
	SchemaVersion   string
	ExpiresAt       time.Time
}

// DirectPlan is a side-effect-free decision produced before direct execution.
// The protocol engine uses it to enforce scopes, file policy, idempotency,
// billing, and direct-flow eligibility before the provider touches reality.
type DirectPlan struct {
	Eligible              bool
	ReasonCode            string
	Reason                string
	ResolvedIntent        string
	NormalizedTask        map[string]any
	RequiredScopes        []string
	AuthorizationRequired bool
	BillingTerms          ascp.BillingTerms
	Price                 ascp.Money
	PriceCeiling          ascp.Money
	PriceBreakdown        []ascp.PriceComponent
	Effects               []ascp.Effect
	Permissions           []ascp.PermissionUse
	DataUse               ascp.DataUse
	RiskClass             string
	IdempotencyRequired   bool
	SLA                   ascp.ServiceLevel
}

// DirectExecutionResult is the compact result of one direct invocation. Small
// facts can be returned inline; files and large records use ArtifactRef.
type DirectExecutionResult struct {
	Result              map[string]any
	Artifacts           []ascp.ArtifactRef
	FinalPrice          *ascp.Money
	FinalPriceBreakdown []ascp.PriceComponent
}

// PreparedContract contains business-specific terms to be signed into a Quote.
// Domain code cannot bypass signing, authorization, billing, or audit by writing
// an arbitrary HTTP response.
type PreparedContract struct {
	NormalizedTask map[string]any
	Price          ascp.Money
	PriceCeiling   ascp.Money
	PriceBreakdown []ascp.PriceComponent
	BillingTerms   ascp.BillingTerms
	Effects        []ascp.Effect
	Permissions    []ascp.PermissionUse
	DataUse        ascp.DataUse
	RiskClass      string
	Confirmation   ascp.ConfirmationRequirement
	SLA            ascp.ServiceLevel
	ExpiresAt      time.Time
	RevocableUntil *time.Time
}

// ExecutionResult is the domain result used to complete a contract task and
// generate a signed receipt.
type ExecutionResult struct {
	Artifacts           []ascp.ArtifactRef
	Metadata            map[string]any
	FinalPrice          *ascp.Money
	FinalPriceBreakdown []ascp.PriceComponent
}

// Service implements the platform-owned agent. The service itself understands
// its internal data and workflows; the protocol engine enforces cross-platform
// security and transaction invariants.
type Service interface {
	// Capabilities returns a compact paginated task catalog. It must not expose the
	// platform's full low-level API schemas.
	Capabilities(ctx context.Context, identity Identity, query ascp.CapabilityQuery) (ascp.CapabilityCatalog, *ascp.Problem)

	// Options performs an optional side-effect-free preflight for one task.
	Options(ctx context.Context, identity Identity, request ascp.OptionsRequest) (ascp.OptionsResponse, *ascp.Problem)

	// PlanDirect determines whether the concrete request may use direct flow.
	PlanDirect(ctx context.Context, identity Identity, request ascp.DirectInvocationRequest) (DirectPlan, *ascp.Problem)

	// ExecuteDirect executes an already validated direct plan. InvocationID is the
	// provider-side deduplication and audit key when idempotency is required.
	ExecuteDirect(ctx context.Context, identity Identity, invocationID string, request ascp.DirectInvocationRequest, plan DirectPlan) (DirectExecutionResult, *ascp.Problem)

	// The remaining methods implement full contract flow.
	Negotiate(ctx context.Context, identity Identity, request ascp.NegotiationRequest) (CapabilityOffer, *ascp.Problem)
	Prepare(ctx context.Context, identity Identity, offer StoredOffer, request ascp.PrepareRequest) (PreparedContract, *ascp.Problem)
	Execute(ctx context.Context, identity Identity, task ascp.Task, quote ascp.Quote) (ExecutionResult, *ascp.Problem)
	Cancel(ctx context.Context, identity Identity, task ascp.Task, request ascp.CancelRequest) *ascp.Problem
}

// StoredOffer is the immutable server-side record of a successful negotiation.
type StoredOffer struct {
	OfferID        string
	NegotiationID  string
	Actor          ascp.EntityRef
	Principal      ascp.EntityRef
	ResolvedIntent string
	SchemaVersion  string
	RequiredScopes []string
	Budget         *ascp.Money
	ExpiresAt      time.Time
}
