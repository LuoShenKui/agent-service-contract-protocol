package billing

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
)

// MockProcessor is deterministic and safe for tests. It models multiple billing
// arrangements without contacting a real payment or accounting system.
type MockProcessor struct {
	mu sync.Mutex

	reservationsByKey map[string]Reservation
	reservationsByRef map[string]Reservation
	mandateUse        map[string]mandateUse
	settlementsByKey  map[string]Settlement
	settlementsByRef  map[string]Settlement
	settlementByHold  map[string]Settlement
	refundsByKey      map[string]Refund
	refundedTotals    map[string]ascp.Money
	released          map[string]bool
	now               func() time.Time
}

type mandateUse struct {
	BindingDigest  string
	IdempotencyKey string
	Reservation    Reservation
}

// NewMockProcessor creates an empty in-memory billing processor.
func NewMockProcessor() *MockProcessor {
	return &MockProcessor{
		reservationsByKey: make(map[string]Reservation),
		reservationsByRef: make(map[string]Reservation),
		mandateUse:        make(map[string]mandateUse),
		settlementsByKey:  make(map[string]Settlement),
		settlementsByRef:  make(map[string]Settlement),
		settlementByHold:  make(map[string]Settlement),
		refundsByKey:      make(map[string]Refund),
		refundedTotals:    make(map[string]ascp.Money),
		released:          make(map[string]bool),
		now:               func() time.Time { return time.Now().UTC() },
	}
}

