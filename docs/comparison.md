# ASCP, MCP, A2A, and ordinary APIs

The protocols solve different layers. The table describes fit, not a universal
quality ranking.

| Concern | Ordinary API | MCP | A2A | ASCP |
|---|---|---|---|---|
| Provider-internal deterministic operation | Strong | Wrapper possible | Not primary | Not primary |
| Tool/resource exposure to a model | Custom | Core fit | Custom | Deliberately bounded |
| General Agent messaging/delegation | Custom | Limited | Core fit | Task service only |
| Compact task catalog | Custom | Tool list can be large | Agent Card/skills | Core capability catalog |
| One-call low-risk service task | Custom | Tool call | Message/task | Core Direct Flow |
| Optional task-specific preflight | Custom | Custom | Custom | Core Options |
| Binding signed quote | Custom | No core rule | Custom | Core Contract Flow |
| Independent approval binding | Custom | Custom | Custom | Core requirement |
| Multiple billing arrangements | Custom | Custom | Custom | Core billing profile |
| Conditional/mandatory idempotency | Custom | Server-specific | Custom | Core requirement |
| Scoped digest-bound file transfer | Custom | Resource-specific | Artifact-specific | Core file profile |
| Signed completion receipt | Custom | No core rule | Custom | Core requirement |
| Signed hash-chain audit | Custom | No core rule | Custom | Core profile |

## Recommended composition

```text
Human or enterprise workflow
        │
        ▼
Client Agent / orchestrator
        │
        ├── A2A or registry: discover/delegate to a platform Agent
        │
        └── ASCP: invoke or contract the service
                     │
                     ▼
              Platform-owned Agent
                     │
                     ├── ordinary API / SQL / queue
                     ├── deterministic policy engine
                     ├── provider-internal model
                     └── MCP tools where useful
```

## Why not publish every internal method as MCP tools?

That approach can be appropriate for bounded internal automation, but a large
consumer platform may have hundreds of methods and complex field precedence. An
external model would need to load schemas, move raw records, understand private
semantics, and reproduce provider policy.

ASCP lets the platform publish a few task-level capabilities. The platform Agent
performs internal queries and returns only the result, evidence, or signed terms
needed for the task.

## Why not use only A2A?

A2A is suitable for discovery, communication, delegation, status, and artifacts.
ASCP adds a specialized service-transaction contract: Direct eligibility,
side-effect-free quote preparation, signed price/billing/files/permissions,
independent approval, idempotency, settlement evidence, and audit.

An A2A task may carry or point to an ASCP transaction. Conversely, an ASCP client
may discover the provider through A2A.

## Why not use only an ordinary REST API?

A carefully designed REST API can implement all required semantics. ASCP is a
shared profile for those semantics, allowing client Agents to use one lifecycle
across different platform Agents without learning every provider's bespoke
contract, idempotency, authorization, file, billing, and audit model.
