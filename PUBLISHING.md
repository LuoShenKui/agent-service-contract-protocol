# Publishing ASCP

Release automation treats the committed Git tree as the source of truth.

## 1. Repository

The public repository is:

```text
github.com/LuoShenKui/agent-service-contract-protocol
```

The repository currently uses `master` as its default branch. A maintainer may
rename it later, but workflows and protection must always cover the actual
default branch.

## 2. Repository controls

Before accepting external contributions or production claims:

1. protect the default branch and require CI and CodeQL;
2. require review for `spec/`, `schemas/`, `openapi/`, billing, idempotency,
   authorization, signing, files, and audit changes;
3. enable secret scanning, push protection, Dependabot, and private vulnerability
   reporting;
4. disable force-push and deletion of release branches/tags;
5. separate release-manager and security permissions;
6. prefer signed commits/tags or vigilant mode where practical.

## 3. Validate a release candidate

```bash
make check

ASCP_ADDR=:18080 \
ASCP_BASE_URL=http://localhost:18080 \
  ./bin/ascp-server

./bin/ascp-conformance \
  -base-url http://localhost:18080 \
  -token ascp-demo-token
```

The reference service is not a production deployment. Providers must complete
`docs/production-readiness-checklist.md` and replace all demo adapters.

## 4. Create a draft release

```bash
./scripts/release.sh v0.2.0-draft.1

git show v0.2.0-draft.1

git push origin master
git push origin v0.2.0-draft.1
```

The tag workflow builds cross-platform binaries, source archive, and checksums.
Tags containing `draft`, `alpha`, `beta`, or `rc` become GitHub prereleases.

## 5. Versioning

- `0.x` versions are working drafts.
- Breaking wire changes require a new protocol version.
- Capability schemas such as `email.send/2` version independently.
- A release tag must preserve matching spec, OpenAPI, JSON Schema, examples,
  conformance cases, implementation, and validation results.
- Never move or recreate a published tag.

## 6. SDK publication

Generated SDKs must preserve:

- signed fields and exact JSON projection;
- optional versus mandatory idempotency;
- problem details and reconciliation;
- billing modes and standing arrangements;
- file references and upload constraints;
- task and invocation states;
- receipt and audit verification.
