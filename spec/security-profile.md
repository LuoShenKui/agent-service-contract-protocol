# ASCP 0.2 security profile

## 1. Security objective

ASCP separates intent interpretation from authority, contract acceptance,
billing, file handling, execution, and audit. No single natural-language message
or model decision is sufficient to exercise consequential authority.

## 2. Authentication and delegation

Production services MUST use OAuth/OIDC, DPoP, mTLS, workload identity, or an
equivalent authenticated delegation mechanism.

The authenticated context MUST bind:

- Actor identity;
- Principal or tenant;
- allowed scopes;
- audience;
- expiry;
- optional device, workload, or policy constraints.

Bearer tokens SHOULD be short-lived. High-value deployments SHOULD use
sender-constrained tokens. Tokens MUST NOT be accepted from query strings.

## 3. Authorization

- Every resource lookup enforces tenant and object ownership.
- Scopes are checked at request acceptance and immediately before execution.
- Contract approval is verified through an independent authorization system.
- Approval binds the exact request or quote digest, Principal, audience, and
  expiry.
- Task text, retrieved content, web pages, email bodies, tool output, or model
  output MUST NOT create or expand authority.
- Standing policies must be versioned, revocable, and auditable.

## 4. Direct Flow controls

A provider MUST fail closed when Direct eligibility is uncertain.

Free read-only Direct calls may omit an idempotency key, but still require:

- authenticated delegation;
- scope and object checks;
- bounded output;
- data-minimization policy;
- signed receipt;
- audit event chain.

Side-effecting or billable Direct calls additionally require durable
idempotency, any declared approval, billing validation, and replayable responses.

## 5. Contract signatures

Quotes and receipts use provider-controlled signing keys. Production keys SHOULD
be held in KMS or HSM custody. Services MUST support rotation, overlap, revocation,
and incident response.

Clients MUST verify:

- JWS algorithm and key ID;
- cryptographic signature;
- embedded payload equality with the unsigned response projection;
- payload digest;
- service ID and trusted manifest binding;
- issue and expiry times;
- Actor and Principal where applicable.

Unsigned display text is not a substitute for signed machine terms.

## 6. Idempotency

Idempotency records are security-sensitive. They MUST be tenant-scoped, durable,
and protected against enumeration and unauthorized replay.

The record binds:

- Actor and Principal;
- route and method;
- key;
- exact request digest;
- lifecycle state;
- stable task or invocation ID;
- stored response;
- retention expiry.

Unknown outcomes remain locked. Timeouts and connection resets do not prove no
effect. A service MUST NOT release an idempotency claim unless it can prove that
no quote, upload ticket, billing entry, task, callback, or provider effect was
created.

## 7. Billing security

- ASCP carries only opaque tokenized or standing arrangement references.
- Raw payment credentials, reusable wallet secrets, bank credentials, and
  private keys are forbidden in protocol payloads, logs, audit, and prompts.
- Billing authorization binds mode, payer, arrangement, service audience,
  request/quote digest, expiry, usage, and ceiling.
- Processor callbacks require signature and replay verification.
- Reservations, settlement, release, refunds, quota recording, invoices, and
  clearing entries are independently idempotent.
- Unknown billing outcomes enter reconciliation and never trigger duplicate
  provider effects.
- Billing data is tenant-isolated and access-controlled as financial data.

## 8. File and attachment security

Production file handling MUST use quarantined object storage and isolated
workers. The service validates:

- authenticated owner;
- upload target and single-purpose token;
- token expiry;
- declared and actual size;
- media type;
- SHA-256 digest;
- capability allowlist;
- readiness and scan status;
- archive expansion and parser limits.

Upload secrets must be redacted. Filenames are untrusted display metadata and
must not create filesystem paths. Content-Disposition values require safe
encoding. Archives, PDFs, media, office documents, and executables should be
parsed in sandboxed, resource-limited processes.

A `FileRef` is not authority to access another tenant's bytes. The provider's
stored metadata is authoritative.

## 9. Context and artifact references

References MUST be opaque or unguessable and bound to tenant, purpose, expiry,
and access policy. Providers SHOULD use short-lived, least-privilege retrieval
tokens. Automatic redirects to another origin are forbidden for authenticated
client libraries unless explicitly re-authorized.

Large source data should remain at its source. References and digests should
replace raw sensitive content in logs and audit.

## 10. Callback security

If callbacks are supported:

- ownership must be established before quote issuance;
- only HTTPS endpoints are allowed on public deployments;
- private, loopback, link-local, metadata, and disallowed network ranges are
  blocked unless a reviewed private profile permits them;
- DNS rebinding is mitigated;
- egress uses allowlists or a controlled relay;
- events are signed and replay-protected;
- callback credentials are referenced through a secret manager, not embedded;
- response bodies and redirects are bounded and not trusted.

## 11. Input validation

Services MUST enforce:

- strict JSON decoding;
- maximum body, array, string, and nesting sizes;
- exact decimal money parsing;
- URI and media-type validation;
- identifier length and character limits;
- deadline and expiry bounds;
- callback and file policy;
- field-level task validation;
- maximum inline result size.

Unknown critical fields or extensions are rejected.

## 12. Prompt-injection and content isolation

Provider-internal models may process untrusted task data, email, documents,
reviews, pages, or videos. The implementation must separate:

- system policy;
- authenticated identity and authority;
- signed contract terms;
- tool output;
- untrusted content.

Untrusted content cannot alter scopes, billing ceilings, destination, callback,
or approval requirements. Deterministic policy and code enforce those boundaries.

## 13. Tenant isolation

Every offer, quote, task, invocation, file, artifact, idempotency record, billing
record, and audit event is tenant-scoped. Cross-tenant identifiers should return
not found rather than reveal existence. Caches, object stores, queues, metrics,
and traces must preserve isolation.

## 14. Data protection and privacy

- Encrypt data in transit and at rest.
- Minimize content retention.
- Separate operational logs from sensitive payload storage.
- Redact tokens, upload credentials, authorization references, and billing
  references.
- Enforce quote-declared data-use policy.
- Define deletion, legal hold, export, and incident procedures.
- Do not use task or attachment content for training unless the signed/declared
  policy and applicable consent permit it.

## 15. Audit security

Audit events are append-only, hash-linked, signed, and exported through
object-level authorization. Audit storage SHOULD be immutable or independently
anchored. Events include decisions and references, not unnecessary raw content.
Clock synchronization and sequence monotonicity are monitored.

## 16. Denial of service and abuse

Rate limits should consider:

- Actor, Principal, tenant, IP, and service;
- capability and risk class;
- upload bytes and scan cost;
- Options and capability enumeration;
- quote and idempotency store growth;
- billing authorization failures;
- callback and artifact bandwidth;
- model or provider execution cost.

Providers should use budgets, concurrency limits, circuit breakers, queue limits,
and abuse detection.

## 17. Supply chain and deployment

- Pin and scan dependencies and container images.
- Use minimal non-root images and read-only filesystems where practical.
- Store secrets in a secret manager.
- Separate signing and billing credentials from application configuration.
- Restrict egress.
- Require code review and protected release workflows.
- Produce SBOMs and signed release artifacts where practical.

## 18. Incident response

Operators must rehearse:

- signing-key compromise;
- access-token theft;
- idempotency corruption;
- billing outage or duplicate callback;
- file scanner compromise;
- object-store exposure;
- provider API uncertainty;
- audit export loss;
- cross-tenant access attempt.

The service must be able to revoke keys and arrangements, stop risky
capabilities, retain original idempotency claims, and reconcile affected tasks.
