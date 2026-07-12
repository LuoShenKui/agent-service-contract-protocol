// Package billing defines the ASCP settlement adapter boundary. It supports
// immediate payment as well as prepaid balances, subscriptions, postpaid
// accounts, invoices, sponsor arrangements, and clearing systems.
package billing

import (
	"context"

	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
)

// Reservation is the provider-neutral result of validating and reserving a
// billing arrangement before execution. Some modes reserve money, while others
// reserve credit, usage allowance, invoice capacity, or sponsor authorization.
type Reservation struct {
	Reference      string
	Mode           ascp.BillingMode
	ArrangementRef string
	Amount         ascp.Money
	State          string
	InvoiceRef     string
	PeriodRef      string
}

// Settlement records the completed financial or accounting effect. A pay-now
// rail may capture funds, while a subscription records usage and a monthly
// invoice appends a line item to an open billing period.
type Settlement struct {
	Reference      string
	Mode           ascp.BillingMode
	ArrangementRef string
	Amount         ascp.Money
	State          string
	InvoiceRef     string
	PeriodRef      string
}

// Refund records a reversal against a prior settlement. Some billing modes may
// implement this as account credit rather than movement on the original rail.
type Refund struct {
	Reference string
	Amount    ascp.Money
	State     string
}

// Processor isolates protocol orchestration from Stripe, Adyen, AP2, internal
// prepaid credit, subscriptions, enterprise chargeback, invoices, or a unified
// platform clearing service. Every operation must be idempotent.
type Processor interface {
	Reserve(
		ctx context.Context,
		serviceID string,
		contractID string,
		bindingDigest string,
		terms ascp.BillingTerms,
		authorization *ascp.BillingAuthorization,
		amount ascp.Money,
		idempotencyKey string,
	) (Reservation, error)
	Settle(ctx context.Context, reservationRef string, amount ascp.Money, idempotencyKey string) (Settlement, error)
	Release(ctx context.Context, reservationRef, idempotencyKey string) error
	Refund(ctx context.Context, settlementRef string, amount ascp.Money, idempotencyKey string) (Refund, error)
}

// DeclinedError is a definitive business decision. Retrying the same terms and
// authorization cannot succeed without changing the funding arrangement.
type DeclinedError struct {
	Reason string
}

func (e DeclinedError) Error() string { return "billing declined: " + e.Reason }

// TemporaryError is retryable only because the adapter guarantees the attempted
// operation created no reservation, settlement, release, or refund state.
type TemporaryError struct {
	Reason string
}

func (e TemporaryError) Error() string { return "temporary no-effect billing error: " + e.Reason }

// UnknownOutcomeError means the billing system may have accepted the operation
// even though the adapter did not receive a definitive result. The outer ASCP
// idempotency claim must remain locked until reconciliation resolves the state.
type UnknownOutcomeError struct {
	Reason            string
	ReconciliationRef string
}

func (e UnknownOutcomeError) Error() string { return "unknown billing outcome: " + e.Reason }
