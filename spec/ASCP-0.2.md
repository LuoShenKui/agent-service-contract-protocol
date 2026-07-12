# Agent Service Contract Protocol 0.2

- Short name: **ASCP 0.2**
- Status: **Working Draft**
- Provisional media type: `application/ascp+json`
- Reference transport: HTTPS + JSON

## 1. Purpose

ASCP defines a compact service boundary between an external client Agent and a
platform-owned service Agent. It supports two execution paths:

1. **Direct Flow** for complete requests whose authority, risk, price, billing,
   and retry behavior are already sufficient for execution.
2. **Contract Flow** for requests that require a binding quote, separate
   approval, variable terms, payment authorization, or stronger execution
   controls.

ASCP deliberately does not expose every provider-internal API, database field,
or tool schema. The provider Agent is responsible for interpreting its platform
semantics and returning the minimum public task contract.

## 2. Conformance language

The key words **MUST**, **MUST NOT**, **REQUIRED**, **SHOULD**, **SHOULD NOT**, and
**MAY** are normative.

An implementation claiming ASCP 0.2 conformance MUST satisfy:

- the applicable requirements in this document;
- [protocol invariants](invariants.md);
- [security profile](security-profile.md);
- [error catalog](error-catalog.md);
- [task state machine](state-machine.md);
- [billing profile](billing-profile.md) when a non-free mode is used.

A deployment MUST accurately declare unsupported optional features.

## 3. Non-goals

ASCP 0.2 does not define:

- model prompts, chain-of-thought, or internal planning;
- provider-internal API or SQL design;
- general Agent messaging, social identity, or group coordination;
- raw payment credential transport;
- universal industry object models;
- a globally complete world-state database;
- arbitrary browser or operating-system automation.

A provider MAY use ordinary APIs, SQL, MCP, A2A, queues, rules, models, or human
operations internally. ASCP constrains only the external service boundary.

## 4. Actors and trust boundaries

### 4.1 Client Agent

The external caller that discovers capabilities, submits tasks, verifies signed
objects, obtains independent approval, and handles retries.

### 4.2 Platform-owned Agent

The service provider that understands its own fields, permissions, business
rules, provenance, execution system, pricing, and billing relationships.

### 4.3 Actor and Principal

- **Actor:** the Agent or service account making the call.
- **Principal:** the user or organization whose authority, data, or funds are
  being used.

Authentication MUST bind both values. Contract Flow repeats them inside the
negotiation and signed quote to prevent confused-deputy substitution.

### 4.4 Authorization verifier

An independent system that validates human approval, enterprise policy, or a
standing mandate. Task text, model output, retrieved documents, and provider
responses MUST NOT create authority by themselves.

### 4.5 Billing processor

A provider-neutral adapter for immediate payment, balance usage, subscriptions,
postpaid credit, invoices, clearing, sponsorship, or external settlement.

### 4.6 File service

A storage and validation boundary that accepts bytes separately from protocol
JSON and returns owner-bound, digest-bound `FileRef` objects.

## 5. Discovery

### 5.1 Manifest

A provider MUST expose:

```http
GET /.well-known/ascp
```

The manifest includes:

- protocol name and supported versions;
- stable service ID and service name;
- base URL and JWKS URI;
- capability, Options, Direct, and file endpoints;
- authentication schemes;
- supported billing modes;
- service-wide features and limits;
- generation and expiry times.

The manifest SHOULD be cacheable. Clients MUST bind verified quotes and receipts
to the discovered `service_id`.

### 5.2 Verification keys

A provider MUST expose public verification keys at its manifest `jwks_uri`.
ASCP 0.2 reference profile uses Ed25519 JWS (`EdDSA`). Production services MUST
support key rotation, overlap, revocation, and stable `kid` values.

### 5.3 Compact capability catalog

A provider MUST expose:

```http
GET /.well-known/ascp/capabilities
```

Each `CapabilityDescriptor` contains routing information only:

- `intent` and capability `version`;
- summary and optional description;
- `default_flow` and `supported_flows`;
- side-effect and risk class;
- required scopes;
- billing modes;
- parameter names, without complete schemas;
- input file policy;
- output modes;
- whether semantic Options is supported.

