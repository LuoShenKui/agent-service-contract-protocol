package ascp

import (
	"testing"
	"time"
)

func TestValidateQuoteSemanticsAndReceiptBinding(t *testing.T) {
	t.Parallel()

	now := time.Now().UTC()
	price := Money{Currency: "USD", Amount: "1.00"}
	quote := Quote{
		ProtocolVersion: ProtocolVersion,
		ServiceID:       "urn:test:service",
		QuoteID:         "quo_test",
		OfferID:         "off_test",
		Intent:          "test.execute",
		Actor:           EntityRef{Type: "agent", ID: "agent"},
		Principal:       EntityRef{Type: "user", ID: "user"},
		NormalizedTask:  map[string]any{"intent": "test.execute"},
		Price:           price,
		PriceCeiling:    price,
		PriceBreakdown:  []PriceComponent{{Type: "service_fee", Amount: price}},
		BillingTerms: BillingTerms{
			Mode:                  BillingPayNow,
			AuthorizationRequired: true,
			SettlementTiming:      "after_success",
			AcceptedSchemes:       []string{"urn:test:pay"},
		},
		Effects:      []Effect{{Type: "test.execute", Summary: "Execute one test"}},
		RiskClass:    "test",
		Confirmation: ConfirmationRequirement{Required: true, Mode: "explicit"},
		IssuedAt:     now,
		ExpiresAt:    now.Add(time.Minute),
	}
	if err := ValidateQuoteSemantics(quote); err != nil {
		t.Fatalf("ValidateQuoteSemantics: %v", err)
	}

	recordAmount := price
	receipt := Receipt{
		ProtocolVersion: ProtocolVersion,
		ServiceID:       quote.ServiceID,
		ReceiptID:       "rcp_test",
		TaskID:          "tsk_test",
		QuoteID:         quote.QuoteID,
		Outcome:         TaskSucceeded,
		Billing: &BillingRecord{
			Mode:   BillingPayNow,
			Amount: &recordAmount,
			State:  "captured",
		},
		BilledAmount:    &price,
		BilledBreakdown: []PriceComponent{{Type: "service_fee", Amount: price}},
		AuditRoot:       "sha256:test",
		CompletedAt:     now.Add(time.Second),
	}
	if err := ValidateReceiptAgainstQuote(receipt, quote); err != nil {
		t.Fatalf("ValidateReceiptAgainstQuote: %v", err)
	}

	tampered := receipt
	tampered.ServiceID = "urn:wrong:service"
	if err := ValidateReceiptAgainstQuote(tampered, quote); err == nil {
		t.Fatal("cross-service receipt was accepted")
	}
}

func TestValidateBillingTermsSupportsStandingArrangements(t *testing.T) {
	t.Parallel()

	price := Money{Currency: "USD", Amount: "0.01"}
	for _, mode := range []BillingMode{
		BillingPrepaidBalance,
		BillingSubscription,
		BillingPostpaid,
		BillingMonthlyInvoice,
		BillingClearing,
		BillingSponsored,
		BillingExternal,
	} {
		terms := BillingTerms{
			Mode:                mode,
			ArrangementRequired: true,
			ArrangementRef:      "arrangement_test",
			SettlementTiming:    "periodic",
		}
		if mode == BillingSubscription || mode == BillingSponsored {
			price = Money{Currency: "USD", Amount: "0.00"}
		} else {
			price = Money{Currency: "USD", Amount: "0.01"}
		}
		if err := ValidateBillingTerms(terms, price); err != nil {
			t.Fatalf("mode %s: %v", mode, err)
		}
	}
}
