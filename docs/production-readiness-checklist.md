# ASCP production-readiness checklist

## Protocol and discovery

- [ ] Manifest and capability catalog accurately describe supported flows,
      billing modes, files, limits, and optional features.
- [ ] Capability catalog is cacheable, paginated, bounded, and free of internal
      implementation details.
- [ ] Options is side-effect-free and not treated as authority.
- [ ] Direct planner fails closed when any term is incomplete.
- [ ] Unknown fields and unsupported critical extensions are rejected.

## Identity and authority

- [ ] OAuth/OIDC, DPoP, mTLS, or equivalent production delegation is deployed.
- [ ] Actor, Principal, tenant, scopes, audience, and expiry are bound.
- [ ] Object-level authorization covers offers, quotes, tasks, invocations,
      files, artifacts, billing, and audit.
- [ ] Independent approval is verified outside task text/model output.
- [ ] Approval binds exact digest, Principal, audience, and expiry.
- [ ] Scopes are rechecked immediately before execution.

## Signatures

- [ ] Signing keys are in KMS/HSM or an approved isolated service.
- [ ] JWKS publication, rotation, overlap, revocation, and incident response are
      tested.
- [ ] Clients verify JWS payload equality, digest, key, service ID, and expiry.
- [ ] Quote and receipts include every execution-changing field.

## Idempotency and durability

- [ ] Every unsafe mutation requires a tenant-scoped key.
- [ ] Same digest replays exact status, headers, and body.
- [ ] Different digest conflicts.
- [ ] Concurrent first requests cannot both execute.
- [ ] Unknown outcomes stay locked and expose reconciliation.
- [ ] Claims release only after proven no effect.
- [ ] Retention covers realistic client/provider/billing retry windows.
- [ ] Task state, audit intent, and outbox commit atomically.

## Billing

- [ ] Offered modes match actual commercial relationships.
- [ ] Per-call authorization is required only when signed terms require it.
- [ ] Standing arrangements are authenticated and tenant-bound.
- [ ] Audience, digest, payer, mode, arrangement, expiry, usage, currency, and
      ceiling are verified.
- [ ] Reserve/settle/release/refund or equivalent usage actions are idempotent.
- [ ] Unknown billing outcomes enter reconciliation.
- [ ] Cumulative refunds/credits cannot exceed policy.
- [ ] Processor callbacks are signed and replay-protected.
- [ ] Raw payment credentials never enter ASCP payloads or logs.

## Files and artifacts

- [ ] Upload tickets are owner-bound, single-purpose, short-lived, and redacted.
- [ ] Size, media type, digest, expiry, and state are verified.
- [ ] Content is quarantined until required scans complete.
- [ ] Archive and parser resource limits are enforced in isolation.
- [ ] Caller metadata cannot replace authoritative file metadata.
- [ ] All contract files are inside the quote signature.
- [ ] Artifact and download access is object-authorized and expires appropriately.
- [ ] File retention and deletion policies are implemented.

## Provider execution

- [ ] External operations use stable provider deduplication IDs.
- [ ] Queue workers are idempotent.
- [ ] Deadlines and cancellation boundaries are explicit.
- [ ] Irreversible effects are never repeated due to billing or response timeout.
- [ ] Compensation, refund, and dispute behavior is documented.

## Audit and privacy

- [ ] Material decisions and transitions are signed and hash-linked.
- [ ] Receipt audit roots verify independently.
- [ ] Audit storage is immutable or independently anchored.
- [ ] Logs and audit contain references/digests, not unnecessary content or
      secrets.
- [ ] Data-use, retention, training, sharing, region, deletion, and legal-hold
      policies match real behavior.

## Network and operations

- [ ] HTTPS, safe headers, body limits, and rate limiting are deployed.
- [ ] Authenticated redirects are disabled.
- [ ] Egress allowlists and callback SSRF controls are tested.
- [ ] Secrets are managed and rotated.
- [ ] Metrics cover idempotency, approval, billing, files, state, signatures,
      audit, abuse, and reconciliation age.
- [ ] Backups and point-in-time recovery are tested without losing idempotency.
- [ ] Signing compromise, billing outage, scanner outage, provider uncertainty,
      database failover, and audit recovery are rehearsed.

## Assurance

- [ ] `make check` and live conformance pass on the release artifact.
- [ ] SAST, dependency and image scanning pass.
- [ ] DAST and penetration testing are complete.
- [ ] Privacy, legal, and billing/payment reviews are complete where applicable.
- [ ] Independent security review has been completed before claiming production
      assurance.