The catalog MUST NOT be treated as authority to execute. It MUST be cacheable
using revision, expiry, and preferably ETag. Providers SHOULD support query,
intent filtering, pagination, and reasonable result limits.

Complete task parameter schemas SHOULD be returned only after the client selects
a task through Options or Negotiate.

## 6. Common transport rules

### 6.1 HTTPS

Internet-facing deployments MUST use HTTPS. Loopback HTTP MAY be used for local
development.

### 6.2 Version header

Requests MAY include:

```http
ASCP-Version: 0.2
```

A service MUST reject an explicitly unsupported version rather than silently
interpreting it as another wire contract.

### 6.3 Media types

JSON requests SHOULD use:

```http
Content-Type: application/ascp+json
```

Services MAY also accept `application/json`. Problems use
`application/problem+json`.

### 6.4 Strict decoding

Protocol objects MUST reject unknown top-level fields unless an extension point
explicitly permits them. Providers MUST enforce body, array, string, recursion,
and file limits before expensive processing.

### 6.5 Authentication and delegation

Every non-public endpoint MUST authenticate the Actor and bind the Principal.
Authorization scopes MUST be checked again immediately before execution.
Authentication, approval, and billing are distinct controls.

### 6.6 Correlation IDs

`X-Request-ID` is only a diagnostic correlation value. It MUST NOT grant
identity, authority, idempotency, or billing rights.

## 7. Direct Flow

### 7.1 Purpose

Direct Flow collapses a complete low-risk service request into one request and
one response:

```http
POST /v1/invoke
```

The caller sends `DirectInvocationRequest`; the service returns
`DirectInvocationResponse` and a signed `InvocationReceipt`.

### 7.2 Eligibility

A provider MUST execute Direct Flow only when all required conditions are known
before the provider effect begins. These include, as applicable:

- resolved intent;
- valid parameters;
- current delegated scopes;
- accepted context and files;
- fixed or bounded price;
- billing relationship;
- independent authorization;
- side-effect class;
- retry and idempotency policy;
- execution constraints.

If any binding term requires separate review or quote acceptance, the service
MUST reject the request with `contract_required`. It MUST NOT silently downgrade
controls or execute a partial equivalent.

### 7.3 Free read-only Direct call

A provider-declared free, read-only operation MAY omit:

- Options;
- `Idempotency-Key`;
- authorization evidence beyond the authenticated delegation;
- billing authorization.

The service still MUST enforce authentication, scopes, data minimization, output
limits, audit, and receipt signing.

### 7.4 Side-effecting or billable Direct call

A provider MAY support a side-effecting Direct capability only when its standing
rules fully determine the operation. It MUST require:

- valid `Idempotency-Key`;
- every declared independent authorization;
- every required billing authorization or standing arrangement;
- signed completion receipt;
- durable replay state.

### 7.5 Direct request binding

The service computes a digest of the exact accepted request representation. Any
Direct authorization or billing authorization MUST bind to that digest and the
service audience.

### 7.6 Direct result

Small results MAY be placed in `result`. Large, sensitive, or independently
retrievable results SHOULD use `ArtifactRef`. A terminal Direct response MUST
contain a signed `InvocationReceipt` that binds:

- protocol version and service ID;
- invocation ID and intent;
- request digest;
- outcome;
- artifacts;
- billing record, if any;
- audit root;
- completion time.

## 8. Optional Options preflight

### 8.1 Semantic preflight

A provider MAY expose:

```http
POST /v1/options
```

The endpoint is optional for the client and side-effect-free for the provider.
It returns only task-relevant details, including:

- resolved intent;
- Direct eligibility and recommended flow;
- required or missing parameters;
- scopes;
- billing choices;
- file policy;
- estimate;
- schema version and expiry.

Options MUST NOT create authority, a quote, a task, billable usage, a billing
reservation, or a provider business effect. It does not require an idempotency
key.

### 8.2 HTTP OPTIONS

`OPTIONS /v1/invoke` MAY publish transport hints such as `Allow`, `Accept-Post`,
capability URI, and semantic Options URI. It is not the semantic task preflight.

## 9. Contract Flow overview

Contract Flow has three stages:

1. **Negotiate:** determine support and return only the minimum required task
   contract shape.
2. **Prepare:** validate exact task terms and issue a signed, side-effect-free
   quote.
