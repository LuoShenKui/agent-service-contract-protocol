# ASCP examples

ASCP 0.2 has two primary paths.

## One-call Direct Flow

A complete, authorized, low-risk request can be sent directly:

```bash
curl --fail-with-body --silent --show-error \
  -X POST "http://localhost:8080/v1/invoke" \
  -H "Authorization: Bearer ascp-demo-token" \
  -H "ASCP-Version: 0.2" \
  -H "Content-Type: application/ascp+json" \
  --data-binary @examples/requests/direct-read-request.json | jq .
```

The free read does not require `Options`, a quote, payment, or an
`Idempotency-Key`. A side-effecting Direct capability may require a key, standing
approval, and a standing billing arrangement.

## Optional Options preflight

Use `POST /v1/options` only when the caller needs task-specific parameter,
flow, file, permission, or billing information. It is side-effect-free and does
not create authority or a billable task.

## Full Contract Flow

Consequential work uses:

```text
Negotiate -> Prepare signed quote -> Commit approved quote
```

`commit-request.json` demonstrates per-call `pay_now` authorization.
`commit-subscription-request.json` demonstrates a standing arrangement: the
signed quote selects the subscription and Commit contains no per-call payment
token.

## Files and attachments

`file-upload-request.json` declares name, type, size, and SHA-256 digest before
upload. The provider returns a short-lived upload ticket. Bytes are uploaded to
the ticket URL; only the resulting `FileRef` enters Direct or Contract JSON.
This avoids base64 expansion, repeated model-context transfer, and unverified
attachment substitution.

Run the complete verified demonstration:

```bash
go run ./cmd/ascp-server

go run ./cmd/ascp-client \
  -to recipient@example.com \
  -attachment ./README.md
```
