package ascp

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// NewID creates an opaque, URL-safe identifier with a readable resource prefix.
// Random identifiers avoid leaking database sequence numbers or tenant volume.
func NewID(prefix string) (string, error) {
	var raw [18]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate random identifier: %w", err)
	}
	return prefix + "_" + base64.RawURLEncoding.EncodeToString(raw[:]), nil
}

// MustNewID is intended for initialization paths where failure of the operating
// system's cryptographic random source is unrecoverable.
func MustNewID(prefix string) string {
	id, err := NewID(prefix)
	if err != nil {
		panic(err)
	}
	return id
}