3. **Commit:** validate quote, approval, scopes, billing, and idempotency; create
   exactly one durable task and execute or schedule it.

A provider MAY optimize internal implementation, but wire-visible safety
properties MUST remain equivalent.

## 10. Negotiate

### 10.1 Endpoint

```http
POST /v1/negotiate
```

`Idempotency-Key` is REQUIRED.

### 10.2 Request

`NegotiationRequest` includes:

- intent and/or goal;
- explicit Actor and Principal;
- constraints and optional budget;
- context references and input file references;
- locale and bounded metadata.

The service MUST verify the authenticated delegation matches the supplied Actor
and Principal.

### 10.3 Response

When supported, the response returns:

- offer ID and expiry;
- resolved intent and schema version;
- minimum parameter definitions;
- required scopes;
- accepted billing options;
- file policy;
- non-binding price estimate.

Negotiation MUST NOT execute the business task.

## 11. Prepare

### 11.1 Endpoint

```http
POST /v1/prepare
```

`Idempotency-Key` is REQUIRED.

### 11.2 Side-effect-free requirement

Prepare MUST validate and price the task without performing the requested
business effect. Preparing an email MUST NOT send it. Preparing a booking MUST
NOT book it. Preparing a refund MUST NOT move funds.

A provider MAY perform bounded internal reads, policy evaluation, availability
checks, pricing, and file validation needed to produce the quote.

### 11.3 Signed quote

The `Quote` is immutable and signs at least:

- protocol version and service ID;
- quote and offer IDs;
- intent;
- Actor and Principal;
- normalized task;
- context and input files;
- callback configuration;
- exact price, ceiling, and breakdown;
- billing terms;
- external effects and reversibility;
- permission use;
- data use and retention;
- risk class;
- confirmation requirement;
- service level and execution constraints;
- issue, expiry, and optional revocation times.

A field that can alter execution, cost, permissions, destination, data access,
or result MUST be inside the signature projection.

### 11.4 Signature projection

The signature member itself is removed from the top-level object before signing.
The JWS embedded payload is authoritative. The response also carries its payload
SHA-256 digest. Clients MUST verify the JWS, compare the decoded payload with the
received unsigned object, validate `kid`, and bind `service_id` to the manifest.

## 12. Commit

### 12.1 Endpoint

```http
POST /v1/commit
```

`Idempotency-Key` is REQUIRED.

### 12.2 Required checks

Before execution, the provider MUST validate:

- quote exists, is unexpired, unrevoked, and owned by the delegation;
- supplied quote digest matches the stored signed quote;
- independent approval is authentic, current, correctly scoped, and bound to
  the quote digest;
- delegated scopes are still active;
- input files remain valid and usable;
- execution deadline remains valid;
- billing requirements are satisfied;
- the idempotency claim permits exactly one outcome.

### 12.3 Authorization evidence

`AuthorizationEvidence` binds:

- evidence type and opaque reference;
- Principal ID;
- service audience;
- approval and expiry times;
- exact quote digest;
- optional policy version.

The provider MUST verify the reference against an authorization system separate
from natural-language task content.

### 12.4 Billing authorization

`BillingAuthorization` is included only when the signed `BillingTerms` require
it. Standing arrangements MAY omit per-call authorization. See
[billing profile](billing-profile.md).

### 12.5 Durable task creation

Commit MUST create at most one durable `Task` for a successful logical request.
The provider MUST have a transaction/outbox or equivalent recovery design for
partial failures between billing, task persistence, queue publication, and
external effects.

## 13. Idempotency

### 13.1 Scope

An idempotency key is scoped by at least:

- authenticated Actor;
- Principal or tenant;
- HTTP method;
- route;
- provider service identity.

### 13.2 Replay states

For the same scope and key:

- same request digest: return the stored response;
- different digest: return `idempotency_conflict`;
- original still running: return `request_in_progress`;
- original outcome uncertain: retain the claim and return reconciliation data.

### 13.3 Safe release

A service MAY release a claim only when it can prove that no quote, billing
record, file ticket, task, callback, provider effect, or externally visible
state was created. Timeouts are not proof of no effect.

### 13.4 Retention

Mutation responses MUST advertise or document idempotency retention. Retention
must be long enough for realistic network, queue, provider, and billing retries.

