// Command ascp-conformance runs the ASCP 0.2 reference profile against a live
// service. It tests the compact direct path, optional preflight, file transfer,
// the full contract path, standing billing, idempotency, signatures, and audit.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/audit"
	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/client"
)

func main() {
	baseURL := flag.String("base-url", "http://localhost:8080", "ASCP service base URL")
	token := flag.String("token", "ascp-demo-token", "Bearer token")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	protocolClient, err := client.New(*baseURL, *token)
	must("construct client", err)
	manifest, err := protocolClient.Manifest(ctx)
	must("discover service manifest", err)

	// Check 1: capability discovery is compact and exposes both direct and
	// contract tasks without exposing provider-internal API definitions.
	catalog, err := protocolClient.Capabilities(ctx, ascp.CapabilityQuery{Limit: 20})
	must("read capability catalog", err)
	if !hasCapability(catalog, "email.latest.read", ascp.FlowDirect) || !hasCapability(catalog, "email.send", ascp.FlowContract) {
		fail("capability catalog does not expose both reference flows")
	}
	pass("compact capability catalog")

	// Check 2: a complete free read is one request and one response. It neither
	// requires Options nor an idempotency key.
	directRequest := ascp.DirectInvocationRequest{
		Intent:     "email.latest.read",
		Parameters: map[string]any{"include_body": true},
	}
	direct, err := protocolClient.Invoke(ctx, directRequest, "")
	must("invoke direct read", err)
	key, err := protocolClient.FetchPublicKey(ctx, direct.Receipt.Signature.KeyID)
	must("fetch direct receipt key", err)
	must("verify direct receipt", client.VerifyInvocationReceiptForRequest(direct.Receipt, directRequest, key))
	events, root, err := protocolClient.InvocationAudit(ctx, direct.InvocationID)
	must("read direct audit", err)
	must("verify direct audit", audit.Verify(events, key))
	must("verify direct audit anchor", audit.VerifyInvocationReceiptAnchor(events, direct.Receipt))
	if root == "" {
		fail("direct audit root is empty")
	}
	pass("one-call free direct invocation")

	// Check 3: Options is optional but can explain why an irreversible operation
	// requires the full contract path and what fields/files/billing it accepts.
	options, err := protocolClient.Options(ctx, ascp.OptionsRequest{Intent: "email.send"})
	must("read semantic options", err)
	if options.DirectEligible || options.RecommendedFlow != ascp.FlowContract || len(options.BillingOptions) < 2 || !options.InputFilePolicy.Accepted {
		fail("email.send options omitted contract, billing, or attachment requirements")
	}
	pass("optional semantic preflight")

	// Check 4: attempting an irreversible send through the direct endpoint must
	// fail closed with contract_required rather than silently executing.
	_, err = protocolClient.Invoke(ctx, ascp.DirectInvocationRequest{
		Intent: "email.send",
		Parameters: map[string]any{
			"recipient": "conformance@example.com",
			"subject":   "must not send directly",
			"body":      "This direct request must be rejected.",
		},
	}, "conformance-direct-send-0001")
	var contractRequired *ascp.Problem
	if !errors.As(err, &contractRequired) || contractRequired.Code != ascp.ErrContractRequired {
		fail("expected contract_required for direct email.send, got %v", err)
	}
	pass("unsafe direct task fails closed")

	// Check 5: upload bytes out-of-band and carry only a verified FileRef through
	// the signed task contract.
	content := []byte("ASCP 0.2 conformance attachment\n")
	digest := ascp.SHA256Digest(content)
	ticket, err := protocolClient.PrepareUpload(ctx, ascp.FileUploadRequest{
		Name:      "conformance.txt",
		MediaType: "text/plain",
		Size:      int64(len(content)),
		Digest:    digest,
		Purpose:   "email_attachment",
	}, "conformance-upload-prepare-0001")
	must("prepare file upload", err)
	fileRef, err := protocolClient.UploadFile(ctx, ticket, "text/plain", digest, content)
	must("upload file", err)
	downloadedRef, downloaded, err := protocolClient.DownloadFile(ctx, fileRef.FileID, 1024)
	must("download staged file", err)
	if !reflect.DeepEqual(content, downloaded) || downloadedRef.Digest != digest {
		fail("downloaded staged file differs from upload")
	}
	pass("digest-bound file transfer")

	actor := ascp.EntityRef{Type: "agent", ID: "demo-client"}
	principal := ascp.EntityRef{Type: "user", ID: "demo-user"}
	negotiationRequest := ascp.NegotiationRequest{
		Intent:     "email.send",
		Goal:       "send a conformance-test email",
		Actor:      actor,
		Principal:  principal,
		Budget:     &ascp.Money{Currency: "USD", Amount: "0.02"},
		InputFiles: []ascp.FileRef{fileRef},
	}

	// Check 6: exact requests replay and changed requests conflict under the same
	// idempotency key.
	negotiationKey := "conformance-negotiate-0001"
	first, err := protocolClient.Negotiate(ctx, negotiationRequest, negotiationKey)
	must("negotiate", err)
	second, err := protocolClient.Negotiate(ctx, negotiationRequest, negotiationKey)
	must("replay negotiation", err)
	if !reflect.DeepEqual(first, second) {
		fail("idempotency replay changed the negotiation response")
	}
	conflicting := negotiationRequest
	conflicting.Goal = "different payload under the same key"
	_, err = protocolClient.Negotiate(ctx, conflicting, negotiationKey)
	var conflict *ascp.Problem
	if !errors.As(err, &conflict) || conflict.Code != ascp.ErrIdempotencyConflict {
		fail("expected idempotency_conflict, got %v", err)
	}
	pass("exact replay and conflict detection")

	// Check 7: prepare preserves files and a standing subscription in the signed
	// quote. No per-call payment token is needed for this billing arrangement.
	quote, err := protocolClient.Prepare(ctx, ascp.PrepareRequest{
		OfferID:       first.OfferID,
		SchemaVersion: first.SchemaVersion,
		Parameters: map[string]any{
			"recipient": "conformance@example.com",
			"subject":   "ASCP 0.2 conformance test",
			"body":      "This message verifies direct, contract, billing, file, and audit behavior.",
		},
		InputFiles: []ascp.FileRef{fileRef},
		Execution:  ascp.ExecutionPreferences{MaxPrice: &ascp.Money{Currency: "USD", Amount: "0.02"}},
		Billing: ascp.BillingPreference{
			Mode:           ascp.BillingSubscription,
			ArrangementRef: "subscription_conformance",
		},
	}, "conformance-prepare-0001")
	must("prepare signed quote", err)
	publicKey, err := protocolClient.FetchPublicKey(ctx, quote.Signature.KeyID)
	must("fetch quote verification key", err)
	must("verify signed quote", client.VerifyQuoteForManifest(quote, manifest, publicKey))
	if len(quote.InputFiles) != 1 || quote.InputFiles[0].Digest != digest || quote.BillingTerms.Mode != ascp.BillingSubscription || quote.BillingTerms.AuthorizationRequired {
		fail("signed quote did not preserve attachment or subscription terms")
	}
	pass("signed file and standing-billing contract")

	// Check 8: commit binds independent approval to the exact quote digest and
	// creates one durable task. A repeated commit returns the same task.
	now := time.Now().UTC()
	commitRequest := ascp.CommitRequest{
		QuoteID:     quote.QuoteID,
		QuoteDigest: quote.Signature.PayloadDigest,
		Authorization: ascp.AuthorizationEvidence{
			Type:          "explicit_user_confirmation",
			Reference:     "demo-approval-conformance",
			PrincipalID:   principal.ID,
			Audience:      quote.ServiceID,
			ApprovedAt:    now,
			ExpiresAt:     now.Add(5 * time.Minute),
			BindingDigest: quote.Signature.PayloadDigest,
		},
		ClientTaskID: "conformance-client-task",
	}
	commitKey := "conformance-commit-000001"
	committed, err := protocolClient.Commit(ctx, commitRequest, commitKey)
	must("commit task", err)
	replayed, err := protocolClient.Commit(ctx, commitRequest, commitKey)
	must("replay commit", err)
	if replayed.Task.TaskID != committed.Task.TaskID {
		fail("commit replay created a second task")
	}
	if committed.Task.Receipt == nil || committed.Task.Billing == nil {
		fail("completed task lacks receipt or billing record")
	}
	must("verify signed receipt", client.VerifyReceiptForQuote(*committed.Task.Receipt, quote, publicKey))
	pass("quote-bound commit and signed receipt")

	// Check 9: all provider decisions and transitions remain independently
	// verifiable through the signed hash-chained task audit export.
	taskEvents, taskRoot, err := protocolClient.Audit(ctx, committed.Task.TaskID)
	must("read task audit trail", err)
	must("verify task audit trail", audit.Verify(taskEvents, publicKey))
	must("verify receipt audit anchor", audit.VerifyReceiptAnchor(taskEvents, *committed.Task.Receipt))
	if taskRoot == "" {
		fail("task audit root is empty")
	}
	pass(fmt.Sprintf("signed task audit chain (%d events)", len(taskEvents)))

	fmt.Println("\nASCP 0.2 reference profile: PASS")
}

func hasCapability(catalog ascp.CapabilityCatalog, intent string, flow ascp.Flow) bool {
	for _, capability := range catalog.Capabilities {
		if capability.Intent != intent {
			continue
		}
		for _, supported := range capability.SupportedFlows {
			if supported == flow {
				return true
			}
		}
	}
	return false
}

func pass(check string) {
	fmt.Println("PASS:", check)
}

func must(operation string, err error) {
	if err != nil {
		fail("%s: %v", operation, err)
	}
}

func fail(format string, arguments ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", arguments...)
	os.Exit(1)
}
