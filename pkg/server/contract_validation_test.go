package server

import (
	"testing"
	"time"

	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
)

func TestValidatePreparedContractRejectsInconsistentPriceBreakdown(t *testing.T) {
	t.Parallel()

	contract := validPreparedContractForTest()
	contract.PriceBreakdown = []ascp.PriceComponent{
		{Type: "service_fee", Amount: ascp.Money{Currency: "USD", Amount: "0.40"}},
		{Type: "tax", Amount: ascp.Money{Currency: "USD", Amount: "0.20"}},
	}
	if err := validatePreparedContract(contract); err == nil {
		t.Fatal("expected inconsistent price breakdown to be rejected")
	}
}

func TestResolveFinalChargeEnforcesSignedVariablePriceCeiling(t *testing.T) {
	t.Parallel()

	quote := ascp.Quote{
		Price:        ascp.Money{Currency: "USD", Amount: "1.00"},
		PriceCeiling: ascp.Money{Currency: "USD", Amount: "2.00"},
		BillingTerms: ascp.BillingTerms{Mode: ascp.BillingPostpaid, ArrangementRequired: true, ArrangementRef: "account_1", SettlementTiming: "periodic", VariablePriceAllowed: true},
	}

	actual := ascp.Money{Currency: "USD", Amount: "1.25"}
	resolved, breakdown, err := resolveFinalCharge(quote, ExecutionResult{
		FinalPrice: &actual,
		FinalPriceBreakdown: []ascp.PriceComponent{
			{Type: "base", Amount: ascp.Money{Currency: "USD", Amount: "1.00"}},
			{Type: "usage", Amount: ascp.Money{Currency: "USD", Amount: "0.25"}},
		},
	})
	if err != nil {
		t.Fatalf("resolveFinalCharge: %v", err)
	}
	if resolved != actual || len(breakdown) != 2 {
		t.Fatalf("unexpected resolved charge: %#v %#v", resolved, breakdown)
	}

	overCeiling := ascp.Money{Currency: "USD", Amount: "2.01"}
	if _, _, err := resolveFinalCharge(quote, ExecutionResult{FinalPrice: &overCeiling}); err == nil {
		t.Fatal("final price above signed ceiling was accepted")
	}

	if _, _, err := resolveFinalCharge(quote, ExecutionResult{}); err == nil {
		t.Fatal("variable-price execution without a final price was accepted")
	}
}

func TestResolveFinalChargeRejectsFixedPriceMutation(t *testing.T) {
	t.Parallel()

	quote := ascp.Quote{
		Price:        ascp.Money{Currency: "USD", Amount: "1.00"},
		PriceCeiling: ascp.Money{Currency: "USD", Amount: "1.00"},
		BillingTerms: ascp.BillingTerms{Mode: ascp.BillingPayNow, SettlementTiming: "after_success", AcceptedSchemes: []string{"urn:test:pay"}},
	}
	changed := ascp.Money{Currency: "USD", Amount: "0.99"}
	if _, _, err := resolveFinalCharge(quote, ExecutionResult{FinalPrice: &changed}); err == nil {
		t.Fatal("fixed-price execution changed the signed amount")
	}
}

func validPreparedContractForTest() PreparedContract {
	price := ascp.Money{Currency: "USD", Amount: "1.00"}
	return PreparedContract{
		NormalizedTask: map[string]any{"intent": "test.execute"},
		Price:          price,
		PriceCeiling:   price,
		PriceBreakdown: []ascp.PriceComponent{{Type: "service_fee", Amount: price}},
		BillingTerms: ascp.BillingTerms{
			Mode:                  ascp.BillingPayNow,
			AuthorizationRequired: true,
			SettlementTiming:      "after_success",
			AcceptedSchemes:       []string{"urn:ascp:billing:test"},
		},
		Effects:      []ascp.Effect{{Type: "test.execute", Summary: "Execute test effect"}},
		RiskClass:    "test",
		Confirmation: ascp.ConfirmationRequirement{Required: true, Mode: "explicit"},
		ExpiresAt:    time.Now().UTC().Add(time.Minute),
	}
}
