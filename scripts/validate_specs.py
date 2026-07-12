#!/usr/bin/env python3
"""Validate ASCP schemas, examples, OpenAPI, docs, and conformance metadata.

The release gate is deterministic and performs no network access. It checks the
contract that implementers consume rather than relying only on Go compilation.
"""

from __future__ import annotations

import json
import re
import sys
from pathlib import Path
from typing import Any, Iterable

import yaml
from jsonschema import Draft202012Validator, FormatChecker

ROOT = Path(__file__).resolve().parents[1]
SCHEMA_PATH = ROOT / "schemas" / "ascp-v0.2.schema.json"
OPENAPI_PATH = ROOT / "openapi" / "ascp-v0.2.yaml"


def load_json(path: Path) -> Any:
    """Load JSON and attach the source path to diagnostics."""
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except Exception as exc:  # pragma: no cover - diagnostic path
        raise RuntimeError(f"could not parse JSON {path}: {exc}") from exc


def resolve_json_pointer(document: Any, fragment: str) -> Any:
    """Resolve the RFC 6901 fragments used by local OpenAPI references."""
    if fragment in ("", "#"):
        return document
    if not fragment.startswith("#/"):
        raise ValueError(f"unsupported JSON reference fragment {fragment!r}")
    current = document
    for raw_token in fragment[2:].split("/"):
        token = raw_token.replace("~1", "/").replace("~0", "~")
        current = current[int(token)] if isinstance(current, list) else current[token]
    return current


def walk(value: Any) -> Iterable[Any]:
    """Yield every node in a parsed JSON/YAML document."""
    yield value
    if isinstance(value, dict):
        for child in value.values():
            yield from walk(child)
    elif isinstance(value, list):
        for child in value:
            yield from walk(child)


def validate_instance(schema: dict[str, Any], definition: str, path: Path) -> None:
    """Validate one public example against a named schema definition."""
    wrapper = {
        "$schema": schema["$schema"],
        "$defs": schema["$defs"],
        "$ref": f"#/$defs/{definition}",
    }
    validator = Draft202012Validator(wrapper, format_checker=FormatChecker())
    errors = sorted(validator.iter_errors(load_json(path)), key=lambda error: list(error.path))
    if errors:
        rendered = "\n".join(f"  - {list(error.path)}: {error.message}" for error in errors)
        raise AssertionError(f"{path} is not a valid {definition}:\n{rendered}")


def validate_openapi_refs(openapi: dict[str, Any]) -> None:
    """Ensure every internal or local-file OpenAPI reference resolves."""
    for node in walk(openapi):
        if not isinstance(node, dict) or "$ref" not in node:
            continue
        reference = node["$ref"]
        if reference.startswith("#"):
            resolve_json_pointer(openapi, reference)
            continue
        file_part, separator, fragment = reference.partition("#")
        target_path = (OPENAPI_PATH.parent / file_part).resolve()
        if not target_path.is_relative_to(ROOT):
            raise AssertionError(f"OpenAPI reference escapes repository: {reference}")
        if not target_path.exists():
            raise AssertionError(f"OpenAPI reference does not exist: {reference}")
        target = load_json(target_path) if target_path.suffix == ".json" else yaml.safe_load(target_path.read_text(encoding="utf-8"))
        resolve_json_pointer(target, f"#{fragment}" if separator else "#")


def operation_parameter_refs(path_item: dict[str, Any], operation: dict[str, Any]) -> set[str | None]:
    """Collect shared parameter references from a path and one operation."""
    parameters = list(path_item.get("parameters", [])) + list(operation.get("parameters", []))
    return {parameter.get("$ref") for parameter in parameters if isinstance(parameter, dict)}


def validate_idempotency_contract(openapi: dict[str, Any]) -> None:
    """Check required, optional, and intentionally absent idempotency semantics."""
    required_post_paths = {
        "/v1/negotiate",
        "/v1/prepare",
        "/v1/commit",
        "/v1/files/prepare-upload",
        "/v1/tasks/{task_id}/cancel",
    }
    for route in required_post_paths:
        path_item = openapi["paths"][route]
        refs = operation_parameter_refs(path_item, path_item["post"])
        if "#/components/parameters/IdempotencyKey" not in refs:
            raise AssertionError(f"POST {route} must require the shared Idempotency-Key")

    invoke_item = openapi["paths"]["/v1/invoke"]
    invoke_refs = operation_parameter_refs(invoke_item, invoke_item["post"])
    if "#/components/parameters/OptionalIdempotencyKey" not in invoke_refs:
        raise AssertionError("POST /v1/invoke must declare conditionally required idempotency")

    options_item = openapi["paths"]["/v1/options"]
    options_refs = operation_parameter_refs(options_item, options_item["post"])
    if any(ref and "IdempotencyKey" in ref for ref in options_refs):
        raise AssertionError("side-effect-free POST /v1/options must not require idempotency")


