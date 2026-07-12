#!/usr/bin/env bash
# Demonstrate the compact ASCP 0.2 direct flow and optional semantic preflight.
# The full contract flow contains dynamic offer IDs and signatures, so use the
# Go reference client for an end-to-end verified send.
set -euo pipefail

BASE_URL="${ASCP_BASE_URL:-http://localhost:8080}"
TOKEN="${ASCP_DEMO_TOKEN:-ascp-demo-token}"

printf '%s\n' '--- compact capability catalog ---'
curl --fail-with-body --silent --show-error \
  "${BASE_URL}/.well-known/ascp/capabilities?limit=20" \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "ASCP-Version: 0.2" | jq .

printf '%s\n' '--- one-call free read ---'
curl --fail-with-body --silent --show-error \
  -X POST "${BASE_URL}/v1/invoke" \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "ASCP-Version: 0.2" \
  -H "Content-Type: application/ascp+json" \
  --data-binary @examples/requests/direct-read-request.json | jq .

printf '%s\n' '--- optional preflight for email.send ---'
curl --fail-with-body --silent --show-error \
  -X POST "${BASE_URL}/v1/options" \
  -H "Authorization: Bearer ${TOKEN}" \
  -H "ASCP-Version: 0.2" \
  -H "Content-Type: application/ascp+json" \
  --data-binary @examples/requests/options-request.json | jq .
