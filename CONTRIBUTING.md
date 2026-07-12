# Contributing to ASCP

ASCP welcomes protocol, implementation, documentation, conformance, and security
contributions.

## Development workflow

1. Open an issue for wire-level or normative changes before implementation.
2. Keep each pull request focused.
3. Add or update conformance cases for every normative change.
4. Update specification, OpenAPI, JSON Schema, examples, and Go types together
   when the wire contract changes.
5. Run the complete release gate.

```bash
make check
```

The expanded commands are:

```bash
gofmt -w ./cmd ./internal ./pkg
go vet ./...
go test -race ./...
go build ./cmd/...
python ./scripts/validate_specs.py
```

## Normative changes

A normative change includes:

- user/interoperability problem;
- security and privacy impact;
- compatibility analysis;
- exact MUST/SHOULD language;
- positive and negative conformance tests;
- migration plan.

Breaking changes target a new protocol version. Do not silently alter published
0.2 wire semantics.

## Code style

- Use standard Go formatting and idioms.
- Comment exported identifiers and non-obvious reliability decisions.
- Use deterministic code for authorization, billing, idempotency, files, and
  state transitions; do not hide those controls inside prompts.
- Avoid dependencies when the standard library is sufficient.
- Never add real credentials, personal data, production billing references, or
  unredacted file content to fixtures and logs.

## Pull requests

Describe:

- what changed;
- why it is needed;
- protocol/security implications;
- tests performed;
- compatibility and migration impact.

Contributions are licensed under Apache-2.0.
