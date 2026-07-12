package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
)

// AuthorizationVerifier proves that a human approval, policy decision, or
// delegated mandate referenced by a commit is authentic and still valid. The
// protocol engine performs structural quote binding first; this interface must
// validate the referenced evidence against the provider's authorization system.
type AuthorizationVerifier interface {
	Verify(ctx context.Context, identity Identity, quote ascp.Quote, evidence ascp.AuthorizationEvidence) *ascp.Problem
}

// DemoAuthorizationVerifier accepts only references beginning with
// "demo-approval-". It is intentionally explicit so deployments cannot mistake
// unverified JSON fields for real authority.
type DemoAuthorizationVerifier struct{}

// Verify implements AuthorizationVerifier for examples and tests.
func (DemoAuthorizationVerifier) Verify(_ context.Context, _ Identity, _ ascp.Quote, evidence ascp.AuthorizationEvidence) *ascp.Problem {
	if !strings.HasPrefix(evidence.Reference, "demo-approval-") {
		problem := ascp.Problem{
			Type:      "urn:ascp:problem:" + ascp.ErrAuthorizationInvalid,
			Title:     "Authorization evidence could not be verified",
			Status:    http.StatusForbidden,
			Detail:    "The demo verifier accepts only demo-approval-* references.",
			Code:      ascp.ErrAuthorizationInvalid,
			Category:  "authorization",
			Retryable: false,
		}
		return &problem
	}
	return nil
}
