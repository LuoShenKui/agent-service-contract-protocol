package ascp

import (
	"encoding/json"
	"testing"
)

func TestSigningProjectionRemovesOnlyTopLevelSignature(t *testing.T) {
	t.Parallel()

	value := struct {
		ID        string         `json:"id"`
		Nested    map[string]any `json:"nested"`
		Signature Signature      `json:"signature"`
	}{
		ID:     "contract-123",
		Nested: map[string]any{"signature": "domain-data"},
		Signature: Signature{
			Algorithm: "EdDSA",
			KeyID:     "key-1",
		},
	}

	projection, err := SigningProjection(value)
	if err != nil {
		t.Fatalf("SigningProjection: %v", err)
	}
	if _, exists := projection["signature"]; exists {
		t.Fatal("top-level signature envelope was not removed")
	}

	var nested map[string]any
	if err := json.Unmarshal(projection["nested"], &nested); err != nil {
		t.Fatalf("decode nested object: %v", err)
	}
	if nested["signature"] != "domain-data" {
		t.Fatalf("nested domain data was changed: %#v", nested)
	}
}
