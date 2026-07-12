package server

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
)

// Identity is the authenticated delegation carried by a protocol request. Actor
// is the calling agent; Principal is the user or organization on whose behalf it
// acts. Keeping both identities prevents confused-deputy decisions.
type Identity struct {
	Actor     ascp.EntityRef
	Principal ascp.EntityRef
	Scopes    map[string]bool
}

// HasScope checks whether the authenticated delegation includes a permission.
func (i Identity) HasScope(scope string) bool {
	return i.Scopes[scope]
}

// Authenticator validates the HTTP request before any protocol state is read or
// mutated. Production implementations should use OAuth 2.0 access tokens bound
// with DPoP or mutual TLS.
type Authenticator interface {
	Authenticate(request *http.Request) (Identity, error)
}

// StaticAuthenticator is a small test authenticator. It must not be used as a
// substitute for OAuth/OIDC in an Internet-facing deployment.
type StaticAuthenticator struct {
	Token    string
	Identity Identity
}

// Authenticate accepts one fixed bearer token. Hash comparison avoids exposing
// obvious token-prefix timing differences even in this deliberately small demo.
func (a StaticAuthenticator) Authenticate(request *http.Request) (Identity, error) {
	parts := strings.Fields(request.Header.Get("Authorization"))
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return Identity{}, errors.New("missing bearer token")
	}
	providedDigest := sha256.Sum256([]byte(parts[1]))
	expectedDigest := sha256.Sum256([]byte(a.Token))
	if subtle.ConstantTimeCompare(providedDigest[:], expectedDigest[:]) != 1 {
		return Identity{}, errors.New("invalid bearer token")
	}
	return a.Identity, nil
}
