# ASCP 0.2 protocol invariants

These rules apply across transports and domain profiles.

## Discovery and flow invariants

1. **Task-level discovery:** capability catalogs expose compact service intents,
   not the provider's complete internal tool surface.
2. **Options is optional and side-effect-free:** it creates no authority, quote,
   task, billable usage, or provider effect.
3. **Direct fails closed:** a task that cannot be proven eligible for Direct Flow
   returns `contract_required` and is not silently executed.
4. **Contract preparation is side-effect-free:** Prepare can validate and price,
   but cannot perform the requested business effect.

## Identity and authority invariants

5. **Authenticated delegation binds Actor and Principal.**
6. **Natural language cannot create authority.** Approval is independently
   verified and digest-bound.
7. **Scopes are checked at execution time, not only during discovery.**
8. **Tenant and object ownership are enforced for every resource and reference.**

## Signature invariants

9. **All execution-changing terms are signed.** Contract price, billing, files,
   destination, callback, effects, permissions, data use, timing, Actor, and
   Principal are not mutable after signature.
10. **The embedded JWS payload is authoritative.** Client verification compares
    it with the received unsigned object and trusted service identity.
11. **Receipts bind final outcome, artifacts, billing, and audit root.**

## Idempotency invariants

12. **Unsafe retries require idempotency.** Free read-only Direct calls may omit a
    key; side-effecting Direct calls and all Contract mutations require one.
13. **Same key and same digest replays; same key and different digest conflicts.**
14. **Unknown outcome is not failure.** Ambiguous billing, persistence, callback,
    upload, or provider effects remain locked for reconciliation.
15. **Claims are released only after proven no effect.**

## Billing invariants

16. **Immediate payment is optional.** Standing subscriptions, prepaid balances,
    postpaid accounts, invoices, clearing, sponsorship, and external settlement
    are first-class modes.
17. **Raw reusable payment credentials never cross ASCP.**
18. **Billing is service- and digest-bound.** Mode, payer, arrangement, audience,
    expiry, usage, currency, and ceiling are validated.
19. **Per-call authorization is required only when signed terms require it.**
20. **No duplicate settlement and no over-settlement or over-refund.**
21. **Billing uncertainty never causes an irreversible task to be repeated.**

## File invariants

22. **Large bytes remain outside protocol JSON.**
23. **Every accepted file is owner-, token-, type-, size-, digest-, expiry-, and
    state-validated.**
24. **Unready or policy-blocked files cannot affect execution.**
25. **Every contract input file is inside the signed quote.**
26. **Caller metadata never overrides the provider's authoritative file record.**

## Audit and privacy invariants

27. **Material transitions are append-only and independently verifiable.**
28. **Receipt roots anchor the relevant audit chain.**
29. **Audit avoids raw secrets and unnecessary content.**
30. **Empty, stale, unsupported, or failed upstream data is not automatically a
    negative real-world fact.**
31. **Context and artifacts use scoped references where practical.**
