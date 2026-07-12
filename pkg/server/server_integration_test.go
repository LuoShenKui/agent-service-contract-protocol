package server_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/LuoShenKui/agent-service-contract-protocol/internal/email"
	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/audit"
	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/billing"
	ascpclient "github.com/LuoShenKui/agent-service-contract-protocol/pkg/client"
	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/server"
)

const testToken = "test-token-for-ascp"

func TestDirectAndContractFlowsWithFilesAndBilling(t *testing.T) {
	t.Parallel()

	client, signer, identity, closeServer := newHarness(t)
	defer closeServer()
	ctx := context.Background()

	catalog, err := client.Capabilities(ctx, ascp.CapabilityQuery{Limit: 10})
	if err != nil {
		t.Fatalf("Capabilities: %v", err)
	}
	if len(catalog.Capabilities) != 2 {
		t.Fatalf("expected two capabilities, got %d", len(catalog.Capabilities))
	}

	options, err := client.Options(ctx, ascp.OptionsRequest{Intent: "email.latest.read"})
	if err != nil {
		t.Fatalf("Options: %v", err)
	}
	if options.RecommendedFlow != ascp.FlowDirect || !options.DirectEligible {
		t.Fatalf("unexpected direct options: %#v", options)
	}

	readRequest := ascp.DirectInvocationRequest{
		Intent:     "email.latest.read",
		Parameters: map[string]any{"include_body": true},
	}
	readResponse, err := client.Invoke(ctx, readRequest, "")
	if err != nil {
		t.Fatalf("Invoke direct read: %v", err)
	}
	if readResponse.State != ascp.InvocationSucceeded || readResponse.Billing == nil || readResponse.Billing.Mode != ascp.BillingFree {
		t.Fatalf("unexpected direct response: %#v", readResponse)
	}
	if err := ascpclient.VerifyInvocationReceiptForRequest(readResponse.Receipt, readRequest, signer.PublicKey()); err != nil {
		t.Fatalf("VerifyInvocationReceiptForRequest: %v", err)
	}
	events, root, err := client.InvocationAudit(ctx, readResponse.InvocationID)
	if err != nil {
		t.Fatalf("InvocationAudit: %v", err)
	}
	if root == "" {
		t.Fatal("invocation audit root is empty")
	}
	if err := audit.Verify(events, signer.PublicKey()); err != nil {
		t.Fatalf("Verify invocation audit: root=%q err=%v", root, err)
	}
	if err := audit.VerifyInvocationReceiptAnchor(events, readResponse.Receipt); err != nil {
		t.Fatalf("VerifyInvocationReceiptAnchor: %v", err)
	}

	_, err = client.Invoke(ctx, ascp.DirectInvocationRequest{
		Intent: "email.send",
		Parameters: map[string]any{
			"recipient": "recipient@example.test",
			"subject":   "must not send directly",
			"body":      "contract required",
		},
	}, "")
	var directProblem *ascp.Problem
	if !errors.As(err, &directProblem) || directProblem.Code != ascp.ErrContractRequired {
		t.Fatalf("expected contract_required, got %v", err)
	}

	content := []byte("attachment content\n")
	digest := ascp.SHA256Digest(content)
	ticket, err := client.PrepareUpload(ctx, ascp.FileUploadRequest{
		Name:      "notes.txt",
		MediaType: "text/plain",
		Size:      int64(len(content)),
		Digest:    digest,
		Purpose:   "email attachment",
	}, "idem-file-prepare-00000001")
	if err != nil {
		t.Fatalf("PrepareUpload: %v", err)
	}
	fileRef, err := client.UploadFile(ctx, ticket, "text/plain", digest, content)
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	// A network retry of the exact same upload must be safe. The file store keeps
	// only a hash of the short-lived token, so it can authenticate the retry
	// without retaining the bearer secret itself.
	replayedFileRef, err := client.UploadFile(ctx, ticket, "text/plain", digest, content)
	if err != nil {
		t.Fatalf("UploadFile exact replay: %v", err)
	}
	if replayedFileRef.FileID != fileRef.FileID || replayedFileRef.Digest != fileRef.Digest {
		t.Fatalf("upload replay changed the file identity: %#v != %#v", replayedFileRef, fileRef)
	}

	_, downloaded, err := client.DownloadFile(ctx, fileRef.FileID, 1<<20)
	if err != nil {
		t.Fatalf("DownloadFile: %v", err)
	}
	if string(downloaded) != string(content) {
		t.Fatalf("downloaded content mismatch: %q", downloaded)
	}

	quote := prepareSendQuote(t, ctx, client, identity, []ascp.FileRef{fileRef}, ascp.BillingPreference{Mode: ascp.BillingPayNow})
	if err := ascpclient.VerifyQuote(quote, signer.PublicKey()); err != nil {
		t.Fatalf("VerifyQuote: %v", err)
	}
	maximum := quote.PriceCeiling
	now := time.Now().UTC()
	commitRequest := ascp.CommitRequest{
		QuoteID:     quote.QuoteID,
		QuoteDigest: quote.Signature.PayloadDigest,
		Authorization: ascp.AuthorizationEvidence{
			Type:          "explicit_user_confirmation",
			Reference:     "demo-approval-send-1",
			PrincipalID:   identity.Principal.ID,
			Audience:      quote.ServiceID,
			ApprovedAt:    now,
			ExpiresAt:     now.Add(5 * time.Minute),
			BindingDigest: quote.Signature.PayloadDigest,
		},
		BillingAuthorization: &ascp.BillingAuthorization{
			Mode:             ascp.BillingPayNow,
			AuthorizationRef: "mockpay_send_1",
			Payer:            identity.Principal,
			Audience:         quote.ServiceID,
			MaximumAmount:    &maximum,
			ExpiresAt:        now.Add(5 * time.Minute),
			BindingDigest:    quote.Signature.PayloadDigest,
			Usage:            "single_use",
		},
	}
	commitKey := "idem-send-commit-00000001"
	committed, err := client.Commit(ctx, commitRequest, commitKey)
	if err != nil {
		t.Fatalf("Commit pay-now: %v", err)
	}
	if committed.Task.State != ascp.TaskSucceeded || committed.Task.Receipt == nil {
		t.Fatalf("unexpected committed task: %#v", committed.Task)
	}
	if committed.Task.Billing == nil || committed.Task.Billing.State != "captured" {
		t.Fatalf("unexpected pay-now billing record: %#v", committed.Task.Billing)
	}
	if len(committed.Task.Artifacts) < 2 {
		t.Fatalf("expected sent message and attachment artifacts, got %#v", committed.Task.Artifacts)
	}
	if err := ascpclient.VerifyReceiptForQuote(*committed.Task.Receipt, quote, signer.PublicKey()); err != nil {
		t.Fatalf("VerifyReceiptForQuote: %v", err)
	}

	replayed, err := client.Commit(ctx, commitRequest, commitKey)
	if err != nil {
		t.Fatalf("Commit replay: %v", err)
	}
	if replayed.Task.TaskID != committed.Task.TaskID {
		t.Fatalf("idempotent replay created another task: %s != %s", replayed.Task.TaskID, committed.Task.TaskID)
	}

	subscriptionQuote := prepareSendQuote(t, ctx, client, identity, nil, ascp.BillingPreference{
		Mode:           ascp.BillingSubscription,
		ArrangementRef: "subscription_demo_team",
	})
	subscriptionCommit := ascp.CommitRequest{
		QuoteID:     subscriptionQuote.QuoteID,
		QuoteDigest: subscriptionQuote.Signature.PayloadDigest,
		Authorization: ascp.AuthorizationEvidence{
			Type:          "enterprise_policy",
			Reference:     "demo-approval-subscription-1",
			PrincipalID:   identity.Principal.ID,
			Audience:      subscriptionQuote.ServiceID,
			ApprovedAt:    now,
			ExpiresAt:     now.Add(5 * time.Minute),
			BindingDigest: subscriptionQuote.Signature.PayloadDigest,
		},
	}
	subscriptionResult, err := client.Commit(ctx, subscriptionCommit, "idem-subscription-commit-0001")
	if err != nil {
		t.Fatalf("Commit subscription: %v", err)
	}
	if subscriptionResult.Task.Billing == nil || subscriptionResult.Task.Billing.Mode != ascp.BillingSubscription || subscriptionResult.Task.Billing.State != "usage_recorded" {
		t.Fatalf("unexpected subscription billing: %#v", subscriptionResult.Task.Billing)
	}
}

