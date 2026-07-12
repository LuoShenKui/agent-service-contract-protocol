// Package client implements an ASCP HTTP client with strict error handling,
// automatic version headers, and quote/receipt verification helpers.
package client

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/LuoShenKui/agent-service-contract-protocol/pkg/ascp"
)

// Client is safe for concurrent use when its HTTPClient is safe for concurrent
// use, as net/http.Client is by default.
type Client struct {
	BaseURL    string
	Token      string
	HTTPClient *http.Client
}

// New constructs a client and validates the service URL.
func New(baseURL, token string) (*Client, error) {
	parsed, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, fmt.Errorf("invalid ASCP base URL %q", baseURL)
	}
	if parsed.Path != "" && parsed.Path != "/" {
		return nil, errors.New("ASCP base URL must not contain a path prefix")
	}
	if parsed.Scheme != "https" {
		if parsed.Scheme != "http" || !isLoopbackHost(parsed.Hostname()) {
			return nil, errors.New("ASCP requires HTTPS except for loopback development endpoints")
		}
	}
	return &Client{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
			CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
				// Authenticated ASCP requests are never redirected automatically.
				// This prevents bearer-token and request-body leakage to another origin.
				return http.ErrUseLastResponse
			},
		},
	}, nil
}

// Manifest returns the service's small discovery document. Callers should bind
// a verified quote to this service identity before approval or payment.
func (c *Client) Manifest(ctx context.Context) (ascp.Manifest, error) {
	var manifest ascp.Manifest
	err := c.doJSON(ctx, http.MethodGet, "/.well-known/ascp", nil, "", &manifest)
	return manifest, err
}

// Capabilities returns the platform agent's compact task catalog. Callers can
// cache the result and request detailed parameters only for a selected intent.
func (c *Client) Capabilities(ctx context.Context, query ascp.CapabilityQuery) (ascp.CapabilityCatalog, error) {
	values := url.Values{}
	if query.Query != "" {
		values.Set("q", query.Query)
	}
	if query.Intent != "" {
		values.Set("intent", query.Intent)
	}
	if query.Cursor != "" {
		values.Set("cursor", query.Cursor)
	}
	if query.Limit > 0 {
		values.Set("limit", fmt.Sprintf("%d", query.Limit))
	}
	path := "/.well-known/ascp/capabilities"
	if encoded := values.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var catalog ascp.CapabilityCatalog
	err := c.doJSON(ctx, http.MethodGet, path, nil, "", &catalog)
	return catalog, err
}

// Options performs the optional semantic preflight. It does not create a quote,
// authority, charge, task, or provider side effect.
func (c *Client) Options(ctx context.Context, request ascp.OptionsRequest) (ascp.OptionsResponse, error) {
	var response ascp.OptionsResponse
	err := c.doJSON(ctx, http.MethodPost, "/v1/options", request, "", &response)
	return response, err
}

// Invoke runs the compact one-call path. The idempotency key may be empty for a
// provider-declared read-only operation; side-effecting direct operations must
// supply a key.
func (c *Client) Invoke(ctx context.Context, request ascp.DirectInvocationRequest, idempotencyKey string) (ascp.DirectInvocationResponse, error) {
	var response ascp.DirectInvocationResponse
	err := c.doJSON(ctx, http.MethodPost, "/v1/invoke", request, idempotencyKey, &response)
	return response, err
}

// Negotiate performs the first handshake.
func (c *Client) Negotiate(ctx context.Context, request ascp.NegotiationRequest, idempotencyKey string) (ascp.NegotiationResponse, error) {
	var response ascp.NegotiationResponse
	err := c.doJSON(ctx, http.MethodPost, "/v1/negotiate", request, idempotencyKey, &response)
	return response, err
}

// Prepare performs the second handshake and obtains a signed binding quote.
func (c *Client) Prepare(ctx context.Context, request ascp.PrepareRequest, idempotencyKey string) (ascp.Quote, error) {
	var quote ascp.Quote
	err := c.doJSON(ctx, http.MethodPost, "/v1/prepare", request, idempotencyKey, &quote)
	return quote, err
}

// Commit performs the third handshake. The service atomically accepts the quote,
// validates authorization, reserves payment, and creates a task.
func (c *Client) Commit(ctx context.Context, request ascp.CommitRequest, idempotencyKey string) (ascp.CommitResponse, error) {
	var response ascp.CommitResponse
	err := c.doJSON(ctx, http.MethodPost, "/v1/commit", request, idempotencyKey, &response)
	return response, err
}