## 14. Billing

### 14.1 Modes

ASCP defines:

- `free`;
- `pay_now`;
- `prepaid_balance`;
- `subscription`;
- `postpaid_account`;
- `monthly_invoice`;
- `clearing_account`;
- `sponsored`;
- `external_settlement`.

Immediate payment is one mode, not the default definition of all service value.

### 14.2 Signed terms

A quote or Direct plan signs or binds:

- selected mode;
- standing arrangement reference, when used;
- whether an arrangement or per-call authorization is required;
- settlement timing;
- accepted authorization schemes;
- capture or usage-recording behavior;
- refund policy;
- variable-price policy;
- billing period and usage unit.

### 14.3 Billing record

The result MAY include reservation, settlement, invoice, or period references.
The state name must describe the selected mode rather than pretending every
arrangement captured a card payment.

Detailed rules are in [billing-profile.md](billing-profile.md).

## 15. Files and attachments

### 15.1 Separation from JSON

Large file bytes MUST NOT be base64-embedded in ordinary ASCP JSON by default.
The provider exposes a scoped upload process and returns `FileRef`.

### 15.2 Prepare upload

```http
POST /v1/files/prepare-upload
```

The caller declares:

- safe filename;
- media type;
- exact size;
- SHA-256 digest;
- purpose;
- optional earlier expiry.

The response contains a short-lived, owner-bound upload target and secret token.
The secret MUST NOT be logged in general application logs.

### 15.3 Upload bytes

```http
PUT /v1/files/{file_id}/content
```

The service MUST verify:

- authenticated owner;
- upload token and expiry;
- allowed media type;
- exact byte length;
- exact digest;
- upload target state.

An identical retry MAY replay safely. Different bytes under the same file ID
MUST be rejected.

### 15.4 Readiness and scanning

A provider MUST NOT use a file until it is ready and satisfies the capability's
scan policy. Production systems SHOULD quarantine uploaded content and perform
malware, archive, parser, and policy checks in isolated workers.

### 15.5 Contract binding

Every input `FileRef` that can affect a Contract task MUST be inside the signed
quote. The provider MUST compare caller-supplied metadata with its authoritative
file record before use.

### 15.6 Artifacts

Results SHOULD be returned as `ArtifactRef` when they are large, sensitive, or
independently retrievable. Artifact authorization and expiry MAY differ from the
receipt lifetime.

## 16. Task lifecycle

A Contract task uses the states defined in [state-machine.md](state-machine.md).
Terminal states include succeeded, failed, cancelled, compensated, and disputed
where applicable. Unknown billing or provider outcomes MUST NOT be mislabeled as
a clean terminal failure.

## 17. Cancellation, compensation, refund, and dispute

Cancellation requires its own idempotency key. Providers MUST declare when a
task is no longer cancellable.

A completed irreversible effect cannot be erased by changing task state. A
provider MAY use compensation, refund, account credit, or dispute handling as a
new audited action. Refunds MUST reference a prior settlement or account record
and MUST NOT exceed the amount eligible for reversal unless an explicit policy
allows a separate goodwill credit.

## 18. Receipts

### 18.1 Contract receipt

A terminal Contract task SHOULD contain a signed `Receipt` binding:

- protocol and service;
- receipt, task, and quote IDs;
- final outcome;
- artifacts;
- billing record, amount, and breakdown;
- audit root;
- completion time.

### 18.2 Direct receipt

A terminal Direct invocation MUST contain a signed `InvocationReceipt` as
specified in Section 7.

### 18.3 Verification

Clients SHOULD verify receipts independently before treating them as evidence of
execution or billing.

## 19. Audit

### 19.1 Event chains

Material decisions and transitions SHOULD be appended to a resource-local audit
chain. Direct invocations and Contract tasks use the same event structure.

Each `AuditEvent` binds:

- stable event ID;
- resource type and ID;
- strictly increasing sequence;
- event type and occurrence time;
- Actor;
- data digest;
- previous event hash;
- current event hash;
- provider signature.

### 19.2 Receipt anchor

The final receipt's `audit_root` MUST match the root immediately before receipt
issuance, according to the documented audit sequence. An export MUST be
independently verifiable without trusting the provider's database query result.

### 19.3 Privacy

