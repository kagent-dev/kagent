#!/usr/bin/env bash

set -euo pipefail

API_URL="${API_URL:-http://localhost:8080/api/plugins}"
PLUGIN_PATH_PREFIX="${PLUGIN_PATH_PREFIX:-kanban-mcp}"
PLUGIN_SECTION="${PLUGIN_SECTION:-AGENTS}"
CONNECT_TIMEOUT_SECONDS="${CONNECT_TIMEOUT_SECONDS:-5}"
MAX_TIME_SECONDS="${MAX_TIME_SECONDS:-15}"
WAIT=false
WAIT_TIMEOUT="${WAIT_TIMEOUT:-120}"
WAIT_INTERVAL="${WAIT_INTERVAL:-5}"
PROXY_CHECK=false
PROXY_BASE_URL="${PROXY_BASE_URL:-http://localhost:8080}"

usage() {
  cat <<'EOF'
Check kagent plugins API and verify expected plugin entry.

Usage:
  scripts/check-plugins-api.sh [OPTIONS]

Options:
  --url       Full plugins endpoint URL (default: http://localhost:8080/api/plugins)
  --plugin    Plugin pathPrefix to validate (default: kanban-mcp)
  --section   Expected section for plugin (default: AGENTS)
  --wait      Poll until plugin appears (default: false)
  --wait-timeout  Max seconds to wait in poll mode (default: 120)
  --wait-interval Seconds between poll attempts (default: 5)
  --proxy     Also verify /_p/{plugin}/ reverse proxy returns non-404
  --proxy-base-url  Base URL for proxy check (default: http://localhost:8080)
  -h, --help  Show help

Environment overrides:
  API_URL, PLUGIN_PATH_PREFIX, PLUGIN_SECTION
  CONNECT_TIMEOUT_SECONDS, MAX_TIME_SECONDS
  WAIT_TIMEOUT, WAIT_INTERVAL, PROXY_BASE_URL

Exit codes:
  0  API reachable and expected plugin found (and proxy ok if --proxy)
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
    --wait)
      WAIT=true
      shift
      ;;
    --wait-timeout)
      WAIT_TIMEOUT="$2"
      shift 2
      ;;
    --wait-interval)
      WAIT_INTERVAL="$2"
      shift 2
      ;;
    --proxy)
      PROXY_CHECK=true
      shift
      ;;
    --proxy-base-url)
      PROXY_BASE_URL="$2"
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

# check_plugins_api does a single check. Returns 0 on success.
check_plugins_api() {
  local http_code
  http_code="$(
    curl -sS \
      --connect-timeout "$CONNECT_TIMEOUT_SECONDS" \
      --max-time "$MAX_TIME_SECONDS" \
      -w "%{http_code}" \
      -o "$tmp_body" \
      "$API_URL" 2>/dev/null
  )" || http_code="000"

  if [[ "$http_code" != "200" ]]; then
    echo "  HTTP $http_code (expected 200)"
    return 1
  fi

  python3 - "$tmp_body" "$PLUGIN_PATH_PREFIX" "$PLUGIN_SECTION" <<'PY'
import json
import sys

body_path, expected_prefix, expected_section = sys.argv[1:]

try:
    with open(body_path, "r", encoding="utf-8") as f:
        payload = json.load(f)
except Exception as exc:
    print(f"  JSON parse error: {exc}", file=sys.stderr)
    sys.exit(1)

data = payload.get("data")
if not isinstance(data, list):
    print("  Response missing 'data' list", file=sys.stderr)
    sys.exit(1)

match = None
for item in data:
    if not isinstance(item, dict):
        continue
    if item.get("pathPrefix") == expected_prefix:
        match = item
        break

if match is None:
    known = [str(p.get("pathPrefix")) for p in data if isinstance(p, dict)]
    print(f"  Plugin '{expected_prefix}' not found (have: {', '.join(known) or 'none'})")
    sys.exit(1)

actual_section = match.get("section")
if actual_section != expected_section:
    print(f"  Section mismatch: expected '{expected_section}', got '{actual_section}'", file=sys.stderr)
    sys.exit(1)

print(f"  PASS: plugin '{expected_prefix}' found in section '{expected_section}'")
print(json.dumps(match, indent=2))
PY
}

echo "Checking endpoint: $API_URL"
echo "Looking for plugin: $PLUGIN_PATH_PREFIX (section: $PLUGIN_SECTION)"

if [[ "$WAIT" == "true" ]]; then
  echo "Polling mode: timeout=${WAIT_TIMEOUT}s, interval=${WAIT_INTERVAL}s"
  elapsed=0
  while (( elapsed < WAIT_TIMEOUT )); do
    if check_plugins_api; then
      break
    fi
    elapsed=$(( elapsed + WAIT_INTERVAL ))
    if (( elapsed >= WAIT_TIMEOUT )); then
      echo "ERROR: timed out after ${WAIT_TIMEOUT}s waiting for plugin" >&2
      exit 1
    fi
    echo "  Retrying in ${WAIT_INTERVAL}s... (${elapsed}/${WAIT_TIMEOUT}s)"
    sleep "$WAIT_INTERVAL"
  done
else
  if ! check_plugins_api; then
    echo "ERROR: plugin check failed" >&2
    exit 1
  fi
fi

# Proxy check: verify /_p/{name}/ returns non-404
if [[ "$PROXY_CHECK" == "true" ]]; then
  proxy_url="${PROXY_BASE_URL}/_p/${PLUGIN_PATH_PREFIX}/"
  echo ""
  echo "Checking proxy: $proxy_url"
  proxy_code="$(
    curl -sS \
      --connect-timeout "$CONNECT_TIMEOUT_SECONDS" \
      --max-time "$MAX_TIME_SECONDS" \
      -w "%{http_code}" \
      -o /dev/null \
      "$proxy_url" 2>/dev/null
  )" || proxy_code="000"

  if [[ "$proxy_code" == "404" ]]; then
    echo "ERROR: proxy returned 404 — plugin routing not configured" >&2
    exit 1
  fi

  echo "  PASS: proxy returned HTTP $proxy_code (non-404)"
fi

echo ""
echo "All checks passed."