// Reserve validates the selected mode, standing arrangement, quote/request
// binding, audience, expiry, and monetary ceiling before creating one durable
// provider-neutral reservation.
func (m *MockProcessor) Reserve(
	_ context.Context,
	serviceID string,
	contractID string,
	bindingDigest string,
	terms ascp.BillingTerms,
	authorization *ascp.BillingAuthorization,
	amount ascp.Money,
	idempotencyKey string,
) (Reservation, error) {
	if terms.Mode == ascp.BillingFree {
		return Reservation{}, DeclinedError{Reason: "free operations do not create billing reservations"}
	}
	if terms.ArrangementRequired && strings.TrimSpace(terms.ArrangementRef) == "" {
		return Reservation{}, DeclinedError{Reason: "required billing arrangement is missing"}
	}
	if terms.AuthorizationRequired && authorization == nil {
		return Reservation{}, DeclinedError{Reason: "per-call billing authorization is required"}
	}
	if authorization != nil {
		if authorization.Mode != terms.Mode {
			return Reservation{}, DeclinedError{Reason: "billing authorization mode mismatch"}
		}
		if authorization.Audience != serviceID {
			return Reservation{}, DeclinedError{Reason: "billing authorization audience mismatch"}
		}
		if authorization.BindingDigest != bindingDigest {
			return Reservation{}, DeclinedError{Reason: "billing authorization is not bound to this contract or request"}
		}
		if !authorization.ExpiresAt.IsZero() && !authorization.ExpiresAt.After(m.now()) {
			return Reservation{}, DeclinedError{Reason: "billing authorization expired"}
		}
		if terms.ArrangementRef != "" && authorization.ArrangementRef != "" && authorization.ArrangementRef != terms.ArrangementRef {
			return Reservation{}, DeclinedError{Reason: "billing arrangement reference mismatch"}
		}
		if authorization.MaximumAmount != nil {
			comparison, err := ascp.CompareMoney(*authorization.MaximumAmount, amount)
			if err != nil {
				return Reservation{}, DeclinedError{Reason: err.Error()}
			}
			if comparison < 0 {
				return Reservation{}, DeclinedError{Reason: "authorized amount is below the contract ceiling"}
			}
		}
		if terms.Mode == ascp.BillingPayNow && !strings.HasPrefix(authorization.AuthorizationRef, "mockpay_") {
			return Reservation{}, DeclinedError{Reason: "unknown mock pay-now authorization reference"}
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	key := contractID + ":" + idempotencyKey
	if reservation, ok := m.reservationsByKey[key]; ok {
		return reservation, nil
	}

	if authorization != nil && authorization.AuthorizationRef != "" {
		usage := authorization.Usage
		if usage == "" {
			usage = "single_use"
		}
		if usage != "single_use" && usage != "reusable" {
			return Reservation{}, DeclinedError{Reason: "unsupported billing authorization usage"}
		}
		if previous, ok := m.mandateUse[authorization.AuthorizationRef]; ok && usage == "single_use" {
			if previous.BindingDigest == bindingDigest && previous.IdempotencyKey == idempotencyKey {
				return previous.Reservation, nil
			}
			return Reservation{}, DeclinedError{Reason: "single-use billing authorization was already consumed"}
		}
	}

	reservation := Reservation{
		Reference:      ascp.MustNewID("blr"),
		Mode:           terms.Mode,
		ArrangementRef: terms.ArrangementRef,
		Amount:         amount,
		State:          reservationState(terms.Mode),
	}
	if terms.Mode == ascp.BillingMonthlyInvoice || terms.Mode == ascp.BillingPostpaid {
		now := m.now()
		reservation.InvoiceRef = "invoice-open-" + now.Format("200601")
		reservation.PeriodRef = now.Format("2006-01")
	}
	m.reservationsByKey[key] = reservation
	m.reservationsByRef[reservation.Reference] = reservation

	if authorization != nil && authorization.AuthorizationRef != "" {
		usage := authorization.Usage
		if usage == "" {
			usage = "single_use"
		}
		if usage == "single_use" {
			m.mandateUse[authorization.AuthorizationRef] = mandateUse{
				BindingDigest:  bindingDigest,
				IdempotencyKey: idempotencyKey,
				Reservation:    reservation,
			}
		}
	}
	return reservation, nil
}

// Settle completes a reservation at most once. The returned state reflects the
// billing mode rather than pretending every arrangement performs a card capture.
func (m *MockProcessor) Settle(_ context.Context, reservationRef string, amount ascp.Money, idempotencyKey string) (Settlement, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := reservationRef + ":" + idempotencyKey
	if settlement, ok := m.settlementsByKey[key]; ok {
		return settlement, nil
	}
	if existing, ok := m.settlementByHold[reservationRef]; ok {
		comparison, err := ascp.CompareMoney(existing.Amount, amount)
		if err != nil || comparison != 0 {
			return Settlement{}, DeclinedError{Reason: "reservation was already settled for a different amount"}
		}
		m.settlementsByKey[key] = existing
		return existing, nil
	}

	reservation, ok := m.reservationsByRef[reservationRef]
	if !ok {
		return Settlement{}, DeclinedError{Reason: "unknown billing reservation"}
	}
	if m.released[reservationRef] {
		return Settlement{}, DeclinedError{Reason: "billing reservation has been released"}
	}
	comparison, err := ascp.CompareMoney(reservation.Amount, amount)
	if err != nil {
		return Settlement{}, DeclinedError{Reason: err.Error()}
	}
	if comparison < 0 {
		return Settlement{}, DeclinedError{Reason: "settlement exceeds reserved amount"}
	}

	settlement := Settlement{
		Reference:      ascp.MustNewID("bls"),
		Mode:           reservation.Mode,
		ArrangementRef: reservation.ArrangementRef,
		Amount:         amount,
		State:          settlementState(reservation.Mode),
		InvoiceRef:     reservation.InvoiceRef,
		PeriodRef:      reservation.PeriodRef,
	}
	m.settlementsByKey[key] = settlement
	m.settlementsByRef[settlement.Reference] = settlement
	m.settlementByHold[reservationRef] = settlement
	return settlement, nil
}

// Release invalidates an unsettled reservation. Repeating the same release is
// safe; a settled reservation requires refund or account credit instead.
func (m *MockProcessor) Release(_ context.Context, reservationRef, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.reservationsByRef[reservationRef]; !ok {
		return fmt.Errorf("unknown billing reservation")
	}
	if _, settled := m.settlementByHold[reservationRef]; settled {
		return fmt.Errorf("settled reservation cannot be released")
	}
	m.released[reservationRef] = true
	return nil
}

// Refund creates an idempotent reversal and rejects cumulative over-refunds.
func (m *MockProcessor) Refund(_ context.Context, settlementRef string, amount ascp.Money, idempotencyKey string) (Refund, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := settlementRef + ":" + idempotencyKey
	if refund, ok := m.refundsByKey[key]; ok {
		return refund, nil
	}
	settlement, ok := m.settlementsByRef[settlementRef]
	if !ok {
		return Refund{}, fmt.Errorf("unknown billing settlement")
	}
	if err := ascp.ValidateMoney(amount); err != nil {
		return Refund{}, err
	}

	total := amount
	if previous, ok := m.refundedTotals[settlementRef]; ok {
		var err error
		total, err = ascp.AddMoney(previous, amount)
		if err != nil {
			return Refund{}, err
		}
	}
	comparison, err := ascp.CompareMoney(settlement.Amount, total)
	if err != nil {
		return Refund{}, err
	}
	if comparison < 0 {
		return Refund{}, fmt.Errorf("cumulative refunds exceed settled amount")
	}

	refund := Refund{
		Reference: ascp.MustNewID("blrfd"),
		Amount:    amount,
		State:     "refunded_or_credited",
	}
	m.refundsByKey[key] = refund
	m.refundedTotals[settlementRef] = total
	return refund, nil
}

func reservationState(mode ascp.BillingMode) string {
	switch mode {
	case ascp.BillingPayNow, ascp.BillingPrepaidBalance, ascp.BillingClearing:
		return "reserved"
	case ascp.BillingSubscription:
		return "allowance_reserved"
	case ascp.BillingPostpaid, ascp.BillingMonthlyInvoice:
		return "credit_reserved"
	case ascp.BillingSponsored:
		return "sponsor_approved"
	case ascp.BillingExternal:
		return "external_acknowledged"
	default:
		return "reserved"
	}
}

func settlementState(mode ascp.BillingMode) string {
	switch mode {
	case ascp.BillingPayNow:
		return "captured"
	case ascp.BillingPrepaidBalance:
		return "balance_debited"
	case ascp.BillingSubscription:
		return "usage_recorded"
	case ascp.BillingPostpaid, ascp.BillingMonthlyInvoice:
		return "invoice_item_recorded"
	case ascp.BillingClearing:
		return "clearing_recorded"
	case ascp.BillingSponsored:
		return "sponsor_usage_recorded"
	case ascp.BillingExternal:
		return "external_settlement_recorded"
	default:
		return "settled"
	}
}
