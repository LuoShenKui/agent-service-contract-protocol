# ASCP 0.2 state machines

## 1. Direct invocation

Direct Flow intentionally has a small public state set:

| State | Meaning | Terminal |
|---|---|---:|
| `accepted` | Provider accepted bounded asynchronous work | No |
| `succeeded` | Requested result completed | Yes |
| `failed` | Definitive failure with no unresolved external outcome | Yes |

A provider MUST NOT return `failed` when an irreversible provider effect,
billing operation, or durable persistence outcome remains unknown. It should
retain `accepted` or expose a reconciliation problem until resolved.

## 2. Contract task states

| State | Meaning | Terminal |
|---|---|---:|
| `accepted` | Signed quote accepted and task durably created | No |
| `scheduled` | Waiting for `not_before`, capacity, or queue execution | No |
| `running` | Provider effect is being attempted | No |
| `waiting_input` | Requires bounded external input or reconciliation | No |
| `waiting_requote` | Terms changed and a new quote is required | No |
| `cancelling` | Cancellation requested but not complete | No |
| `cancelled` | Task stopped before an uncompensated effect | Yes |
| `succeeded` | Requested effect completed and receipt issued | Yes |
| `failed` | Definitive failure with resolved billing/effect state | Yes |
| `compensating` | Provider is attempting a compensating action | No |
| `compensated` | Compensation completed | Yes |
| `disputed` | Outcome requires a formal dispute process | Yes/profile-defined |

## 3. Valid transition principles

Typical transitions:

```text
accepted -> scheduled -> running -> succeeded
accepted -> running -> failed
accepted -> cancelled
scheduled -> cancelling -> cancelled
running -> waiting_input -> running
running -> waiting_requote
succeeded -> compensating -> compensated
succeeded -> disputed
```

Implementations may restrict transitions further. They MUST NOT invent a terminal
success or failure merely to simplify billing or retry logic.

## 4. Persistence and versioning

Every task transition increments `version` and updates `updated_at`. Optimistic
cancellation MAY require `expected_version`. State, audit intent, and outbox
publication SHOULD commit atomically.

## 5. Billing and provider uncertainty

Examples that require non-terminal reconciliation:

- billing reserve timed out after submission;
- provider effect succeeded but settlement response is unknown;
- task was created but queue publication is uncertain;
- provider returned no result after an irreversible request;
- billing release after failure could not be confirmed.

The service MUST preserve the original idempotency claim and stable task or
invocation ID while reconciliation runs.

## 6. Receipts

A terminal receipt is issued only after the service can state an internally
consistent outcome, billing record, artifacts, and audit root. A later refund,
compensation, or dispute is a new audited event and may produce an additional
profile-defined document; it does not rewrite the original signed receipt.
