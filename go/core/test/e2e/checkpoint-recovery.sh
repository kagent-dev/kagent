#!/usr/bin/env bash
set -euo pipefail

# Run from any directory against the disposable E2E installation. Reuse its
# controller identity; private certificate material never enters test output.
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
: "${KAGENT_E2E_RUNTIME_IMAGE:?set the digest-pinned Go ADK image}"
checkpoint_tmp=$(mktemp -d)
chmod 700 "$checkpoint_tmp"
forward_pid=
cleanup() {
  if [[ -n "$forward_pid" ]]; then
    kill "$forward_pid" 2>/dev/null || true
    wait "$forward_pid" 2>/dev/null || true
  fi
  rm -rf "$checkpoint_tmp"
}
trap cleanup EXIT
kubectl -n kagent exec deployment/kagent-controller -- \
  cat /run/substrate-servicedns/trust-bundle.pem > "$checkpoint_tmp/ca.pem"
kubectl -n kagent exec deployment/kagent-controller -- \
  cat /run/substrate-podidentity/credential-bundle.pem > "$checkpoint_tmp/client.pem"
chmod 600 "$checkpoint_tmp/client.pem"
kubectl -n ate-system port-forward service/api :443 \
  > "$checkpoint_tmp/forward.log" 2>&1 &
forward_pid=$!
port=
for _ in $(seq 1 100); do
  port=$(awk '$1 == "Forwarding" && $3 ~ /^127[.]0[.]0[.]1:/ { split($3, address, ":"); print address[2]; exit }' "$checkpoint_tmp/forward.log")
  if [[ -n "$port" ]]; then break; fi
  if ! kill -0 "$forward_pid" 2>/dev/null; then
    cat "$checkpoint_tmp/forward.log" >&2
    exit 1
  fi
  sleep 0.1
done
test -n "$port"
jq -n --arg endpoint "127.0.0.1:$port" --arg ca "$checkpoint_tmp/ca.pem" \
  --arg certificate "$checkpoint_tmp/client.pem" --arg image "$KAGENT_E2E_RUNTIME_IMAGE" \
  '{endpoint: $endpoint, serverName: "api.ate-system.svc", caFile: $ca,
    clientCertFile: $certificate, atespace: "kagent", image: $image}' \
  > "$checkpoint_tmp/config.json"
KAGENT_E2E_CHECKPOINT_SUBSTRATE_CONFIG="$checkpoint_tmp/config.json" \
  go test -race ./core/internal/service/checkpoint \
    -run '^TestCheckpointRecoveryWithSubstrate$' -v -count=1 -timeout=10m
