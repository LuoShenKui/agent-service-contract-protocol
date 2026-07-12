# ASCP threat model

## Assets

- user and enterprise authority;
- email, documents, files, and private platform data;
- money, credit, quota, invoices, and clearing records;
- signed quotes and receipts;
- idempotency state and stable task identities;
- provider side effects;
- audit integrity and privacy;
- signing, OAuth, billing, upload, and callback credentials.

## Adversaries and failures

- malicious external Agent;
- compromised user token;
- confused deputy or cross-tenant reference;
- prompt injection from task or retrieved content;
- dishonest or buggy provider integration;
- duplicate/reordered network requests;
- process crash or partial transaction;
- billing/provider timeout with unknown outcome;
- malicious attachment or decompression bomb;
- callback SSRF and credential exfiltration;
- insider abuse and audit tampering;
- stale, unsupported, or incomplete real-world data.

## Principal threats and mitigations

| Threat | Failure mode | Required mitigation |
|---|---|---|
| Tool-schema overload | Client loads irrelevant internal schemas and misroutes work | Compact capability catalog and task-specific Options |
| Unsafe one-call execution | Irreversible task runs without terms | Provider Direct planner fails closed with `contract_required` |
| Prompt-created authority | Content instructs Agent to send/pay/read more | Independent digest-bound authorization and deterministic scope checks |
| Quote substitution | Price, destination, file, or callback changes after review | Signed complete quote and payload comparison |
| Replay/duplicate effect | Retry sends email or bills twice | Scoped idempotency, provider deduplication, exact response replay |
| Idempotency key reuse | Different payload hides under old key | Request digest conflict rejection |
| Overcharge | Final amount exceeds approval | Signed ceiling and billing adapter validation |
| Standing-account abuse | Caller names another subscription/invoice | Tenant-bound arrangement verification |
| Unknown billing outcome | Timeout causes second charge/effect | Locked idempotency and reconciliation |
| File substitution | Different bytes replace approved attachment | Declared size/digest and signed FileRef |
| Malicious content | Parser exploit, malware, archive bomb | Quarantine, scan, sandbox, resource limits |
| Cross-tenant file/artifact | Guessing IDs exposes data | Owner/object authorization and opaque IDs |
| Callback SSRF | Provider calls internal metadata service | Ownership proof, allowlist, DNS/IP checks, controlled relay |
| Audit rewriting | Provider changes history | Signed hash chain and immutable/independent anchoring |
| Data leakage through logs | Bodies/tokens/files enter telemetry | Redaction, references, digest-only audit, access control |
| Empty data interpreted as safe | Unsupported/stale source appears normal | Explicit coverage, freshness, unknown/conflict states in domain profiles |

## Failure boundaries to test

Inject crashes or timeouts:

1. before idempotency acquisition;
2. after claim but before quote/file/task creation;
3. after billing reserve but before task transaction;
4. after task commit but before outbox publication;
5. during provider side effect;
6. after provider success but before durable result;
7. during billing settlement or usage recording;
8. before receipt signing;
9. after receipt signing but before response storage;
10. during callback delivery;
11. during file upload and scan;
12. during backup restore and regional failover.

For each boundary, prove that the system either returns the original result,
continues reconciliation under the same stable ID, or definitively proves no
effect before allowing a new attempt.

## Residual risk

ASCP cannot prove that an upstream platform's data is true, complete, fresh, or
unbiased. Platform-owned Agents and domain profiles should expose provenance,
coverage, freshness, confidence, and unknown states. High-risk callers may need
independent sources and human review.