// GetTask reads the current durable task state.
func (c *Client) GetTask(ctx context.Context, taskID string) (ascp.Task, error) {
	var task ascp.Task
	err := c.doJSON(ctx, http.MethodGet, "/v1/tasks/"+url.PathEscape(taskID), nil, "", &task)
	return task, err
}

// Cancel requests cancellation with optimistic concurrency.
func (c *Client) Cancel(ctx context.Context, taskID string, request ascp.CancelRequest, idempotencyKey string) (ascp.Task, error) {
	var task ascp.Task
	err := c.doJSON(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(taskID)+"/cancel", request, idempotencyKey, &task)
	return task, err
}

// Audit retrieves the complete signed audit chain for a task.
func (c *Client) Audit(ctx context.Context, taskID string) ([]ascp.AuditEvent, string, error) {
	var response struct {
		TaskID string            `json:"task_id"`
		Events []ascp.AuditEvent `json:"events"`
		Root   string            `json:"root"`
	}
	err := c.doJSON(ctx, http.MethodGet, "/v1/tasks/"+url.PathEscape(taskID)+"/audit", nil, "", &response)
	return response.Events, response.Root, err
}

// GetInvocation retrieves a previously completed or accepted direct invocation.
func (c *Client) GetInvocation(ctx context.Context, invocationID string) (ascp.DirectInvocationResponse, error) {
	var response ascp.DirectInvocationResponse
	err := c.doJSON(ctx, http.MethodGet, "/v1/invocations/"+url.PathEscape(invocationID), nil, "", &response)
	return response, err
}

// InvocationAudit retrieves the signed audit chain for a direct invocation.
func (c *Client) InvocationAudit(ctx context.Context, invocationID string) ([]ascp.AuditEvent, string, error) {
	var response struct {
		InvocationID string            `json:"invocation_id"`
		Events       []ascp.AuditEvent `json:"events"`
		Root         string            `json:"root"`
	}
	err := c.doJSON(ctx, http.MethodGet, "/v1/invocations/"+url.PathEscape(invocationID)+"/audit", nil, "", &response)
	return response.Events, response.Root, err
}

// PrepareUpload obtains a short-lived, single-file upload credential. The
// caller sends bytes separately and uses only the returned FileRef in ASCP JSON.
func (c *Client) PrepareUpload(ctx context.Context, request ascp.FileUploadRequest, idempotencyKey string) (ascp.FileUploadTicket, error) {
	var ticket ascp.FileUploadTicket
	err := c.doJSON(ctx, http.MethodPost, "/v1/files/prepare-upload", request, idempotencyKey, &ticket)
	return ticket, err
}

// UploadFile uploads exact bytes to a prepared ticket. Redirects remain disabled
// so the bearer access token and upload credential cannot leak to another host.
func (c *Client) UploadFile(ctx context.Context, ticket ascp.FileUploadTicket, mediaType, digest string, content []byte) (ascp.FileRef, error) {
	uploadURL, err := c.resolveSameOriginURL(ticket.UploadURL)
	if err != nil {
		return ascp.FileRef{}, err
	}
	request, err := http.NewRequestWithContext(ctx, ticket.UploadMethod, uploadURL.String(), bytes.NewReader(content))
	if err != nil {
		return ascp.FileRef{}, fmt.Errorf("create upload request: %w", err)
	}
	request.Header.Set("Content-Type", mediaType)
	request.Header.Set("X-ASCP-Content-Digest", digest)
	request.Header.Set("X-ASCP-Upload-Token", ticket.UploadToken)
	request.Header.Set("ASCP-Version", ascp.ProtocolVersion)
	request.Header.Set("Accept", ascp.MediaType+", application/problem+json")
	if c.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}
	var ref ascp.FileRef
	if err := c.doRequest(request, &ref); err != nil {
		return ascp.FileRef{}, err
	}
	return ref, nil
}

// GetFile returns staged file metadata without downloading the bytes.
func (c *Client) GetFile(ctx context.Context, fileID string) (ascp.FileRef, error) {
	var ref ascp.FileRef
	err := c.doJSON(ctx, http.MethodGet, "/v1/files/"+url.PathEscape(fileID), nil, "", &ref)
	return ref, err
}

