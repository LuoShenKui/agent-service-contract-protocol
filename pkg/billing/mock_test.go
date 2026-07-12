package billing

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
)

const testServiceID = "urn:ascp:service:test"

func TestMockProcessorEnforcesAudienceBindingAndAmountCeiling(t *testing.T) {
	t.Parallel()

	processor := NewMockProcessor()
	maximum := ascp.Money{Currency: "USD", Amount: "10.00"}
	authorization := &ascp.BillingAuthorization{
		Mode:             ascp.BillingPayNow,
		AuthorizationRef: "mockpay_unit",
		Payer:            ascp.EntityRef{Type: "user", ID: "payer"},
		Audience:         testServiceID,
		MaximumAmount:    &maximum,
		ExpiresAt:        time.Now().UTC().Add(time.Minute),
		BindingDigest:    "sha256:quote",
		Usage:            "single_use",
	}
	terms := ascp.BillingTerms{
		Mode:                  ascp.BillingPayNow,
		AuthorizationRequired: true,
		SettlementTiming:      "after_success",
		AcceptedSchemes:       []string{"urn:ascp:billing:mock-pay-now"},
	}

	_, err := processor.Reserve(context.Background(), "urn:wrong:service", "quote-1", authorization.BindingDigest, terms, authorization,
		ascp.Money{Currency: "USD", Amount: "1.00"}, "reserve-audience")
	var declined DeclinedError
	if !errors.As(err, &declined) {
		t.Fatalf("expected audience-binding decline, got %v", err)
	}

	_, err = processor.Reserve(context.Background(), testServiceID, "quote-1", "sha256:different", terms, authorization,
		ascp.Money{Currency: "USD", Amount: "1.00"}, "reserve-digest")
	if !errors.As(err, &declined) {
		t.Fatalf("expected binding-digest decline, got %v", err)
	}

	reservation, err := processor.Reserve(context.Background(), testServiceID, "quote-1", authorization.BindingDigest, terms, authorization,
		ascp.Money{Currency: "USD", Amount: "5.00"}, "reserve-valid")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if _, err := processor.Settle(context.Background(), reservation.Reference,
		ascp.Money{Currency: "USD", Amount: "6.00"}, "settle-over"); !errors.As(err, &declined) {
		t.Fatalf("expected over-settlement decline, got %v", err)
	}
}

func TestMockProcessorSupportsStandingAccountArrangements(t *testing.T) {
	t.Parallel()

	processor := NewMockProcessor()
	amount := ascp.Money{Currency: "USD", Amount: "1.00"}
	terms := ascp.BillingTerms{
		Mode:                ascp.BillingMonthlyInvoice,
		ArrangementRequired: true,
		ArrangementRef:      "invoice-account-123",
		SettlementTiming:    "invoice",
		BillingPeriod:       "P1M",
	}
	reservation, err := processor.Reserve(context.Background(), testServiceID, "quote-invoice", "sha256:invoice", terms, nil, amount, "reserve-invoice")
	if err != nil {
		t.Fatal(err)
	}
	if reservation.InvoiceRef == "" || reservation.PeriodRef == "" {
		t.Fatal("postpaid reservation did not expose invoice and period references")
	}
	settlement, err := processor.Settle(context.Background(), reservation.Reference, amount, "settle-invoice")
	if err != nil {
		t.Fatal(err)
	}
	if settlement.State != "invoice_item_recorded" {
		t.Fatalf("unexpected invoice settlement state %q", settlement.State)
	}
}

func TestMockProcessorOperationsAreIdempotent(t *testing.T) {
	t.Parallel()

	processor := NewMockProcessor()
	amount := ascp.Money{Currency: "USD", Amount: "1.00"}
	maximum := amount
	authorization := &ascp.BillingAuthorization{
		Mode:             ascp.BillingPayNow,
		AuthorizationRef: "mockpay_idempotent",
		Payer:            ascp.EntityRef{Type: "user", ID: "payer"},
		Audience:         testServiceID,
		MaximumAmount:    &maximum,
		ExpiresAt:        time.Now().UTC().Add(time.Minute),
		BindingDigest:    "sha256:quote",
		Usage:            "single_use",
	}
	terms := ascp.BillingTerms{
		Mode:                  ascp.BillingPayNow,
		AuthorizationRequired: true,
		SettlementTiming:      "after_success",
		AcceptedSchemes:       []string{"urn:ascp:billing:mock-pay-now"},
	}
	first, err := processor.Reserve(context.Background(), testServiceID, "quote-1", authorization.BindingDigest, terms, authorization, amount, "reserve-key")
	if err != nil {
		t.Fatal(err)
	}
	second, err := processor.Reserve(context.Background(), testServiceID, "quote-1", authorization.BindingDigest, terms, authorization, amount, "reserve-key")
	if err != nil {
		t.Fatal(err)
	}
	if first.Reference != second.Reference {
		t.Fatalf("reserve replay produced a new reservation: %s != %s", first.Reference, second.Reference)
	}

	if _, err := processor.Reserve(context.Background(), testServiceID, "quote-1", authorization.BindingDigest, terms, authorization, amount, "changed-key"); !errors.As(err, new(DeclinedError)) {
		t.Fatalf("expected single-use authorization reuse to be declined, got %v", err)
	}

	settlementOne, err := processor.Settle(context.Background(), first.Reference, amount, "settle-key")
	if err != nil {
		t.Fatal(err)
	}
	settlementTwo, err := processor.Settle(context.Background(), first.Reference, amount, "another-settle-key")
	if err != nil {
		t.Fatal(err)
	}
	if settlementOne.Reference != settlementTwo.Reference {
		t.Fatalf("second settlement produced a new record: %s != %s", settlementOne.Reference, settlementTwo.Reference)
	}
	if err := processor.Release(context.Background(), first.Reference, "release-after-settle"); err == nil {
		t.Fatal("settled reservation was incorrectly released")
	}
}

func TestMockProcessorRejectsCumulativeOverRefund(t *testing.T) {
	t.Parallel()

	processor := NewMockProcessor()
	amount := ascp.Money{Currency: "USD", Amount: "10.00"}
	maximum := amount
	authorization := &ascp.BillingAuthorization{
		Mode:             ascp.BillingPayNow,
		AuthorizationRef: "mockpay_refund",
		Payer:            ascp.EntityRef{Type: "user", ID: "payer"},
		Audience:         testServiceID,
		MaximumAmount:    &maximum,
		ExpiresAt:        time.Now().UTC().Add(time.Minute),
		BindingDigest:    "sha256:refund-quote",
	}
	terms := ascp.BillingTerms{
		Mode:                  ascp.BillingPayNow,
		AuthorizationRequired: true,
		SettlementTiming:      "after_success",
		AcceptedSchemes:       []string{"urn:ascp:billing:mock-pay-now"},
	}
	reservation, err := processor.Reserve(context.Background(), testServiceID, "quote-refund", authorization.BindingDigest, terms, authorization, amount, "reserve-refund")
	if err != nil {
		t.Fatal(err)
	}
	settlement, err := processor.Settle(context.Background(), reservation.Reference, amount, "settle-refund")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := processor.Refund(context.Background(), settlement.Reference,
		ascp.Money{Currency: "USD", Amount: "6.00"}, "refund-one"); err != nil {
		t.Fatal(err)
	}
	if _, err := processor.Refund(context.Background(), settlement.Reference,
		ascp.Money{Currency: "USD", Amount: "5.00"}, "refund-two"); err == nil {
		t.Fatal("cumulative over-refund was accepted")
	}
}
