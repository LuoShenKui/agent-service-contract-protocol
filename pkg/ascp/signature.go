package ascp

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// Signer issues Ed25519 JWS signatures for quotes, audit events, and receipts.
// Production deployments should load a persistent key from an HSM or KMS and
// rotate keys through the JWKS document instead of generating one at startup.
type Signer struct {
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	keyID      string
	now        func() time.Time
}

// JWK is the subset of JSON Web Key fields required for an Ed25519 public key.
type JWK struct {
	KeyType string `json:"kty"`
	Curve   string `json:"crv"`
	Use     string `json:"use"`
	Alg     string `json:"alg"`
	KeyID   string `json:"kid"`
	X       string `json:"x"`
}

// JWKSet is served from the manifest's jwks_uri.
type JWKSet struct {
	Keys []JWK `json:"keys"`
}

// NewSigner generates a signer suitable for local development and tests.
func NewSigner(keyID string) (*Signer, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("generate Ed25519 key: %w", err)
	}
	return NewSignerFromPrivateKey(keyID, privateKey)
}

// NewSignerFromPrivateKey creates a signer from a persistent Ed25519 key.
func NewSignerFromPrivateKey(keyID string, privateKey ed25519.PrivateKey) (*Signer, error) {
	if keyID == "" {
		return nil, errors.New("key ID is required")
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid Ed25519 private key length: %d", len(privateKey))
	}
	publicKey, ok := privateKey.Public().(ed25519.PublicKey)
	if !ok {
		return nil, errors.New("private key does not expose an Ed25519 public key")
	}
	return &Signer{
		privateKey: append(ed25519.PrivateKey(nil), privateKey...),
		publicKey:  append(ed25519.PublicKey(nil), publicKey...),
		keyID:      keyID,
		now:        func() time.Time { return time.Now().UTC() },
	}, nil
}

// PublicKey returns a defensive copy for verification by local clients.
func (s *Signer) PublicKey() ed25519.PublicKey {
	return append(ed25519.PublicKey(nil), s.publicKey...)
}

// KeyID returns the identifier published in JWS headers and JWKS.
func (s *Signer) KeyID() string {
	return s.keyID
}

// JWKS returns the public verification key in RFC 8037-compatible OKP form.
func (s *Signer) JWKS() JWKSet {
	return JWKSet{Keys: []JWK{{
		KeyType: "OKP",
		Curve:   "Ed25519",
		Use:     "sig",
		Alg:     "EdDSA",
		KeyID:   s.keyID,
		X:       base64.RawURLEncoding.EncodeToString(s.publicKey),
	}}}
}

// SignJSON creates a compact JWS whose payload is the exact JSON serialization
// of value. Because the payload is embedded, verifiers do not need to recreate
// a canonical byte sequence from an independently parsed object.
func (s *Signer) SignJSON(value any) (Signature, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return Signature{}, fmt.Errorf("marshal signed payload: %w", err)
	}

	createdAt := s.now().UTC().Truncate(time.Second)
	protectedHeader := struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
		Type      string `json:"typ"`
		IssuedAt  int64  `json:"iat"`
	}{
		Algorithm: "EdDSA",
		KeyID:     s.keyID,
		Type:      "ascp+jws",
		IssuedAt:  createdAt.Unix(),
	}
	protected, err := json.Marshal(protectedHeader)
	if err != nil {
		return Signature{}, fmt.Errorf("marshal JWS header: %w", err)
	}

	encodedHeader := base64.RawURLEncoding.EncodeToString(protected)
	encodedPayload := base64.RawURLEncoding.EncodeToString(payload)
	signingInput := encodedHeader + "." + encodedPayload
	rawSignature := ed25519.Sign(s.privateKey, []byte(signingInput))
	compact := signingInput + "." + base64.RawURLEncoding.EncodeToString(rawSignature)

	digest := sha256.Sum256(payload)
	return Signature{
		Algorithm:     "EdDSA",
		KeyID:         s.keyID,
		CreatedAt:     createdAt,
		PayloadDigest: "sha256:" + base64.RawURLEncoding.EncodeToString(digest[:]),
		JWS:           compact,
	}, nil
}

// VerifyJSON validates a compact JWS and unmarshals its signed payload into out.
// Callers should use the decoded payload as authoritative and compare any
// convenience fields received outside the JWS before acting on them.
func VerifyJSON(signature Signature, publicKey ed25519.PublicKey, out any) error {
	if signature.Algorithm != "EdDSA" {
		return fmt.Errorf("unsupported signature algorithm %q", signature.Algorithm)
	}
	if len(publicKey) != ed25519.PublicKeySize {
		return errors.New("invalid Ed25519 public key")
	}

	parts := strings.Split(signature.JWS, ".")
	if len(parts) != 3 {
		return errors.New("invalid compact JWS")
	}

	headerBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return fmt.Errorf("decode JWS header: %w", err)
	}
	var header struct {
		Algorithm string `json:"alg"`
		KeyID     string `json:"kid"`
		Type      string `json:"typ"`
		IssuedAt  int64  `json:"iat"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil {
		return fmt.Errorf("parse JWS header: %w", err)
	}
	if header.Algorithm != "EdDSA" || header.KeyID != signature.KeyID || header.Type != "ascp+jws" ||
		header.IssuedAt != signature.CreatedAt.UTC().Unix() {
		return errors.New("JWS header does not match signature metadata")
	}

	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return fmt.Errorf("decode JWS payload: %w", err)
	}
	rawSignature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return fmt.Errorf("decode JWS signature: %w", err)
	}
	if !ed25519.Verify(publicKey, []byte(parts[0]+"."+parts[1]), rawSignature) {
		return errors.New("JWS signature verification failed")
	}

	digest := sha256.Sum256(payload)
	expectedDigest := "sha256:" + base64.RawURLEncoding.EncodeToString(digest[:])
	if expectedDigest != signature.PayloadDigest {
		return errors.New("signed payload digest mismatch")
	}

	if err := json.Unmarshal(payload, out); err != nil {
		return fmt.Errorf("unmarshal signed payload: %w", err)
	}
	return nil
}

// ValidateSHA256Digest verifies the canonical ASCP digest form produced by
// SHA256Digest: the literal "sha256:" followed by an unpadded base64url
// encoding of exactly 32 bytes. Canonical validation avoids accepting aliases
// that compare differently as strings while naming the same bytes.
func ValidateSHA256Digest(value string) error {
	const prefix = "sha256:"
	if !strings.HasPrefix(value, prefix) {
		return errors.New("digest must use the sha256 scheme")
	}
	encoded := strings.TrimPrefix(value, prefix)
	decoded, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return fmt.Errorf("decode sha256 digest: %w", err)
	}
	if len(decoded) != sha256.Size {
		return fmt.Errorf("sha256 digest has %d bytes; want %d", len(decoded), sha256.Size)
	}
	if base64.RawURLEncoding.EncodeToString(decoded) != encoded {
		return errors.New("sha256 digest is not canonical base64url")
	}
	return nil
}

// SHA256Digest returns the ASCP textual digest format for arbitrary bytes.
func SHA256Digest(data []byte) string {
	digest := sha256.Sum256(data)
	return "sha256:" + base64.RawURLEncoding.EncodeToString(digest[:])
}
