package ascp

import (
	"fmt"
	"math/big"
	"regexp"
	"strings"
)

var currencyCodePattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{2,11}$`)

// ValidateMoney validates ASCP's exact decimal representation. It deliberately
// rejects exponents, signs, whitespace, and more than 18 fractional digits so
// all implementations can compare amounts without binary floating-point loss.
func ValidateMoney(value Money) error {
	if !currencyCodePattern.MatchString(value.Currency) {
		return fmt.Errorf("invalid currency code %q", value.Currency)
	}
	if value.Amount == "" || strings.TrimSpace(value.Amount) != value.Amount {
		return fmt.Errorf("invalid monetary amount %q", value.Amount)
	}
	parts := strings.Split(value.Amount, ".")
	if len(parts) > 2 || parts[0] == "" {
		return fmt.Errorf("invalid monetary amount %q", value.Amount)
	}
	for _, r := range parts[0] {
		if r < '0' || r > '9' {
			return fmt.Errorf("invalid monetary amount %q", value.Amount)
		}
	}
	if len(parts) == 2 {
		if len(parts[1]) == 0 || len(parts[1]) > 18 {
			return fmt.Errorf("invalid monetary amount %q", value.Amount)
		}
		for _, r := range parts[1] {
			if r < '0' || r > '9' {
				return fmt.Errorf("invalid monetary amount %q", value.Amount)
			}
		}
	}
	if _, ok := new(big.Rat).SetString(value.Amount); !ok {
		return fmt.Errorf("invalid monetary amount %q", value.Amount)
	}
	return nil
}

// CompareMoney compares two exact amounts. Both values must use the same
// currency. The result follows strings.Compare semantics: -1, 0, or 1.
func CompareMoney(left, right Money) (int, error) {
	if err := ValidateMoney(left); err != nil {
		return 0, err
	}
	if err := ValidateMoney(right); err != nil {
		return 0, err
	}
	if left.Currency != right.Currency {
		return 0, fmt.Errorf("currency mismatch: %s != %s", left.Currency, right.Currency)
	}
	leftRat, _ := new(big.Rat).SetString(left.Amount)
	rightRat, _ := new(big.Rat).SetString(right.Amount)
	return leftRat.Cmp(rightRat), nil
}

// AddMoney adds two exact decimal monetary values without converting through
// binary floating point. The result preserves enough decimal places to represent
// the exact sum and removes only insignificant trailing fractional zeroes.
func AddMoney(left, right Money) (Money, error) {
	if err := ValidateMoney(left); err != nil {
		return Money{}, err
	}
	if err := ValidateMoney(right); err != nil {
		return Money{}, err
	}
	if left.Currency != right.Currency {
		return Money{}, fmt.Errorf("currency mismatch: %s != %s", left.Currency, right.Currency)
	}

	leftRat, _ := new(big.Rat).SetString(left.Amount)
	rightRat, _ := new(big.Rat).SetString(right.Amount)
	sum := new(big.Rat).Add(leftRat, rightRat)

	// Both inputs are finite base-10 decimals with at most 18 fractional digits.
	// Their exact sum is therefore representable at the larger input scale.
	scale := decimalScale(left.Amount)
	if rightScale := decimalScale(right.Amount); rightScale > scale {
		scale = rightScale
	}
	amount := sum.FloatString(scale)
	if strings.Contains(amount, ".") {
		amount = strings.TrimRight(amount, "0")
		amount = strings.TrimRight(amount, ".")
	}
	if amount == "" {
		amount = "0"
	}
	return Money{Currency: left.Currency, Amount: amount}, nil
}

// decimalScale returns the count of fractional decimal digits in an already
// validated ASCP monetary amount.
func decimalScale(amount string) int {
	if separator := strings.IndexByte(amount, '.'); separator >= 0 {
		return len(amount) - separator - 1
	}
	return 0
}
