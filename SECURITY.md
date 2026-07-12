# Security policy

## Supported versions

Security fixes target the latest 0.2.x working draft and the default branch until
a stable support policy is published.

## Reporting a vulnerability

Do not open a public issue for suspected vulnerabilities involving:

- authentication, delegation, or authorization bypass;
- quote or receipt signature verification;
- idempotency, replay, or duplicate real-world effects;
- cross-tenant resource access;
- billing arrangement, ceiling, settlement, or refund enforcement;
- file upload, parser, malware, or attachment substitution;
- callback SSRF;
- sensitive-data exposure;
- audit tampering;
- supply-chain compromise.

Use GitHub private vulnerability reporting when enabled. Until then, contact the
repository owner privately through the GitHub profile.

Include:

- affected commit or release;
- reproducible request/response sequence;
- impact and prerequisite access;
- suggested mitigation when known;
- whether real credentials or personal/customer data were involved.

## Security expectations

This repository is not independently audited. The reference service uses an
in-memory store, static demo token, demo approval verifier, mock billing, local
memory files, and possibly an ephemeral signing key. Those components are
explicitly unsuitable for public production use. Follow
`docs/deployment.md`, `docs/production-readiness-checklist.md`, and
`spec/security-profile.md`.
