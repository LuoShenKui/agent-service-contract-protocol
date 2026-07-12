package ascp

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// ValidateQuoteSemantics checks deterministic cross-provider invariants after a
// quote signature has been verified. It does not decide whether a principal
// should approve the quoted effects and it does not treat expiry as a structural
// error, because archived quotes remain verifiable after they expire.
func ValidateQuoteSemantics(quote Quote) error {
	if quote.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("unsupported signed protocol version %q", quote.ProtocolVersion)
	}
	if strings.TrimSpace(quote.ServiceID) == "" || strings.TrimSpace(quote.QuoteID) == "" ||
		strings.TrimSpace(quote.OfferID) == "" || strings.TrimSpace(quote.Intent) == "" {
		return errors.New("quote identity fields are incomplete")
	}
	if strings.TrimSpace(quote.Actor.Type) == "" || strings.TrimSpace(quote.Actor.ID) == "" ||
		strings.TrimSpace(quote.Principal.Type) == "" || strings.TrimSpace(quote.Principal.ID) == "" {
		return errors.New("quote actor or principal is incomplete")
	}
	if len(quote.NormalizedTask) == 0 {
		return errors.New("quote normalized task is empty")
	}
	for index, ref := range quote.ContextRefs {
		if err := validateContextRef(ref); err != nil {
			return fmt.Errorf("quote context reference %d: %w", index, err)
		}
		if !ref.ExpiresAt.IsZero() && !quote.ExpiresAt.IsZero() && ref.ExpiresAt.Before(quote.ExpiresAt) {
			return fmt.Errorf("quote context reference %d expires before the quote", index)
		}
	}
	for index, file := range quote.InputFiles {
		if err := ValidateFileRef(file); err != nil {
			return fmt.Errorf("quote input file %d: %w", index, err)
		}
		if !file.ExpiresAt.IsZero() && !quote.ExpiresAt.IsZero() && file.ExpiresAt.Before(quote.ExpiresAt) {
			return fmt.Errorf("quote input file %d expires before the quote", index)
		}
	}
	if quote.Callback != nil {
		parsed, err := url.Parse(quote.Callback.URL)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return errors.New("quote callback must be an absolute HTTPS URL without user info")
		}
	}
	if quote.IssuedAt.IsZero() || quote.ExpiresAt.IsZero() || !quote.ExpiresAt.After(quote.IssuedAt) {
		return errors.New("quote validity interval is invalid")
	}
	if quote.RevocableUntil != nil && quote.RevocableUntil.After(quote.ExpiresAt) {
		return errors.New("quote revocation deadline exceeds expiry")
	}
	if strings.TrimSpace(quote.RiskClass) == "" {
		return errors.New("quote risk class is missing")
	}
	if quote.Confirmation.Required && strings.TrimSpace(quote.Confirmation.Mode) == "" {
		return errors.New("quote confirmation mode is missing")
	}
	if quote.DataUse.RetentionSeconds < 0 {
		return errors.New("quote data retention is negative")
	}
	if quote.SLA.ExpectedCompletionSeconds < 0 || quote.SLA.MaximumCompletionSeconds < 0 ||
		(quote.SLA.MaximumCompletionSeconds > 0 && quote.SLA.ExpectedCompletionSeconds > quote.SLA.MaximumCompletionSeconds) {
		return errors.New("quote service-level interval is invalid")
	}
	if len(quote.Effects) == 0 {
		return errors.New("quote contains no explicit effect or access description")
	}
	for index, effect := range quote.Effects {
		if strings.TrimSpace(effect.Type) == "" || strings.TrimSpace(effect.Summary) == "" {
			return fmt.Errorf("quote effect %d requires type and summary", index)
		}
	}

	if err := ValidateMoney(quote.Price); err != nil {
		return fmt.Errorf("quote price: %w", err)
	}
	if err := ValidateMoney(quote.PriceCeiling); err != nil {
		return fmt.Errorf("quote price ceiling: %w", err)
	}
	ceilingComparison, err := CompareMoney(quote.PriceCeiling, quote.Price)
	if err != nil {
		return fmt.Errorf("compare quote price and ceiling: %w", err)
	}
	if ceilingComparison < 0 {
		return errors.New("quote price exceeds price ceiling")
	}
	if !quote.BillingTerms.VariablePriceAllowed && ceilingComparison != 0 {
		return errors.New("fixed-price quote has a different price ceiling")
	}
	if err := ValidateBillingTerms(quote.BillingTerms, quote.Price); err != nil {
		return fmt.Errorf("quote billing terms: %w", err)
	}
	if err := validatePriceComponents(quote.PriceBreakdown, quote.Price, "quote"); err != nil {
		return err
	}
	if quote.Execution.MaxPrice != nil {
		comparison, err := CompareMoney(*quote.Execution.MaxPrice, quote.PriceCeiling)
		if err != nil {
			return fmt.Errorf("compare execution maximum price: %w", err)
		}
		if comparison < 0 {
			return errors.New("quote exceeds its embedded client maximum price")
		}
	}
	if quote.Execution.NotBefore != nil && quote.Execution.Deadline != nil && quote.Execution.NotBefore.After(*quote.Execution.Deadline) {
		return errors.New("quote execution window is invalid")
	}
	return nil
}

