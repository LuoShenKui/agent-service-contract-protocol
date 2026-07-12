package ascp

import (
	"testing"
	"time"
)

func TestSignerRoundTripAndMetadataBinding(t *testing.T) {
	t.Parallel()

	signer, err := NewSigner("test-key")
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	payload := map[string]any{"quote_id": "quo_123", "amount": "0.01"}
	signature, err := signer.SignJSON(payload)
	if err != nil {
		t.Fatalf("SignJSON: %v", err)
	}

	var decoded map[string]any
	if err := VerifyJSON(signature, signer.PublicKey(), &decoded); err != nil {
		t.Fatalf("VerifyJSON: %v", err)
	}
	if decoded["quote_id"] != "quo_123" {
		t.Fatalf("unexpected signed payload: %#v", decoded)
	}

	tampered := signature
	tampered.CreatedAt = tampered.CreatedAt.Add(time.Second)
	if err := VerifyJSON(tampered, signer.PublicKey(), &decoded); err == nil {
		t.Fatal("expected tampered signature metadata to be rejected")
	}
}

func TestValidateSHA256DigestRejectsNonCanonicalValues(t *testing.T) {
	t.Parallel()

	canonical := SHA256Digest([]byte("canonical digest"))
	if err := ValidateSHA256Digest(canonical); err != nil {
		t.Fatalf("canonical digest was rejected: %v", err)
	}

	// ASCP deliberately accepts one textual representation only. Rejecting
	// padding and alternate alphabets prevents two strings from naming the same
	// bytes while producing different quote, idempotency, or audit bindings.
	cases := []string{
		"",
		"SHA256:" + canonical[len("sha256:"):],
		canonical + "=",
		"sha256:not-base64url!",
		"sha256:" + canonical[len("sha256:"):len(canonical)-1],
	}
	for _, value := range cases {
		if err := ValidateSHA256Digest(value); err == nil {
			t.Fatalf("non-canonical digest was accepted: %q", value)
		}
	}
}
