#!/usr/bin/env bash
set -euo pipefail

KAGENT_URL="${KAGENT_URL:-http://localhost:8083}"
NAMESPACE="${NAMESPACE:-kagent}"
AGENT_NAME="${AGENT_NAME:-memory-showcase-agent}"
USER_ID="${USER_ID:-memory-showcase-user@example.com}"
TIMEOUT_SECONDS="${TIMEOUT_SECONDS:-180}"
AUTH_HEADER="${AUTH_HEADER:-}"
RESET_MEMORY="${RESET_MEMORY:-true}"
DEMO_FACT="blue-sunrise"

require() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "Missing required command: $1" >&2
    exit 1
  fi
}

new_id() {
  if command -v uuidgen >/dev/null 2>&1; then
    uuidgen | tr '[:upper:]' '[:lower:]'
  else
    printf "memory-showcase-%s-%s" "$(date +%s)" "$RANDOM"
  fi
}

json_payload() {
  local prompt="$1"
  local context_id="$2"
  local message_id rpc_id
  message_id="$(new_id)"
  rpc_id="$(new_id)"

  jq -n \
    --arg rpc_id "$rpc_id" \
    --arg message_id "$message_id" \
    --arg context_id "$context_id" \
    --arg prompt "$prompt" \
    '{
      jsonrpc: "2.0",
      method: "message/stream",
      id: $rpc_id,
      params: {
        message: {
          kind: "message",
          messageId: $message_id,
          contextId: $context_id,
          role: "user",
          parts: [{ kind: "text", text: $prompt }]
        },
        metadata: {}
      }
    }'
}

curl_headers=(
  -H "Content-Type: application/json"
  -H "Accept: text/event-stream"
  -H "X-User-ID: ${USER_ID}"
)

if [[ -n "$AUTH_HEADER" ]]; then
  curl_headers+=(-H "Authorization: ${AUTH_HEADER}")
fi

send_message() {
  local prompt="$1"
  local context_id="$2"
  local output_file="$3"

  echo
  echo ">>> ${prompt}"
  curl -fsS -N \
    --max-time "$TIMEOUT_SECONDS" \
    "${curl_headers[@]}" \
    -d "$(json_payload "$prompt" "$context_id")" \
    "${KAGENT_URL%/}/api/a2a/${NAMESPACE}/${AGENT_NAME}/" \
    | tee "$output_file" >/dev/null
}

extract_text() {
  local output_file="$1"
  sed -n 's/^data: //p' "$output_file" \
    | sed '/^\[DONE\]$/d' \
    | jq -r '.. | objects | select(has("text")) | .text' \
    | sed '/^$/d'
}

list_memories() {
  local encoded_agent encoded_user
  encoded_agent="$(jq -rn --arg value "$AGENT_NAME" '$value | @uri')"
  encoded_user="$(jq -rn --arg value "$USER_ID" '$value | @uri')"

  curl -fsS \
    "${curl_headers[@]}" \
    "${KAGENT_URL%/}/api/memories?agent_name=${encoded_agent}&user_id=${encoded_user}" \
    | jq -r '.[]? | "- " + .content'
}

delete_memories() {
  local encoded_agent encoded_user
  encoded_agent="$(jq -rn --arg value "$AGENT_NAME" '$value | @uri')"
  encoded_user="$(jq -rn --arg value "$USER_ID" '$value | @uri')"

  curl -fsS -X DELETE \
    "${curl_headers[@]}" \
    "${KAGENT_URL%/}/api/memories?agent_name=${encoded_agent}&user_id=${encoded_user}" \
    >/dev/null
}

require curl
require jq

first_context_id="$(new_id)"
second_context_id="$(new_id)"
tmp_dir="$(mktemp -d)"
trap 'rm -rf "$tmp_dir"' EXIT

echo "Kagent URL: ${KAGENT_URL%/}"
echo "Agent: ${NAMESPACE}/${AGENT_NAME}"
echo "User ID: ${USER_ID}"
echo "First context: ${first_context_id}"
echo "Second context: ${second_context_id}"

if [[ "$RESET_MEMORY" == "true" ]]; then
  echo
  echo "Resetting existing memories for this demo agent/user."
  delete_memories
fi

echo
echo "Memory before turn 1:"
before="$(list_memories)"
if [[ -z "$before" ]]; then
  echo "- no memories found for this agent/user"
else
  echo "$before"
fi

turn1_output="$tmp_dir/turn1.sse"
send_message \
  "Remember this exact fact in long-term memory: In the memory showcase, my release codename is blue-sunrise." \
  "$first_context_id" \
  "$turn1_output"

echo
echo "Turn 1 response text:"
turn1_text="$(extract_text "$turn1_output")"
echo "$turn1_text"

echo
echo "Memory after turn 1:"
after="$(list_memories)"
echo "$after"
if ! grep -qi "$DEMO_FACT" <<<"$after"; then
  echo "Expected saved memory containing ${DEMO_FACT}, but it was not found." >&2
  exit 1
fi

turn2_output="$tmp_dir/turn2.sse"
send_message \
  "What is my release codename for the memory showcase? Use memory before answering." \
  "$second_context_id" \
  "$turn2_output"

echo
echo "Turn 2 response text:"
turn2_text="$(extract_text "$turn2_output")"
echo "$turn2_text"
if ! grep -qi "$DEMO_FACT" <<<"$turn2_text"; then
  echo "Expected second response to include ${DEMO_FACT}, but it did not." >&2
  exit 1
fi

echo
echo "Success: the second response used memory and included ${DEMO_FACT}."
