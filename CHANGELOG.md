# Changelog

All notable project changes are documented here.

## [0.2.0-draft.1] — 2026-07-12

### Added

- **Direct Flow:** complete low-risk requests can execute through one
  `POST /v1/invoke` request and return a signed invocation receipt.
- **Optional Options:** task-specific parameter, flow, permission, file, and
  billing preflight without creating authority, quote, charge, or task.
- **Compact capability catalog:** cacheable task-level discovery that avoids
  loading every provider-internal tool schema.
- **Multiple billing arrangements:** free, pay-now, prepaid, subscription,
  postpaid, monthly invoice, clearing, sponsored, and external settlement.
- **Scoped files and attachments:** digest-bound upload tickets, owner-bound
  FileRefs, out-of-band bytes, and signed file metadata in quotes.
- Direct invocation resources, signed receipts, and independent audit export.
- Reference `email.latest.read` Direct capability and `email.send` Contract
  capability with attachments.
- Conformance coverage for Direct, Options, files, standing billing, signatures,
  idempotency, and audit.

### Changed

- Protocol version advanced from 0.1 to 0.2.
- Payment-specific structures became provider-neutral billing structures.
- Per-call payment authorization is no longer required for standing arrangements.
- Audit events now identify a generic resource type and ID, supporting both
  Contract tasks and Direct invocations.
- OpenAPI, JSON Schema, examples, database blueprint, docs, and Go reference
  implementation updated to the two-flow design.

### Reliability and security

- Direct work fails closed with `contract_required` when terms are incomplete.
- Idempotency is optional only for provider-declared read-only Direct calls and
  mandatory for unsafe retry paths.
- Billing uncertainty retains the original operation for reconciliation.
- File use validates identity, token, type, size, digest, expiry, readiness, and
  scan state.
- Signed quotes bind input files, billing terms, permissions, data use, effects,
  callbacks, and price ceiling.

## [0.1.0-draft.1]

- Initial Negotiate / Prepare / Commit working draft.
- Signed quotes, task receipts, append-only audit, scoped idempotency, approval,
  tokenized pay-now adapter, Go client/server, OpenAPI, JSON Schema, Docker, CI,
  and production guidance.