func TestUploadRejectsDigestMismatch(t *testing.T) {
	t.Parallel()

	client, _, _, closeServer := newHarness(t)
	defer closeServer()
	ctx := context.Background()
	content := []byte("expected")
	ticket, err := client.PrepareUpload(ctx, ascp.FileUploadRequest{
		Name:      "digest.txt",
		MediaType: "text/plain",
		Size:      int64(len(content)),
		Digest:    ascp.SHA256Digest(content),
	}, "idem-file-mismatch-prepare-1")
	if err != nil {
		t.Fatalf("PrepareUpload: %v", err)
	}
	_, err = client.UploadFile(ctx, ticket, "text/plain", ascp.SHA256Digest(content), []byte("changed!"))
	var p *ascp.Problem
	if !errors.As(err, &p) || (p.Code != ascp.ErrDigestMismatch && p.Code != ascp.ErrValidationFailed) {
		t.Fatalf("expected digest/size rejection, got %v", err)
	}
}

func TestPrepareUploadRejectsMalformedDigestAndPastExpiry(t *testing.T) {
	t.Parallel()

	client, _, _, closeServer := newHarness(t)
	defer closeServer()
	ctx := context.Background()

	// A malformed digest must fail before an upload credential is issued. This
	// keeps aliases and unverifiable content out of later signed contracts.
	_, err := client.PrepareUpload(ctx, ascp.FileUploadRequest{
		Name:      "bad-digest.txt",
		MediaType: "text/plain",
		Size:      1,
		Digest:    "sha256:not-a-canonical-digest",
	}, "idem-file-invalid-digest-0001")
	var problem *ascp.Problem
	if !errors.As(err, &problem) || problem.Code != ascp.ErrValidationFailed {
		t.Fatalf("expected malformed digest rejection, got %v", err)
	}

	// A caller cannot create an already-expired reference. Accepting this would
	// produce an unusable upload ticket and ambiguous retry behavior.
	_, err = client.PrepareUpload(ctx, ascp.FileUploadRequest{
		Name:      "expired.txt",
		MediaType: "text/plain",
		Size:      1,
		Digest:    ascp.SHA256Digest([]byte("x")),
		ExpiresAt: time.Now().UTC().Add(-time.Minute),
	}, "idem-file-expired-prepare-0001")
	problem = nil
	if !errors.As(err, &problem) || problem.Code != ascp.ErrValidationFailed {
		t.Fatalf("expected past expiry rejection, got %v", err)
	}
}

