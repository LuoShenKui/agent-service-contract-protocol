# ASCP reference architecture

## 1. Layered model

```text
┌─────────────────────────────────────────────────────────────────────┐
│ External client Agent                                               │
│ intent selection · policy · quote verification · user interaction   │
└───────────────────────────────┬─────────────────────────────────────┘
                                │ ASCP
┌───────────────────────────────▼─────────────────────────────────────┐
│ Platform-owned service Agent                                       │
│ compact capabilities · Options · Direct planning · contract pricing │
└────────────┬──────────────┬──────────────┬──────────────┬───────────┘
             │              │              │              │
       Auth/delegation  Idempotency    Contract/task   Audit/signing
             │              │              │              │
             ├──────────────┼───────┬──────┼──────────────┤
             │              │       │      │              │
          Billing        File store │  Worker/outbox   KMS/HSM
             │                      │
             └────────── Provider-internal APIs / SQL / queues / MCP
```

The provider owns the internal schema and orchestration. The external caller
sees task-level contracts.

## 2. Direct Flow

```text
authenticate
→ strict decode
→ validate files/references
→ provider PlanDirect
→ enforce scopes and standing authority
→ require idempotency only when unsafe retry exists
→ reserve or validate billing when applicable
→ provider ExecuteDirect
→ settle/record billing
→ append audit
→ sign invocation receipt
→ persist replay response
```

The Direct planner is a policy boundary, not merely an LLM opinion. It must
return a complete plan with risk, scopes, billing, effects, and idempotency
requirements. The protocol engine fails closed if the plan is incomplete.

## 3. Contract Flow

```text
Negotiate
  → support, minimal parameters, scopes, files, billing choices

Prepare
  → exact validation, normalization, pricing
  → signed quote, no business side effect

Commit
  → quote/approval/scope/file/billing/idempotency validation
  → durable task + outbox
  → provider execution
  → settlement or usage record
  → signed receipt + audit
```

Production systems should commit task state, audit intent, and outbox publication
atomically. External billing reservations may require a saga and reconciler.

## 4. Capability discovery

The catalog is deliberately small and cacheable. A client filters task intents,
then requests detailed parameters only for the selected task. This reduces model
context, network transfer, and the chance that an external model misunderstands
provider-internal fields.

## 5. Files

File bytes use a separate upload path. Metadata and digest are declared first.
The storage layer is authoritative and tenant-bound. Production architecture:

```text
prepare ticket
→ quarantine upload
→ digest/size/type verification
→ malware/archive/parser scanning
→ ready FileRef
→ signed quote or Direct plan
→ provider-controlled retrieval
```

## 6. Billing adapter

The adapter does not assume a card payment. It provides provider-neutral
Reserve, Settle, Release, and Refund semantics over:

- payment authorization;
- prepaid balances;
- subscriptions and quota;
- postpaid credit;
- monthly invoices;
- clearing accounts;
- sponsorship;
- external agreements.

Adapter results use mode-specific states and opaque references.

## 7. Audit

Each task or invocation owns a resource-local signed hash chain. The final
receipt anchors the chain root. Production audit should be immutable or anchored
to an independent store.

## 8. Extension points

The Go engine exposes interfaces for:

- authentication;
- independent authorization;
- service capability/planning/execution;
- durable store;
- idempotency backend;
- billing processor;
- file store;
- signer;
- audit store.

The in-memory implementations are deterministic reference components, not
production dependencies.
