# ASCP 0.2 billing profile

## 1. Scope

This profile defines how ASCP represents service value, funding, accounting, and
settlement without assuming every task performs an immediate payment.

ASCP carries opaque references and signed commercial terms. It does not carry
raw card numbers, bank credentials, private wallet keys, reusable payment
secrets, or an implementation-specific ledger schema.

## 2. Billing modes

### `free`

No service charge or billable usage. No reservation or per-call billing
authorization is created. A provider may still return a `BillingRecord` with
state `not_billable` for audit clarity.

### `pay_now`

A tokenized per-call authorization is normally required. The provider reserves
up to the signed ceiling before execution and settles after the documented safe
point.

### `prepaid_balance`

The caller selects an existing balance with `arrangement_ref`. The provider
reserves or checks sufficient balance, then debits after success.

### `subscription`

The caller selects an existing plan or allowance. The provider may reserve quota
and record usage. The per-task monetary price can be zero while usage is still
accounted for.

### `postpaid_account`

The caller selects an approved credit account. The provider reserves account
capacity and records a charge or usage item for later settlement.

### `monthly_invoice`

The caller selects an invoice agreement. The provider appends a line item to a
billing period after the task reaches the agreed point.

### `clearing_account`

The caller selects a platform or inter-Agent clearing relationship. The service
records debits/credits for later net settlement.

### `sponsored`

An approved sponsor relationship funds or accounts for the task. The caller
must not substitute an arbitrary sponsor ID; the provider verifies the standing
relationship.

### `external_settlement`

The service is performed under a commercial agreement whose settlement occurs
outside ASCP. The provider still records an opaque external reference and
accurate state; ASCP does not falsely report an immediate capture.

## 3. Discovery and selection

Capability and Options responses advertise non-binding `BillingOption` values.
The client selects a preference in Direct or Prepare:

- mode;
- standing `arrangement_ref`, if applicable;
- optional client maximum amount.

The signed `BillingTerms` in a quote or the provider-validated Direct plan are
authoritative.

## 4. Signed billing terms

`BillingTerms` includes:

- mode;
- arrangement reference and whether it is required;
- whether per-call authorization is required;
- settlement timing;
- accepted authorization schemes;
- authorization and capture mode;
- refund policy URI;
- variable-price permission;
- billing period;
- usage unit.

A provider MUST NOT change the selected mode, arrangement, ceiling, or settlement
behavior after quote signature without issuing a new quote.

## 5. Per-call billing authorization

When required, `BillingAuthorization` binds:

- billing mode;
- standing arrangement, if any;
- opaque authorization reference;
- payer;
- provider service audience;
- maximum amount, when monetary;
- expiry;
- exact request or quote binding digest;
- single-use or reusable policy.

A provider MUST validate this object through the relevant processor or internal
billing system. Merely receiving JSON fields is not proof of authority.

A single-use authorization MUST NOT fund a different digest or idempotency key.

## 6. Adapter operations

A billing adapter exposes idempotent operations equivalent to:

```text
Reserve(binding, terms, authorization, ceiling, idempotency_key)
Settle(reservation, final_amount, idempotency_key)
Release(reservation, idempotency_key)
Refund(settlement, amount, idempotency_key)
```

The internal implementation may use payment intents, balance holds, quota
reservations, account credit, invoice drafts, clearing entries, sponsorship
approval, or external contract records.

## 7. State semantics

State names MUST describe the selected mode. Examples:

| Mode | Reservation state | Settlement state |
|---|---|---|
| `pay_now` | `reserved` | `captured` |
| `prepaid_balance` | `reserved` | `balance_debited` |
| `subscription` | `allowance_reserved` | `usage_recorded` |
| `postpaid_account` | `credit_reserved` | `invoice_item_recorded` |
| `monthly_invoice` | `credit_reserved` | `invoice_item_recorded` |
| `clearing_account` | `reserved` | `clearing_recorded` |
| `sponsored` | `sponsor_approved` | `sponsor_usage_recorded` |
| `external_settlement` | `external_acknowledged` | `external_settlement_recorded` |

A provider MUST NOT label subscription usage or an invoice line as a card
capture.

## 8. Ceiling and final amount

- All amounts use exact decimal strings.
- The provider MUST validate currency and scale.
- The final settled or recorded monetary amount MUST NOT exceed the signed
  ceiling.
- Variable pricing is forbidden unless signed terms explicitly allow it.
- If a standing arrangement includes the task, price may be zero while usage is
  recorded.

## 9. Execution ordering

The safe ordering depends on reversibility and billing mode. A common pattern is:

```text
validate terms and authority
→ reserve money/credit/quota/account capacity
→ durably create task/outbox
→ perform provider effect
→ settle or record usage
→ issue receipt
```

A provider MUST design recovery for crashes at every boundary. It MUST NOT repeat
an irreversible provider effect merely because billing status is uncertain.

## 10. Error classes

### Definitive decline

Examples: invalid arrangement, expired authorization, wrong audience, insufficient
balance, ceiling too small. Retry is false unless the client changes the terms or
funding relationship.

### Temporary no-effect failure

Retry is allowed only when the adapter positively guarantees that no reservation,
settlement, usage record, invoice line, or external effect was created.

### Unknown outcome

A timeout or ambiguous response after submission means the operation may have
succeeded. The idempotency claim remains locked. The provider returns a
reconciliation reference and resolves the original operation; it does not issue
a new one.

## 11. Refunds and credits

Refund or credit operations are idempotent and reference a prior settlement or
billing record. Cumulative reversal MUST NOT exceed the eligible settled amount
unless a separate, explicit goodwill-credit policy applies.

For subscriptions or invoices, a reversal may be represented as restored quota,
account credit, or a negative invoice item rather than a payment-rail refund.

## 12. Periodic and unified settlement

Monthly, postpaid, and clearing modes may complete the service task before cash
moves. The `BillingRecord` should include `invoice_ref`, `period_ref`, or a
clearing reference sufficient for audit and later reconciliation.

ASCP receipt signing proves what the service recorded; it does not replace the
provider's legal invoice, statement, tax document, or payment-processor record.

## 13. Security

- Opaque billing references are sensitive and must be access-controlled.
- Raw credentials must never appear in prompts, task JSON, logs, audit data, or
  artifacts.
- Provider and billing audiences must be exact.
- Webhooks and processor callbacks require signature and replay verification.
- Billing adapters require strict tenant isolation.
- Operators must monitor orphaned reservations, duplicated records, unusual
  usage, and reconciliation age.