func prepareSendQuote(
	t *testing.T,
	ctx context.Context,
	client *ascpclient.Client,
	identity server.Identity,
	files []ascp.FileRef,
	billingPreference ascp.BillingPreference,
) ascp.Quote {
	t.Helper()
	negotiated, err := client.Negotiate(ctx, ascp.NegotiationRequest{
		Intent:     "email.send",
		Goal:       "Send one email",
		Actor:      identity.Actor,
		Principal:  identity.Principal,
		InputFiles: files,
	}, "idem-negotiate-"+ascp.MustNewID("test"))
	if err != nil {
		t.Fatalf("Negotiate: %v", err)
	}
	if !negotiated.Supported || negotiated.OfferID == "" {
		t.Fatalf("unexpected negotiation response: %#v", negotiated)
	}
	quote, err := client.Prepare(ctx, ascp.PrepareRequest{
		OfferID:       negotiated.OfferID,
		SchemaVersion: negotiated.SchemaVersion,
		Parameters: map[string]any{
			"recipient": "recipient@example.test",
			"subject":   "ASCP integration message",
			"body":      "This message uses the full contract flow.",
		},
		InputFiles: files,
		Billing:    billingPreference,
	}, "idem-prepare-"+ascp.MustNewID("test"))
	if err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	return quote
}

func newHarness(t *testing.T) (*ascpclient.Client, *ascp.Signer, server.Identity, func()) {
	t.Helper()
	signer, err := ascp.NewSigner("test-signing-key")
	if err != nil {
		t.Fatalf("NewSigner: %v", err)
	}
	identity := server.Identity{
		Actor:     ascp.EntityRef{Type: "agent", ID: "test-agent"},
		Principal: ascp.EntityRef{Type: "user", ID: "test-user"},
		Scopes:    map[string]bool{"email.read": true, "email.send": true},
	}
	engine, err := server.New(server.Config{
		Manifest: ascp.Manifest{
			ServiceID:      "urn:ascp:service:reference-email",
			ServiceName:    "ASCP test email service",
			JWKSURI:        "/.well-known/jwks.json",
			AuthSchemes:    []string{"bearer-test"},
			BillingModes:   []ascp.BillingMode{ascp.BillingFree, ascp.BillingPayNow, ascp.BillingSubscription},
			PaymentSchemes: []string{"urn:ascp:billing:mock-pay-now"},
			Features:       map[string]bool{},
		},
		Authenticator: server.StaticAuthenticator{Token: testToken, Identity: identity},
		Authorization: server.DemoAuthorizationVerifier{},
		Service:       email.NewService(),
		Store:         server.NewMemoryStore(),
		Idempotency:   server.NewIdempotencyStore(),
		Audit:         audit.NewLog(signer),
		Billing:       billing.NewMockProcessor(),
		Files:         server.NewMemoryFileStore(10 << 20),
		Signer:        signer,
	})
	if err != nil {
		t.Fatalf("server.New: %v", err)
	}
	httpServer := httptest.NewServer(engine)
	client, err := ascpclient.New(httpServer.URL, testToken)
	if err != nil {
		httpServer.Close()
		t.Fatalf("client.New: %v", err)
	}
	return client, signer, identity, httpServer.Close
}
