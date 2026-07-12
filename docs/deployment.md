# Production deployment profile

The reference binary is not a production deployment. This profile describes the
minimum architecture expected for an internet-facing ASCP service.

## 1. Edge

Use a hardened API gateway or ingress with:

- TLS 1.2+ and modern cipher policy;
- request body and header limits;
- OAuth/OIDC, DPoP, mTLS, or workload identity;
- tenant-aware rate limits;
- WAF and abuse controls;
- no automatic authenticated redirects;
- request correlation and privacy-safe access logs.

## 2. Durable database

Persist at least:

- offers and signed quotes;
- direct invocations;
- contract tasks and versions;
- scoped idempotency claims and exact replay bodies;
- billing reservations/records and reconciliation state;
- file metadata and scan state;
- audit events and roots;
- outbox/inbox events;
- approval and external-operation references.

Use row-level tenant isolation or an equivalent hard boundary. Encrypt sensitive
columns. Apply uniqueness constraints for idempotency and provider deduplication.

## 3. Transaction and outbox

Task state, audit intent, and outbox publication should commit atomically.
Workers consume outbox records idempotently. Provider API calls use stable task
or operation IDs as deduplication keys.

External billing reserve may precede the local transaction. A reconciler must
identify and resolve:

- reservation without task;
- task without queue publication;
- provider effect with unknown response;
- settlement without receipt;
- receipt without immutable audit export;
- release/refund with unknown outcome.

## 4. Idempotency backend

The backend must be strongly consistent enough to prevent concurrent acceptance
of the same scoped key. It stores request digest, state, stable resource ID,
response bytes, headers, and expiry. Unknown outcomes stay locked.

Do not use an evictable cache as the only source of truth for consequential
operations.

## 5. Billing

Replace the mock with approved adapters for the offered modes. Each adapter must:

- verify audience, digest, payer, arrangement, expiry, and ceiling;
- implement idempotent reserve/settle/release/refund or equivalent usage logic;
- distinguish definitive decline, proven no-effect temporary failure, and
  unknown outcome;
- verify signed processor callbacks;
- expose reconciliation data;
- never log raw credentials.

## 6. Files

Use private object storage with quarantine prefixes or buckets. Upload tickets
should be single-purpose and short-lived. A scanner pipeline verifies digest,
media type, archive behavior, malware, parser safety, and policy. Only clean,
ready objects receive usable FileRefs.

Downloads require object-level authorization. URLs should be short-lived and
same-origin where possible. Retention and deletion follow task and privacy
policy.

## 7. Signing

Use KMS/HSM-backed Ed25519 or an approved profile. Publish JWKS, rotate keys with
overlap, retain verification keys for receipt lifetime, and have compromise
revocation procedures. Signing access is isolated from ordinary application
credentials.

## 8. Audit

Store signed events in append-only storage and periodically anchor roots to an
independent system. Restrict exports by tenant and resource. Monitor sequence
gaps, signature failures, clock skew, and root mismatches.

## 9. Workers and timeouts

Use bounded queues and worker pools. Distinguish:

- HTTP request timeout;
- provider execution deadline;
- billing timeout;
- callback delivery timeout;
- reconciliation deadline.

A timeout does not imply failure. Persist stable IDs before calling external
systems.

## 10. Network and callback controls

Use egress allowlists for billing, approval, provider, object-store, and callback
relays. Block metadata and private-network SSRF. Resolve DNS safely and re-check
destination policy at connection time. Do not follow redirects with bearer
credentials.

## 11. Secrets

Load OAuth, signing, billing, storage, and callback credentials from a secret
manager. Rotate them. Do not place secrets in environment examples, repository
files, images, prompts, task metadata, audit, or client-visible errors.

## 12. Observability

Metrics should cover:

- capability and Direct/Contract route rates;
- Direct eligibility and contract-required decisions;
- idempotency replay, conflict, in-progress, and unknown outcomes;
- quote verification and approval failures;
- billing state by mode and reconciliation age;
- task transitions and provider latency;
- upload bytes, scan outcomes, and file rejection;
- signature and audit verification failures;
- callback delivery and SSRF rejection;
- tenant-level budgets and abuse signals.

Logs should use IDs and digests, not raw content.

## 13. Backup and recovery

Back up durable state, signing metadata, audit, and file metadata. Test point-in-
time recovery. Ensure restored systems do not repeat provider effects or lose
idempotency claims. Reconciliation must run before normal processing resumes.

## 14. Release gate

Before production:

- run unit, race, integration, schema, OpenAPI, and live conformance tests;
- perform SAST, dependency/image scanning, DAST, and penetration testing;
- review privacy and data retention;
- complete payment/billing certification where applicable;
- exercise failure injection at every external boundary;
- obtain independent security review.
