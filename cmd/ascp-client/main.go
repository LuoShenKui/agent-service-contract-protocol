// Command ascp-client demonstrates both ASCP 0.2 interaction paths:
//
//  1. A one-call direct read that needs no quote and no idempotency key.
//  2. A full signed contract for an irreversible email send, including an
//     optional uploaded attachment and a selectable billing arrangement.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/audit"
	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/client"
)

func main() {
	baseURL := flag.String("base-url", "http://localhost:8080", "ASCP service base URL")
	token := flag.String("token", "ascp-demo-token", "Bearer token delegated to this client agent")
	recipient := flag.String("to", "recipient@example.com", "Recipient for the contract-flow email")
	subject := flag.String("subject", "ASCP 0.2 demonstration", "Email subject")
	body := flag.String("body", "This message was delivered through a signed ASCP service contract.", "Plain-text email body")
	attachmentPath := flag.String("attachment", "", "Optional local attachment path")
	billingMode := flag.String("billing", string(ascp.BillingPayNow), "Billing mode: pay_now, prepaid_balance, subscription, postpaid_account, monthly_invoice, clearing_account, sponsored, or external_settlement")
	arrangementRef := flag.String("arrangement-ref", "", "Existing subscription, balance, invoice, sponsor, or clearing relationship")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	protocolClient, err := client.New(*baseURL, *token)
	fatalIf(err)

	// Discovery is compact and cacheable. It gives the caller a task list without
	// loading every platform-internal API or parameter schema into model context.
	manifest, err := protocolClient.Manifest(ctx)
	fatalIf(err)
	catalog, err := protocolClient.Capabilities(ctx, ascp.CapabilityQuery{Limit: 20})
	fatalIf(err)
	printJSON("SERVICE MANIFEST", manifest)
	printJSON("COMPACT CAPABILITY CATALOG", catalog)

	// Direct flow: a complete, free, read-only request can be answered in one
	// call. No negotiate/prepare/commit sequence and no idempotency key are needed.
	directRequest := ascp.DirectInvocationRequest{
		Intent: "email.latest.read",
		Parameters: map[string]any{
			"include_body": true,
		},
	}
	directResponse, err := protocolClient.Invoke(ctx, directRequest, "")
	fatalIf(err)
	directKey, err := protocolClient.FetchPublicKey(ctx, directResponse.Receipt.Signature.KeyID)
	fatalIf(err)
	fatalIf(client.VerifyInvocationReceiptForRequest(directResponse.Receipt, directRequest, directKey))
	directEvents, _, err := protocolClient.InvocationAudit(ctx, directResponse.InvocationID)
	fatalIf(err)
	fatalIf(audit.Verify(directEvents, directKey))
	fatalIf(audit.VerifyInvocationReceiptAnchor(directEvents, directResponse.Receipt))
	printJSON("ONE-CALL DIRECT RESULT", directResponse)

	// Uploads are separate from protocol JSON. Only a digest-bound, owner-bound
	// FileRef enters the signed quote, keeping attachment bytes out of the model
	// context and out of repeated JSON transport.
	var inputFiles []ascp.FileRef
	if strings.TrimSpace(*attachmentPath) != "" {
		content, readErr := os.ReadFile(*attachmentPath)
		fatalIf(readErr)
		digest := ascp.SHA256Digest(content)
		ticket, prepareErr := protocolClient.PrepareUpload(ctx, ascp.FileUploadRequest{
			Name:      fileBaseName(*attachmentPath),
			MediaType: "application/octet-stream",
			Size:      int64(len(content)),
			Digest:    digest,
			Purpose:   "email_attachment",
		}, ascp.MustNewID("idem"))
		fatalIf(prepareErr)
		ref, uploadErr := protocolClient.UploadFile(ctx, ticket, "application/octet-stream", digest, content)
		fatalIf(uploadErr)
		inputFiles = append(inputFiles, ref)
		printJSON("UPLOADED FILE REFERENCE", ref)
	}

	actor := ascp.EntityRef{Type: "agent", ID: "demo-client"}
	principal := ascp.EntityRef{Type: "user", ID: "demo-user"}
	selectedMode := ascp.BillingMode(*billingMode)

	// Full contract, stage 1: ask whether the irreversible task is supported and
	// obtain only the parameters, scopes, file policy, and billing choices needed
	// for this selected intent.
	negotiation, err := protocolClient.Negotiate(ctx, ascp.NegotiationRequest{
		Intent:     "email.send",
		Goal:       "send one email with optional attachments",
		Actor:      actor,
		Principal:  principal,
		Budget:     &ascp.Money{Currency: "USD", Amount: "0.02"},
		InputFiles: inputFiles,
	}, ascp.MustNewID("idem"))
	fatalIf(err)
	if !negotiation.Supported {
		fatalIf(fmt.Errorf("service rejected capability: %s", negotiation.Reason))
	}
	printJSON("CONTRACT NEGOTIATION", negotiation)

	// Full contract, stage 2: submit exact task details and the preferred billing
	// relationship. Prepare is side-effect-free and returns a signed quote.
	quote, err := protocolClient.Prepare(ctx, ascp.PrepareRequest{
		OfferID:       negotiation.OfferID,
		SchemaVersion: negotiation.SchemaVersion,
		Parameters: map[string]any{
			"recipient": *recipient,
			"subject":   *subject,
			"body":      *body,
		},
		InputFiles: inputFiles,
		Execution: ascp.ExecutionPreferences{
			MaxPrice: &ascp.Money{Currency: "USD", Amount: "0.02"},
		},
		Billing: ascp.BillingPreference{
			Mode:           selectedMode,
			ArrangementRef: *arrangementRef,
		},
	}, ascp.MustNewID("idem"))
	fatalIf(err)
	quoteKey, err := protocolClient.FetchPublicKey(ctx, quote.Signature.KeyID)
	fatalIf(err)
	fatalIf(client.VerifyQuoteForManifest(quote, manifest, quoteKey))
	printJSON("VERIFIED SIGNED QUOTE", quote)

	// Full contract, stage 3: approval is bound to the exact signed quote. A
	// separate pay-now authorization is supplied only when the quote requires it;
	// subscriptions, monthly invoices, prepaid balances, and similar standing
	// arrangements use their existing arrangement reference instead.
	now := time.Now().UTC()
	commitRequest := ascp.CommitRequest{
		QuoteID:     quote.QuoteID,
		QuoteDigest: quote.Signature.PayloadDigest,
		Authorization: ascp.AuthorizationEvidence{
			Type:          "explicit_user_confirmation",
			Reference:     "demo-approval-" + ascp.MustNewID("approval"),
			PrincipalID:   principal.ID,
			Audience:      quote.ServiceID,
			ApprovedAt:    now,
			ExpiresAt:     now.Add(5 * time.Minute),
			BindingDigest: quote.Signature.PayloadDigest,
		},
		ClientTaskID: ascp.MustNewID("client-task"),
	}
	if quote.BillingTerms.AuthorizationRequired {
		commitRequest.BillingAuthorization = &ascp.BillingAuthorization{
			Mode:             quote.BillingTerms.Mode,
			ArrangementRef:   quote.BillingTerms.ArrangementRef,
			AuthorizationRef: "mockpay_" + ascp.MustNewID("auth"),
			Payer:            principal,
			Audience:         quote.ServiceID,
			MaximumAmount:    &quote.PriceCeiling,
			ExpiresAt:        now.Add(5 * time.Minute),
			BindingDigest:    quote.Signature.PayloadDigest,
			Usage:            "single_use",
		}
	}

	committed, err := protocolClient.Commit(ctx, commitRequest, ascp.MustNewID("idem"))
	fatalIf(err)
	if committed.Task.Receipt != nil {
		fatalIf(client.VerifyReceiptForQuote(*committed.Task.Receipt, quote, quoteKey))
	}
	printJSON("CONTRACT EXECUTION RESULT", committed)

	contractEvents, root, err := protocolClient.Audit(ctx, committed.Task.TaskID)
	fatalIf(err)
	fatalIf(audit.Verify(contractEvents, quoteKey))
	if committed.Task.Receipt != nil {
		fatalIf(audit.VerifyReceiptAnchor(contractEvents, *committed.Task.Receipt))
	}
	fmt.Printf("\nAUDIT VERIFIED: %d contract events, root=%s\n", len(contractEvents), root)
}

func fileBaseName(path string) string {
	// Handle both Unix and Windows separators without importing platform-specific
	// path semantics for a command that may prepare a remote filename.
	path = strings.ReplaceAll(path, "\\", "/")
	parts := strings.Split(path, "/")
	return parts[len(parts)-1]
}

func printJSON(label string, value any) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	fatalIf(err)
	fmt.Printf("\n=== %s ===\n%s\n", label, encoded)
}

func fatalIf(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
