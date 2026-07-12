# ASCP 0.2 error catalog

Errors use `application/problem+json`. `retryable` is authoritative only together
with idempotency and reconciliation instructions.

## Core request and discovery

| Code | HTTP | Retryable | Meaning |
|---|---:|---:|---|
| `invalid_request` | 400 | No | Malformed request, header, or idempotency key |
| `validation_failed` | 422 | No | Structurally valid JSON violates task or protocol rules |
| `unsupported_version` | 400 | No | Requested wire version is unsupported |
| `unsupported_media_type` | 415 | No | Body media type is unsupported |
| `unsupported_intent` | 422 | No | Provider does not support the requested task |
| `contract_required` | 409 | No | Direct Flow is not allowed; use Contract Flow |
| `not_found` | 404 | No | Resource does not exist or is hidden by authorization |
| `service_unavailable` | 503 | Yes | Temporary service dependency failure with safe retry semantics |

## Authentication and authority

| Code | HTTP | Retryable | Meaning |
|---|---:|---:|---|
| `unauthenticated` | 401 | No | Missing, invalid, or expired access token |
| `forbidden` | 403 | No | Delegation or object access is insufficient |
| `authorization_required` | 403 | No | Independent approval is required |
| `authorization_invalid` | 403 | No | Approval reference, audience, digest, principal, or expiry failed |

## Quote and execution

| Code | HTTP | Retryable | Meaning |
|---|---:|---:|---|
| `quote_expired` | 409 | No | Prepare a new quote and obtain new approval |
| `quote_mismatch` | 409 | No | Commit digest does not match stored signed quote |
| `deadline_expired` | 409 | No | Execution deadline has passed |
| `callback_unsupported` | 422 | No | Provider does not support requested callback behavior |
| `task_not_cancellable` | 409 | No | Task passed its cancellation boundary |
| `precondition_failed` | 412 | No | Task version or another explicit precondition failed |

## Idempotency and outcome

| Code | HTTP | Retryable | Meaning |
|---|---:|---:|---|
| `idempotency_conflict` | 409 | No | Same key was reused with a different request digest |
| `request_in_progress` | 409 | Yes | Original identical request remains active |
| `outcome_unknown` | 500/503 | No/new call | Reconcile the original stable resource; do not create a new logical request |

## Billing

| Code | HTTP | Retryable | Meaning |
|---|---:|---:|---|
| `billing_required` | 402/428 | No | Required arrangement or authorization is missing |
| `billing_mode_unsupported` | 422 | No | Selected billing mode is unsupported for this task |
| `billing_declined` | 402 | No | Definitive funding, credit, arrangement, or ceiling decline |
| `billing_unavailable` | 503 | Yes only if no effect | Billing system failed and positively guarantees no record was created |
| `billing_outcome_unknown` | 503 | No/new call | Billing may have succeeded; reconcile original operation |

## Files

| Code | HTTP | Retryable | Meaning |
|---|---:|---:|---|
| `file_too_large` | 413 | No | Declared or actual bytes exceed policy |
| `upload_expired` | 410 | No | Prepare a new upload ticket |
| `digest_mismatch` | 422 | No | Uploaded bytes do not match declared SHA-256 |
| `file_not_ready` | 409/410 | Sometimes | Upload, scan, or retention state is not usable |
| `file_rejected` | 409/422 | No | Metadata or policy validation failed |

## Problem fields

A problem contains:

- RFC 9457-style `type`, `title`, `status`, and optional `detail`;
- stable ASCP `code` and `category`;
- `retryable` and optional `retry_after_ms`;
- request ID;
- optional task or invocation ID/state;
- optional reconciliation reference;
- JSON Pointer field errors;
- bounded extension data.

## Safe retry rule

A service may mark a mutation retryable and release an idempotency claim only
when it can prove no quote, task, billing record, upload ticket, callback,
provider effect, or other externally visible state was created. Otherwise the
client retries only the identical request with the same key or follows the
returned reconciliation resource.
