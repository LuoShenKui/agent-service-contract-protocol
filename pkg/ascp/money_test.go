package ascp

import "testing"

func TestCompareMoneyUsesExactDecimals(t *testing.T) {
	t.Parallel()

	comparison, err := CompareMoney(
		Money{Currency: "USD", Amount: "0.10"},
		Money{Currency: "USD", Amount: "0.1"},
	)
	if err != nil {
		t.Fatalf("CompareMoney returned an error: %v", err)
	}
	if comparison != 0 {
		t.Fatalf("expected exact decimal equality, got %d", comparison)
	}
}

func TestValidateMoneyRejectsAmbiguousFormats(t *testing.T) {
	t.Parallel()

	invalid := []Money{
		{Currency: "usd", Amount: "1.00"},
		{Currency: "USD", Amount: "-1"},
		{Currency: "USD", Amount: "1e3"},
		{Currency: "USD", Amount: " 1.00"},
		{Currency: "USD", Amount: "1."},
	}
	for _, value := range invalid {
		if err := ValidateMoney(value); err == nil {
			t.Errorf("expected %#v to be rejected", value)
		}
	}
}

func TestAddMoneyUsesExactDecimalArithmetic(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		left  Money
		right Money
		want  Money
	}{
		{
			name:  "decimal fractions do not use binary floating point",
			left:  Money{Currency: "USD", Amount: "0.10"},
			right: Money{Currency: "USD", Amount: "0.20"},
			want:  Money{Currency: "USD", Amount: "0.3"},
		},
		{
			name:  "large exact values",
			left:  Money{Currency: "USD", Amount: "999999999999999999.999999999999999999"},
			right: Money{Currency: "USD", Amount: "0.000000000000000001"},
			want:  Money{Currency: "USD", Amount: "1000000000000000000"},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := AddMoney(test.left, test.right)
			if err != nil {
				t.Fatalf("AddMoney: %v", err)
			}
			if got != test.want {
				t.Fatalf("AddMoney = %#v, want %#v", got, test.want)
			}
		})
	}

	if _, err := AddMoney(Money{Currency: "USD", Amount: "1"}, Money{Currency: "EUR", Amount: "1"}); err == nil {
		t.Fatal("AddMoney accepted mismatched currencies")
	}
}
