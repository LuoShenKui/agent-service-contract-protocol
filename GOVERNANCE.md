# ASCP governance

## Current phase

ASCP is in an author-led incubation phase. The immediate goal is to obtain public
review, independent implementations, adversarial tests, and real platform-agent
feedback before declaring a stable 1.0 protocol.

## Decision principles

Protocol decisions prioritize, in order:

1. prevention of unauthorized or duplicate real-world effects;
2. explicit uncertainty and recoverability;
3. interoperability and deterministic semantics;
4. data minimization and privacy;
5. operational simplicity;
6. model and vendor neutrality;
7. performance and convenience.

## Change process

- Editorial fixes may be merged through ordinary review.
- New optional extensions require documentation and test vectors.
- New core behavior requires an issue, security review, compatibility analysis,
  and conformance tests.
- Breaking wire changes require a new protocol version.
- A feature may not weaken signed quote binding, required idempotency,
  independent authorization, billing ceilings, file integrity, or audit evidence.

## Stabilization criteria

A 1.0 proposal should not be made until there are:

- at least two independent client implementations;
- at least two independent service implementations;
- successful cross-implementation conformance testing;
- an external security review;
- production experience with Direct, Contract, billed, and free tasks;
- documented resolution of crash-consistency and reconciliation cases;
- an established maintainer and disclosure process.

## Trademarks and naming

The project name is descriptive and no trademark rights are asserted by this
file. Implementations should state exact conformance version and avoid implying
certification without an approved conformance program.