def validate_yaml_files() -> None:
    """Parse every YAML file so CI catches malformed deployment metadata."""
    for path in sorted(list(ROOT.rglob("*.yml")) + list(ROOT.rglob("*.yaml"))):
        if ".git" in path.parts:
            continue
        try:
            list(yaml.safe_load_all(path.read_text(encoding="utf-8")))
        except Exception as exc:
            raise AssertionError(f"could not parse YAML {path}: {exc}") from exc


def validate_local_markdown_links() -> None:
    """Check repository-relative Markdown links without making network calls."""
    link_pattern = re.compile(r"(?<!!)\[[^\]]*\]\(([^)]+)\)")
    for path in ROOT.rglob("*.md"):
        if ".git" in path.parts:
            continue
        for match in link_pattern.finditer(path.read_text(encoding="utf-8")):
            raw_target = match.group(1).strip().split()[0].strip("<>")
            if raw_target.startswith(("http://", "https://", "mailto:", "#")):
                continue
            target_part = raw_target.split("#", 1)[0]
            if not target_part:
                continue
            target = (path.parent / target_part).resolve()
            if not target.is_relative_to(ROOT) or not target.exists():
                raise AssertionError(f"broken local Markdown link in {path}: {raw_target}")


def main() -> int:
    schema = load_json(SCHEMA_PATH)
    Draft202012Validator.check_schema(schema)
    if schema.get("$id") != "urn:ascp:schema:0.2":
        raise AssertionError("schema ID must be urn:ascp:schema:0.2")

    examples = {
        "DirectInvocationRequest": ROOT / "examples/requests/direct-read-request.json",
        "OptionsRequest": ROOT / "examples/requests/options-request.json",
        "NegotiationRequest": ROOT / "examples/requests/negotiate-request.json",
        "PrepareRequest": ROOT / "examples/requests/prepare-request.json",
        "CommitRequest": ROOT / "examples/requests/commit-request.json",
        "CommitRequest#standing": ROOT / "examples/requests/commit-subscription-request.json",
        "FileUploadRequest": ROOT / "examples/requests/file-upload-request.json",
        "CancelRequest": ROOT / "examples/requests/cancel-request.json",
        "CapabilityCatalog": ROOT / "examples/responses/capabilities-response.json",
        "OptionsResponse": ROOT / "examples/responses/options-response.json",
        "DirectInvocationResponse": ROOT / "examples/responses/direct-read-response.json",
        "FileRef": ROOT / "examples/responses/file-ref.json",
        "Problem": ROOT / "examples/problem.json",
    }
    for key, path in examples.items():
        definition = key.split("#", 1)[0]
        validate_instance(schema, definition, path)

    validate_yaml_files()
    validate_local_markdown_links()

    openapi = yaml.safe_load(OPENAPI_PATH.read_text(encoding="utf-8"))
    if not str(openapi.get("openapi", "")).startswith("3.1."):
        raise AssertionError("OpenAPI document must declare a 3.1.x version")
    if openapi.get("info", {}).get("version") != "0.2.0-draft.1":
        raise AssertionError("OpenAPI info.version is not 0.2.0-draft.1")
    validate_openapi_refs(openapi)
    validate_idempotency_contract(openapi)

    required_definitions = {
        "CapabilityCatalog",
        "OptionsRequest",
        "DirectInvocationRequest",
        "DirectInvocationResponse",
        "BillingTerms",
        "BillingRecord",
        "FileUploadTicket",
        "FileRef",
        "Quote",
        "Receipt",
        "AuditEvent",
    }
    missing = sorted(required_definitions - set(schema["$defs"]))
    if missing:
        raise AssertionError(f"schema is missing v0.2 definitions: {missing}")

    quote_properties = schema["$defs"]["Quote"]["properties"]
    for required_property in ("input_files", "billing_terms", "callback", "confirmation"):
        if required_property not in quote_properties:
            raise AssertionError(f"Quote schema is missing signed {required_property}")
    receipt_properties = schema["$defs"]["Receipt"]["properties"]
    if "billed_breakdown" not in receipt_properties or "billing" not in receipt_properties:
        raise AssertionError("Receipt schema is missing final billing evidence")
    if "IdempotencyReleased" not in openapi["components"]["headers"]:
        raise AssertionError("OpenAPI is missing Idempotency-Released semantics")

    cases = load_json(ROOT / "conformance/cases.json")
    identifiers = [case["id"] for case in cases["cases"]]
    if len(identifiers) != len(set(identifiers)):
        raise AssertionError("conformance case IDs must be unique")
    if cases.get("version") != "0.2":
        raise AssertionError("conformance index version is not 0.2")

    forbidden = "agentservice" + "protocol.dev"
    for path in ROOT.rglob("*"):
        if path.is_file() and ".git" not in path.parts:
            try:
                content = path.read_text(encoding="utf-8")
            except UnicodeDecodeError:
                continue
            if forbidden in content:
                raise AssertionError(f"uncontrolled placeholder domain remains in {path}")

    print(
        "validated ASCP 0.2: "
        f"{len(schema['$defs'])} schemas, {len(examples)} examples, "
        f"{len(openapi['paths'])} OpenAPI paths, {len(identifiers)} conformance cases"
    )
    return 0


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as error:
        print(f"validation failed: {error}", file=sys.stderr)
        raise SystemExit(1)