// ValidateBillingTerms verifies that a selected billing mode has the minimum
// fields needed to interpret settlement without guessing from prose.
func ValidateBillingTerms(terms BillingTerms, price Money) error {
	if !validBillingMode(terms.Mode) {
		return fmt.Errorf("unsupported billing mode %q", terms.Mode)
	}
	if strings.TrimSpace(terms.SettlementTiming) == "" {
		return errors.New("settlement timing is required")
	}
	if terms.ArrangementRequired && strings.TrimSpace(terms.ArrangementRef) == "" {
		return errors.New("billing arrangement reference is required")
	}
	if terms.Mode == BillingPayNow && len(terms.AcceptedSchemes) == 0 {
		return errors.New("pay-now terms list no accepted payment scheme")
	}
	if terms.Mode == BillingFree {
		zero := Money{Currency: price.Currency, Amount: "0"}
		comparison, err := CompareMoney(price, zero)
		if err != nil {
			return err
		}
		if comparison != 0 {
			return errors.New("free billing mode must have a zero price")
		}
	}
	return nil
}

// ValidateFileRef checks wire-level file invariants. Ownership, readiness, scan
// evidence, and actual digest matching remain provider-side checks.
func ValidateFileRef(file FileRef) error {
	if strings.TrimSpace(file.FileID) == "" || strings.TrimSpace(file.URI) == "" {
		return errors.New("file_id and uri are required")
	}
	if strings.TrimSpace(file.Name) == "" || len(file.Name) > 512 {
		return errors.New("file name is missing or too long")
	}
	if strings.ContainsAny(file.Name, "\r\n\x00") {
		return errors.New("file name contains control characters")
	}
	if strings.TrimSpace(file.MediaType) == "" {
		return errors.New("file media type is required")
	}
	if file.Size < 0 {
		return errors.New("file size is negative")
	}
	if err := ValidateSHA256Digest(file.Digest); err != nil {
		return fmt.Errorf("file digest: %w", err)
	}
	if strings.TrimSpace(file.State) == "" || strings.TrimSpace(file.ScanStatus) == "" {
		return errors.New("file state and scan status are required")
	}
	return nil
}

