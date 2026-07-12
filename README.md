# Agent Service Contract Protocol (ASCP)

[![Protocol](https://img.shields.io/badge/Protocol-Draft%200.2-orange.svg)](spec/ASCP-0.2.md)
[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.23%2B-00ADD8.svg)](go.mod)

**ASCP is an open protocol for calling platform-owned service agents with two
paths: one-call execution for complete low-risk requests, and signed service
contracts for paid, variable, irreversible, or otherwise consequential work.**

A client agent should not need Gmail's entire internal API schema to ask for the
latest email. It should be able to send one compact request and receive one
answer. When it asks Gmail to send an external message, attach files, spend
money, or exercise elevated authority, the provider can require a signed quote,
explicit approval, billing validation, idempotent execution, and an auditable
receipt.

> **Status:** `v0.2.0-draft.1` is a working draft and Go reference
> implementation. It is suitable for public review, interoperability work, and
> controlled integrations. It has not received independent security
> certification, payment-provider certification, or broad multi-vendor
> interoperability validation.

## The two flows

### 1. Direct Flow: ask and answer

```text
Client agent                         Platform-owned agent
     |                                        |
     | "Read my latest email"                 |
     |--------------------------------------->|
     | Auth/scopes/policy checked internally  |
     |<---------------------------------------|
     | Result + signed receipt                |
```

A complete, authorized, provider-declared low-risk request uses:

```http
POST /v1/invoke
```

For a free read-only operation, this is one request and one response. It does
not require a capability-schema dump, Options, quote, payment, or idempotency
key. The provider still authenticates the caller, checks delegated scopes,
limits output, records an audit chain, and signs the result receipt.

A provider may also permit a side-effecting Direct capability when all terms are
already fixed by standing authority and billing arrangements. Such a call must
require `Idempotency-Key` and any provider-declared approval or mandate.

### Optional Options preflight

```http
POST /v1/options
```

Options is not a mandatory handshake. It exists for cases where the client does
not know:

- the exact intent;
- missing parameters;
- whether Direct Flow is currently allowed;
- required scopes;
- accepted files;
- available billing arrangements;
- whether a signed contract is required.

Options is side-effect-free and creates no quote, charge, task, or authority.
Ordinary HTTP `OPTIONS /v1/invoke` returns only transport hints.

### 2. Contract Flow: consequential work

```text
Client agent                         Platform-owned agent
     |                                        |
     | Negotiate: can you do this task?       |
     |--------------------------------------->|
     | Minimum fields, scopes, files, billing |
     |<---------------------------------------|
     |                                        |
     | Prepare: exact task and constraints    |
     |--------------------------------------->|
     | Signed quote + effect preview          |
     |<---------------------------------------|
     |                                        |
     | Verify, obtain approval, choose funding|
     |                                        |
     | Commit: accept exact signed quote      |
     |--------------------------------------->|
     | Durable task + receipt + audit         |
     |<---------------------------------------|
```

The provider, not the external model, understands its internal fields, rules,
APIs, data provenance, and execution system. The external agent describes a
task; the platform agent returns only the minimum task contract needed to
approve and execute it.

## Compact capability catalog

Every provider publishes a cacheable task catalog at:

```http
GET /.well-known/ascp/capabilities
```

A capability descriptor includes:

- intent and task version;
- short summary;
- Direct and/or Contract support;
- side-effect and risk class;
- required delegated scopes;
- supported billing modes;
- parameter names, without full schemas;
- file policy;
- output modes;
- whether task-specific Options is available.

Full parameter schemas are returned only for the selected task through Options
or Negotiate. This avoids injecting dozens or hundreds of irrelevant tool
schemas into every model context.

## Billing is not synonymous with immediate payment

ASCP supports the following provider-neutral modes:

| Mode | Meaning |
|---|---|
| `free` | No service charge or billable usage |
| `pay_now` | Per-call tokenized authorization, reserve, then settle |
| `prepaid_balance` | Deduct from an existing stored balance |
| `subscription` | Record usage against a standing plan or allowance |
| `postpaid_account` | Record usage against an approved credit account |
| `monthly_invoice` | Append a line item to an invoice period |
| `clearing_account` | Settle through a unified platform clearing relationship |
| `sponsored` | Charge or account usage to an approved sponsor |
| `external_settlement` | Execute under an external commercial agreement |

`BillingAuthorization` is required only when the signed `BillingTerms` require
per-call authorization. A subscription, enterprise invoice agreement, prepaid
wallet, sponsor, or clearing account can be selected by an opaque standing
`arrangement_ref` and may not perform an immediate payment at all.

ASCP does not carry raw card, bank, wallet, or reusable account credentials.
Adapters integrate payment processors, AP2-like authorization, enterprise
billing, prepaid ledgers, or internal chargeback systems.

## Files and attachments

Large bytes do not belong in model context or repeated JSON bodies.

```text
1. Client declares file name, type, size, digest, and purpose.
2. Provider returns a short-lived, owner-bound upload ticket.
3. Client uploads bytes to the scoped target.
4. Provider verifies identity, token, media type, length, digest, expiry,
   readiness, and malware-scan state.
5. Only the resulting FileRef enters Direct or Contract JSON.
6. A Contract quote signs every input FileRef that can affect execution.
```

Core endpoints:

```text
POST /v1/files/prepare-upload
PUT  /v1/files/{file_id}/content
GET  /v1/files/{file_id}
GET  /v1/files/{file_id}/content
```

The reference email service accepts up to ten attachments and returns artifacts
as references rather than echoing all bytes through the protocol response.

## Reliability properties

Within its intended service-transaction scope, ASCP makes these constraints
explicit:

- **Fail-closed routing:** a task that is not eligible for Direct Flow returns
  `contract_required`; it is not silently executed.
- **Signed terms:** price, ceiling, billing mode, files, callback, effects,
  permissions, data use, confirmation, timing, actor, and principal are inside
  the signed quote.
- **Independent authority:** task text, retrieved content, or model output cannot
  create permission. Approval evidence is verified independently and bound to
  the exact request or quote digest.
- **Conditional and mandatory idempotency:** read-only Direct calls may omit a
  key; every unsafe retry path requires one. Unknown outcomes remain locked for
  reconciliation rather than being repeated.
- **Provider-neutral billing:** immediate payment is one mode, not the protocol's
  only commercial model.
- **Digest-bound files:** bytes are transferred separately and referenced by
  exact metadata included in the contract.
- **Signed receipts and audit:** Direct invocations and Contract tasks produce
  verifiable receipts anchored to append-only signed hash chains.
- **Minimal data movement:** context and artifacts can remain at the source
  behind scoped references.

## Relationship to MCP and A2A

ASCP is not a universal replacement for MCP or A2A.

| Protocol/layer | Best fit |
|---|---|
| Ordinary API / SQL | Deterministic provider-internal primitives |
| MCP | Exposing bounded tools and resources to a model or agent runtime |
| A2A | General agent discovery, communication, delegation, and progress |
| **ASCP** | Service acceptance, signed terms, authority, billing, files, idempotency, execution evidence, and audit |

A platform-owned agent can use SQL, ordinary APIs, queues, rules, models, or MCP
internally. It can be reached through A2A or another discovery layer. ASCP is the
transaction boundary used when the external caller needs a compact, enforceable,
auditable service interaction instead of the provider's complete internal tool
surface.

See [comparison](docs/comparison.md).

## Repository layout

```text
cmd/ascp-server/          Runnable reference service
cmd/ascp-client/          Direct + Contract demonstration client
cmd/ascp-conformance/     Live deployment conformance runner
internal/email/           Platform-owned email agent example
pkg/ascp/                 Wire types, validation, money, signatures
pkg/server/               HTTP engine, routing, idempotency, files, lifecycle
pkg/client/               Go client and verification helpers
pkg/billing/              Billing adapter boundary and deterministic mock
pkg/audit/                Signed append-only hash chain
schemas/                  JSON Schema 2020-12
openapi/                  OpenAPI 3.1 binding
spec/                     Normative core, security, billing, errors, states
examples/                 Direct, contract, billing, and file examples
conformance/              Machine-readable conformance case index
deploy/                    PostgreSQL and Kubernetes production blueprints
```

## Quick start

Requirements:

- Go 1.23 or newer;
- Python 3 with `PyYAML` and `jsonschema` for spec validation;
- optional `jq` for the curl examples.

Validate everything:

```bash
make check
```

Start the reference service:

```bash
go run ./cmd/ascp-server
```

Run a one-call read and a signed email send:

```bash
go run ./cmd/ascp-client -to recipient@example.com
```

Include a real attachment in the local demonstration:

```bash
go run ./cmd/ascp-client \
  -to recipient@example.com \
  -attachment ./README.md
```

Use a standing subscription instead of per-call pay-now:

```bash
go run ./cmd/ascp-client \
  -to recipient@example.com \
  -billing subscription \
  -arrangement-ref subscription_demo
```

Check a deployed endpoint:

```bash
go run ./cmd/ascp-conformance \
  -base-url http://localhost:8080 \
  -token ascp-demo-token
```

## Minimal Direct request

```json
{
  "intent": "email.latest.read",
  "parameters": {
    "include_body": true
  }
}
```

No `Options`, quote, payment, or idempotency key is required for the reference
read capability.

## Production boundary

The reference executable intentionally uses:

- a fixed demo bearer token;
- in-memory offers, quotes, tasks, invocations, idempotency, files, and audit;
- a demo approval verifier;
- a deterministic mock billing processor;
- an ephemeral signing key unless configured otherwise;
- immediate in-process execution.

A production service must replace these with durable tenant-isolated storage,
OAuth/OIDC or mTLS delegation, KMS/HSM-backed signing and rotation, real billing
and reconciliation, outbox/worker execution, immutable audit export, quarantined
object storage and malware scanning, callback SSRF controls, rate limiting,
abuse prevention, privacy controls, and independent security review.

See:

- [Core specification](spec/ASCP-0.2.md)
- [Billing profile](spec/billing-profile.md)
- [Security profile](spec/security-profile.md)
- [Deployment profile](docs/deployment.md)
- [Production checklist](docs/production-readiness-checklist.md)
- [Threat model](docs/threat-model.md)

## License

Apache License 2.0. See [LICENSE](LICENSE).