Audit events SHOULD contain references, classifications, hashes, and decisions
rather than raw secrets, message bodies, payment credentials, or attachment
bytes. Retention and access controls remain mandatory.

## 20. Errors

Errors use `application/problem+json` with stable machine-readable `code`, HTTP
status, retryability, request ID, and optional task/invocation/reconciliation or
field details.

A client MUST base retry behavior on the error code, retryability, idempotency
state, and reconciliation instructions—not solely on HTTP status.

The normative catalog is [error-catalog.md](error-catalog.md).

## 21. Data minimization and references

Context, documents, mailbox threads, videos, and artifacts SHOULD remain at the
source when the service can securely resolve a scoped reference. References MUST
be bound to identity, tenant, purpose, expiry, and access policy.

A missing response, empty list, unsupported region, stale source, or failed
upstream query MUST NOT automatically be interpreted as a negative fact such as
“safe,” “open,” or “no alert.” Domain services SHOULD represent coverage,
freshness, confidence, unknown, stale, conflict, and unsupported states.

## 22. Extensions and domain profiles

Extensions MAY add industry-specific fields and states but MUST NOT weaken:

- authentication and delegation;
- signature binding;
- approval independence;
- billing ceilings;
- idempotency and unknown-outcome handling;
- file integrity;
- audit verifiability;
- data minimization.

Extensions SHOULD be namespaced and versioned. Unrecognized critical extensions
MUST cause rejection.

## 23. Security requirements

At minimum, production deployments MUST provide:

- OAuth/OIDC, DPoP, mTLS, or equivalent delegation;
- tenant isolation and object-level authorization;
- strict input validation and rate limiting;
- KMS/HSM-backed signing and rotation;
- durable idempotency and transaction/outbox recovery;
- safe billing reconciliation;
- quarantined object storage and isolated file parsing;
- callback SSRF and credential-exfiltration controls;
- immutable audit retention;
- secrets management and egress allowlists;
- monitoring, abuse detection, and incident response.

See [security-profile.md](security-profile.md).

## 24. Privacy requirements

Providers MUST document:

- data categories and purposes;
- retention;
- training use;
- third-party sharing;
- processing region where material;
- artifact and audit access;
- deletion and legal-hold behavior.

Data-use statements in quotes and Direct plans MUST reflect actual provider
behavior.

## 25. Reference endpoints

```text
GET     /.well-known/ascp
GET     /.well-known/ascp/capabilities
GET     /.well-known/jwks.json
OPTIONS /v1/invoke
POST    /v1/options
POST    /v1/invoke
POST    /v1/negotiate
POST    /v1/prepare
POST    /v1/commit
POST    /v1/files/prepare-upload
PUT     /v1/files/{file_id}/content
GET     /v1/files/{file_id}
GET     /v1/files/{file_id}/content
GET     /v1/invocations/{invocation_id}
GET     /v1/invocations/{invocation_id}/audit
GET     /v1/tasks/{task_id}
POST    /v1/tasks/{task_id}/cancel
GET     /v1/tasks/{task_id}/events
GET     /v1/tasks/{task_id}/audit
```

The authoritative object definitions are in
[`schemas/ascp-v0.2.schema.json`](../schemas/ascp-v0.2.schema.json), and the HTTP
binding is in [`openapi/ascp-v0.2.yaml`](../openapi/ascp-v0.2.yaml).

## 26. Conformance profiles

### 26.1 Direct-only service

A Direct-only service MUST implement discovery, Direct invocation, receipt
verification keys, appropriate idempotency, and audit.

### 26.2 Contract service

A Contract service MUST implement Negotiate, Prepare, Commit, signed quote,
independent approval, durable task state, idempotency, receipt, and audit.

### 26.3 File-capable service

A file-capable service MUST additionally enforce scoped upload, digest, size,
owner, expiry, readiness, scan policy, and signed file binding.

### 26.4 Billed service

A billed service MUST implement the selected billing modes, idempotent adapter
operations, ceilings, audience and digest binding, reconciliation, and accurate
receipt states.

### 26.5 Reference implementation limitation

The Go reference service uses in-memory stores, a demo token, demo approval,
mock billing, and local file memory. Those defaults are intentionally not
production-conformant until replaced according to the deployment profile.