// DownloadFile retrieves a ready file with a caller-supplied safety limit.
func (c *Client) DownloadFile(ctx context.Context, fileID string, maximumBytes int64) (ascp.FileRef, []byte, error) {
	if maximumBytes <= 0 {
		return ascp.FileRef{}, nil, errors.New("maximumBytes must be positive")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL+"/v1/files/"+url.PathEscape(fileID)+"/content", nil)
	if err != nil {
		return ascp.FileRef{}, nil, err
	}
	request.Header.Set("ASCP-Version", ascp.ProtocolVersion)
	if c.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}
	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return ascp.FileRef{}, nil, err
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		data, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		problem, decodeErr := ascp.DecodeProblem(data)
		if decodeErr != nil {
			return ascp.FileRef{}, nil, fmt.Errorf("file download returned HTTP %d", response.StatusCode)
		}
		return ascp.FileRef{}, nil, problem
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, maximumBytes+1))
	if err != nil {
		return ascp.FileRef{}, nil, err
	}
	if int64(len(content)) > maximumBytes {
		return ascp.FileRef{}, nil, errors.New("download exceeds caller safety limit")
	}
	ref, err := c.GetFile(ctx, fileID)
	if err != nil {
		return ascp.FileRef{}, nil, err
	}
	if ascp.SHA256Digest(content) != ref.Digest {
		return ascp.FileRef{}, nil, errors.New("download digest does not match file metadata")
	}
	return ref, content, nil
}

func (c *Client) doRequest(request *http.Request, responseBody any) error {
	response, err := c.HTTPClient.Do(request)
	if err != nil {
		return fmt.Errorf("perform request: %w", err)
	}
	defer response.Body.Close()
	const maximumResponseBytes = 4 << 20
	data, err := io.ReadAll(io.LimitReader(response.Body, maximumResponseBytes+1))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if len(data) > maximumResponseBytes {
		return errors.New("ASCP response exceeds the 4 MiB client safety limit")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		problem, decodeErr := ascp.DecodeProblem(data)
		if decodeErr != nil {
			return fmt.Errorf("ASCP service returned HTTP %d with malformed problem details: %s", response.StatusCode, strings.TrimSpace(string(data)))
		}
		return problem
	}
	if responseBody == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, responseBody); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// FetchPublicKey discovers the service JWKS and returns the requested Ed25519
// key. A production client should additionally validate service identity and
// cache keys according to HTTP cache directives.
func (c *Client) FetchPublicKey(ctx context.Context, keyID string) (ed25519.PublicKey, error) {
	manifest, err := c.Manifest(ctx)
	if err != nil {
		return nil, err
	}

	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, err
	}
	jwksURL, err := url.Parse(manifest.JWKSURI)
	if err != nil {
		return nil, fmt.Errorf("parse JWKS URI: %w", err)
	}
	if jwksURL.IsAbs() {
		if !strings.EqualFold(jwksURL.Scheme, base.Scheme) || !strings.EqualFold(jwksURL.Host, base.Host) {
			return nil, errors.New("cross-origin JWKS URI requires explicit trust configuration")
		}
	} else {
		jwksURL = base.ResolveReference(jwksURL)
	}
	jwksPath := jwksURL.EscapedPath()
	if jwksURL.RawQuery != "" {
		jwksPath += "?" + jwksURL.RawQuery
	}
	var set ascp.JWKSet
	if err := c.doJSON(ctx, http.MethodGet, jwksPath, nil, "", &set); err != nil {
		return nil, err
	}
	for _, key := range set.Keys {
		if key.KeyID == keyID && key.KeyType == "OKP" && key.Curve == "Ed25519" && key.Alg == "EdDSA" {
			raw, err := base64.RawURLEncoding.DecodeString(key.X)
			if err != nil {
				return nil, fmt.Errorf("decode JWK: %w", err)
			}
			if len(raw) != ed25519.PublicKeySize {
				return nil, errors.New("invalid Ed25519 JWK length")
			}
			return ed25519.PublicKey(raw), nil
		}
	}
	return nil, fmt.Errorf("verification key %q not found", keyID)
}

// VerifyQuote verifies the embedded JWS and confirms that every unsigned
// response field exactly matches the signed projection. The signature envelope
// itself must not appear inside the protected payload.
func VerifyQuote(quote ascp.Quote, publicKey ed25519.PublicKey) error {
	if err := verifySignedProjection(quote, quote.Signature, publicKey, "quote"); err != nil {
		return err
	}
	return ascp.ValidateQuoteSemantics(quote)
}