// ValidateReceiptAgainstQuote checks that a verified contract receipt is a
// coherent settlement record for an already verified quote.
func ValidateReceiptAgainstQuote(receipt Receipt, quote Quote) error {
	if receipt.ProtocolVersion != quote.ProtocolVersion || receipt.ProtocolVersion != ProtocolVersion {
		return errors.New("receipt protocol version does not match quote")
	}
	if receipt.ServiceID != quote.ServiceID || receipt.QuoteID != quote.QuoteID {
		return errors.New("receipt service or quote identity does not match")
	}
	if strings.TrimSpace(receipt.ReceiptID) == "" || strings.TrimSpace(receipt.TaskID) == "" {
		return errors.New("receipt identity fields are incomplete")
	}
	if receipt.CompletedAt.IsZero() || receipt.CompletedAt.Before(quote.IssuedAt) {
		return errors.New("receipt completion time is invalid")
	}
	if strings.TrimSpace(receipt.AuditRoot) == "" {
		return errors.New("receipt audit root is missing")
	}
	switch receipt.Outcome {
	case TaskSucceeded, TaskFailed, TaskCancelled, TaskCompensated, TaskDisputed:
	default:
		return fmt.Errorf("receipt outcome %q is not terminal", receipt.Outcome)
	}
	if receipt.BilledAmount != nil {
		if err := ValidateMoney(*receipt.BilledAmount); err != nil {
			return fmt.Errorf("receipt billed amount: %w", err)
		}
		comparison, err := CompareMoney(quote.PriceCeiling, *receipt.BilledAmount)
		if err != nil {
			return fmt.Errorf("compare receipt billing with quote ceiling: %w", err)
		}
		if comparison < 0 {
			return errors.New("receipt billing exceeds signed quote ceiling")
		}
		if !quote.BillingTerms.VariablePriceAllowed {
			comparison, err = CompareMoney(quote.Price, *receipt.BilledAmount)
			if err != nil || comparison != 0 {
				return errors.New("fixed-price receipt billing differs from signed price")
			}
		}
		if err := validatePriceComponents(receipt.BilledBreakdown, *receipt.BilledAmount, "receipt"); err != nil {
			return err
		}
	} else if len(receipt.BilledBreakdown) > 0 {
		return errors.New("receipt contains billed breakdown without billed amount")
	}
	if receipt.Billing != nil {
		if receipt.Billing.Mode != quote.BillingTerms.Mode {
			return errors.New("receipt billing mode differs from signed quote")
		}
		if receipt.Billing.Amount != nil && receipt.BilledAmount != nil {
			comparison, err := CompareMoney(*receipt.Billing.Amount, *receipt.BilledAmount)
			if err != nil || comparison != 0 {
				return errors.New("receipt billing record amount differs from billed amount")
			}
		}
	}
	return nil
}

// ValidateInvocationReceipt verifies structural semantics after the JWS has been
// checked. RequestDigest must be compared with the exact outbound request by the
// caller using VerifyInvocationReceiptForRequest.
func ValidateInvocationReceipt(receipt InvocationReceipt) error {
	if receipt.ProtocolVersion != ProtocolVersion {
		return fmt.Errorf("unsupported invocation receipt version %q", receipt.ProtocolVersion)
	}
	if strings.TrimSpace(receipt.ServiceID) == "" || strings.TrimSpace(receipt.ReceiptID) == "" ||
		strings.TrimSpace(receipt.InvocationID) == "" || strings.TrimSpace(receipt.Intent) == "" {
		return errors.New("invocation receipt identity fields are incomplete")
	}
	if strings.TrimSpace(receipt.RequestDigest) == "" || strings.TrimSpace(receipt.AuditRoot) == "" {
		return errors.New("invocation receipt digest or audit root is missing")
	}
	if receipt.CompletedAt.IsZero() {
		return errors.New("invocation receipt completion time is missing")
	}
	switch receipt.Outcome {
	case InvocationSucceeded, InvocationFailed, InvocationAccepted:
	default:
		return fmt.Errorf("unsupported invocation outcome %q", receipt.Outcome)
	}
	return nil
}

func validateContextRef(ref ContextRef) error {
	if strings.TrimSpace(ref.URI) == "" || len(ref.URI) > 4096 {
		return errors.New("invalid URI")
	}
	if ref.Size < 0 {
		return errors.New("negative size")
	}
	if strings.ContainsAny(ref.Digest, "\r\n\t ") {
		return errors.New("invalid digest")
	}
	return nil
}

func validBillingMode(mode BillingMode) bool {
	switch mode {
	case BillingFree, BillingPayNow, BillingPrepaidBalance, BillingSubscription,
		BillingPostpaid, BillingMonthlyInvoice, BillingClearing, BillingSponsored,
		BillingExternal:
		return true
	default:
		return false
	}
}

func validatePriceComponents(components []PriceComponent, expected Money, label string) error {
	if len(components) == 0 {
		return nil
	}
	total := Money{Currency: expected.Currency, Amount: "0"}
	for index, component := range components {
		if strings.TrimSpace(component.Type) == "" {
			return fmt.Errorf("%s price component %d has no type", label, index)
		}
		var err error
		total, err = AddMoney(total, component.Amount)
		if err != nil {
			return fmt.Errorf("%s price component %d: %w", label, index, err)
		}
	}
	comparison, err := CompareMoney(total, expected)
	if err != nil || comparison != 0 {
		return fmt.Errorf("%s price breakdown does not sum to amount", label)
	}
	return nil
}
