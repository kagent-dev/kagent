#!/usr/bin/env bash

set -euo pipefail

API_URL="${API_URL:-http://localhost:8080/api/plugins}"
PLUGIN_PATH_PREFIX="${PLUGIN_PATH_PREFIX:-kanban}"
PLUGIN_SECTION="${PLUGIN_SECTION:-AGENTS}"
CONNECT_TIMEOUT_SECONDS="${CONNECT_TIMEOUT_SECONDS:-5}"
MAX_TIME_SECONDS="${MAX_TIME_SECONDS:-15}"

usage() {
  cat <<'EOF'
Check kagent plugins API and verify expected plugin entry.

Usage:
  scripts/check-plugins-api.sh [--url <api-url>] [--plugin <pathPrefix>] [--section <section>]

Options:
  --url       Full plugins endpoint URL (default: http://localhost:8080/api/plugins)
  --plugin    Plugin pathPrefix to validate (default: kanban)
  --section   Expected section for plugin (default: AGENTS)
  -h, --help  Show help

Environment overrides:
  API_URL
  PLUGIN_PATH_PREFIX
  PLUGIN_SECTION
  CONNECT_TIMEOUT_SECONDS
  MAX_TIME_SECONDS

Exit codes:
  0  API reachable and expected plugin found
  1  Validation failed
  2  Missing required runtime dependency
EOF
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --url)
      API_URL="$2"
      shift 2
      ;;
    --plugin)
      PLUGIN_PATH_PREFIX="$2"
      shift 2
      ;;
    --section)
      PLUGIN_SECTION="$2"
      shift 2
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown argument: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
done

if ! command -v curl >/dev/null 2>&1; then
  echo "ERROR: curl is required but not found in PATH." >&2
  exit 2
fi

if ! command -v python3 >/dev/null 2>&1; then
  echo "ERROR: python3 is required but not found in PATH." >&2
  exit 2
fi

tmp_body="$(mktemp)"
trap 'rm -f "$tmp_body"' EXIT

echo "Checking endpoint: $API_URL"
http_code="$(
  curl -sS \
    --connect-timeout "$CONNECT_TIMEOUT_SECONDS" \
    --max-time "$MAX_TIME_SECONDS" \
    -w "%{http_code}" \
    -o "$tmp_body" \
    "$API_URL"
)"

if [[ "$http_code" != "200" ]]; then
  echo "ERROR: expected HTTP 200, got $http_code" >&2
  echo "Response body:" >&2
  cat "$tmp_body" >&2
  exit 1
fi

python3 - "$tmp_body" "$PLUGIN_PATH_PREFIX" "$PLUGIN_SECTION" <<'PY'
import json
import sys

body_path, expected_prefix, expected_section = sys.argv[1:]

try:
    with open(body_path, "r", encoding="utf-8") as f:
        payload = json.load(f)
except Exception as exc:
    print(f"ERROR: response is not valid JSON: {exc}", file=sys.stderr)
    sys.exit(1)

data = payload.get("data")
if not isinstance(data, list):
    print("ERROR: response JSON does not contain list field 'data'.", file=sys.stderr)
    print(json.dumps(payload, indent=2), file=sys.stderr)
    sys.exit(1)

print(f"Found {len(data)} plugin(s) in /api/plugins")

match = None
for item in data:
    if not isinstance(item, dict):
        continue
    if item.get("pathPrefix") == expected_prefix:
        match = item
        break

if match is None:
    print(f"ERROR: plugin with pathPrefix='{expected_prefix}' not found.", file=sys.stderr)
    if data:
        known = [str(p.get("pathPrefix")) for p in data if isinstance(p, dict)]
        print(f"Known pathPrefix values: {', '.join(known)}", file=sys.stderr)
    sys.exit(1)

actual_section = match.get("section")
if actual_section != expected_section:
    print(
        f"ERROR: plugin '{expected_prefix}' section mismatch: "
        f"expected '{expected_section}', got '{actual_section}'",
        file=sys.stderr,
    )
    print(json.dumps(match, indent=2), file=sys.stderr)
    sys.exit(1)

print("PASS: plugin entry is present and section matches.")
print(json.dumps(match, indent=2))
PY