// VerifyQuoteForManifest additionally binds the signed quote to the provider
// identity discovered from the service manifest. This prevents a valid quote
// from one provider being presented as a contract from another provider.
func VerifyQuoteForManifest(quote ascp.Quote, manifest ascp.Manifest, publicKey ed25519.PublicKey) error {
	if err := VerifyQuote(quote, publicKey); err != nil {
		return err
	}
	if manifest.ServiceID == "" || quote.ServiceID != manifest.ServiceID {
		return errors.New("quote service identity does not match the discovered manifest")
	}
	versionSupported := false
	for _, version := range manifest.Versions {
		if version == quote.ProtocolVersion {
			versionSupported = true
			break
		}
	}
	if !versionSupported {
		return fmt.Errorf("quote protocol version %q is not advertised by the service", quote.ProtocolVersion)
	}
	return nil
}

func (c *Client) resolveSameOriginURL(rawURL string) (*url.URL, error) {
	base, err := url.Parse(c.BaseURL)
	if err != nil {
		return nil, err
	}
	target, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse service URL: %w", err)
	}
	if !target.IsAbs() {
		target = base.ResolveReference(target)
	}
	if !strings.EqualFold(target.Scheme, base.Scheme) || !strings.EqualFold(target.Host, base.Host) || target.User != nil {
		return nil, errors.New("service URL must remain on the configured ASCP origin")
	}
	return target, nil
}

// VerifyReceipt verifies the embedded JWS and response projection.
func VerifyReceipt(receipt ascp.Receipt, publicKey ed25519.PublicKey) error {
	return verifySignedProjection(receipt, receipt.Signature, publicKey, "receipt")
}

// VerifyReceiptForQuote verifies the receipt signature and then binds the
// settlement record to the already-verified quote and its signed price ceiling.
func VerifyReceiptForQuote(receipt ascp.Receipt, quote ascp.Quote, publicKey ed25519.PublicKey) error {
	if err := VerifyReceipt(receipt, publicKey); err != nil {
		return err
	}
	return ascp.ValidateReceiptAgainstQuote(receipt, quote)
}

// VerifyInvocationReceipt checks the signature and protocol-level semantics of
// a direct invocation receipt.
func VerifyInvocationReceipt(receipt ascp.InvocationReceipt, publicKey ed25519.PublicKey) error {
	if err := verifySignedProjection(receipt, receipt.Signature, publicKey, "invocation receipt"); err != nil {
		return err
	}
	return ascp.ValidateInvocationReceipt(receipt)
}

// VerifyInvocationReceiptForRequest additionally binds the receipt to the exact
// JSON request representation emitted by this Go client.
func VerifyInvocationReceiptForRequest(receipt ascp.InvocationReceipt, request ascp.DirectInvocationRequest, publicKey ed25519.PublicKey) error {
	if err := VerifyInvocationReceipt(receipt, publicKey); err != nil {
		return err
	}
	encoded, err := json.Marshal(request)
	if err != nil {
		return err
	}
	if receipt.RequestDigest != ascp.SHA256Digest(encoded) {
		return errors.New("invocation receipt does not match the outbound request digest")
	}
	return nil
}

func verifySignedProjection(value any, signature ascp.Signature, publicKey ed25519.PublicKey, description string) error {
	var signed map[string]json.RawMessage
	if err := ascp.VerifyJSON(signature, publicKey, &signed); err != nil {
		return err
	}
	if _, included := signed["signature"]; included {
		return fmt.Errorf("%s signed payload unexpectedly contains the signature envelope", description)
	}
	expected, err := ascp.SigningProjection(value)
	if err != nil {
		return fmt.Errorf("create %s signing projection: %w", description, err)
	}
	return compareJSON(expected, signed, description)
}

func (c *Client) doJSON(ctx context.Context, method, path string, requestBody any, idempotencyKey string, responseBody any) error {
	var body io.Reader
	if requestBody != nil {
		encoded, err := json.Marshal(requestBody)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	request, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	request.Header.Set("Accept", ascp.MediaType+", application/problem+json")
	request.Header.Set("ASCP-Version", ascp.ProtocolVersion)
	if requestBody != nil {
		request.Header.Set("Content-Type", ascp.MediaType)
	}
	if c.Token != "" {
		request.Header.Set("Authorization", "Bearer "+c.Token)
	}
	if idempotencyKey != "" {
		request.Header.Set("Idempotency-Key", idempotencyKey)
	}

	return c.doRequest(request, responseBody)
}

// isLoopbackHost permits plaintext HTTP only for local development and tests.
// Production service identities always use certificate-authenticated HTTPS.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func compareJSON(left, right any, description string) error {
	leftJSON, err := json.Marshal(left)
	if err != nil {
		return err
	}
	rightJSON, err := json.Marshal(right)
	if err != nil {
		return err
	}
	if !bytes.Equal(leftJSON, rightJSON) {
		return fmt.Errorf("%s response fields do not match the signed payload", description)
	}
	return nil
}
