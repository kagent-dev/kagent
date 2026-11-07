#!/usr/bin/env bash
set -euo pipefail

# Run from any directory against the disposable E2E installation. Reuse its
# controller identity; private certificate material never enters test output.
cd "$(dirname "${BASH_SOURCE[0]}")/../../.."
: "${KAGENT_E2E_RUNTIME_IMAGE:?set the digest-pinned Go ADK image}"
checkpoint_tmp=$(mktemp -d)
chmod 700 "$checkpoint_tmp"
forward_pids=()
cleanup() {
  for pid in "${forward_pids[@]:-}"; do
    if [[ -n "$pid" ]]; then
      kill "$pid" 2>/dev/null || true
      wait "$pid" 2>/dev/null || true
    fi
  done
  rm -rf "$checkpoint_tmp"
}
trap cleanup EXIT
kubectl -n kagent exec deployment/kagent-controller -- \
  cat /run/substrate-servicedns/trust-bundle.pem > "$checkpoint_tmp/ca.pem"
kubectl -n kagent exec deployment/kagent-controller -- \
  cat /run/substrate-podidentity/credential-bundle.pem > "$checkpoint_tmp/client.pem"
chmod 600 "$checkpoint_tmp/client.pem"
forward_port() {
  local service=$1 remote_port=$2 forward_pid log
  log="$checkpoint_tmp/$service-forward.log"
  kubectl -n ate-system port-forward "service/$service" ":$remote_port" > "$log" 2>&1 &
  forward_pid=$!
  forward_pids+=("$forward_pid")
  checkpoint_forwarded_port=
  for _ in $(seq 1 100); do
    checkpoint_forwarded_port=$(awk '$1 == "Forwarding" && $3 ~ /^127[.]0[.]0[.]1:/ { split($3, address, ":"); print address[2]; exit }' "$log")
    if [[ -n "$checkpoint_forwarded_port" ]]; then return; fi
    if ! kill -0 "$forward_pid" 2>/dev/null; then
      cat "$log" >&2
      return 1
    fi
    sleep 0.1
  done
  echo "Timed out forwarding $service" >&2
  return 1
}
forward_port api 443
api_port=$checkpoint_forwarded_port
forward_port rustfs 9000
object_store_port=$checkpoint_forwarded_port
# The disposable Substrate installation provides literal RustFS credentials.
# Keep them in private files rather than command arguments or test output.
kubectl -n ate-system get deployment/rustfs -o json > "$checkpoint_tmp/object-store.json"
jq -n --slurpfile store "$checkpoint_tmp/object-store.json" \
  --arg endpoint "127.0.0.1:$api_port" --arg ca "$checkpoint_tmp/ca.pem" \
  --arg objectStoreEndpoint "http://127.0.0.1:$object_store_port" \
  --arg certificate "$checkpoint_tmp/client.pem" --arg image "$KAGENT_E2E_RUNTIME_IMAGE" \
  '{endpoint: $endpoint, serverName: "api.ate-system.svc", caFile: $ca,
    clientCertFile: $certificate, atespace: "kagent", image: $image,
    objectStoreEndpoint: $objectStoreEndpoint,
    objectStoreAccessKey: ([$store[0].spec.template.spec.containers[].env[] | select(.name == "RUSTFS_ACCESS_KEY").value][0] // error("missing RustFS access key")),
    objectStoreSecretKey: ([$store[0].spec.template.spec.containers[].env[] | select(.name == "RUSTFS_SECRET_KEY").value][0] // error("missing RustFS secret key"))}' \
  > "$checkpoint_tmp/config.json"
chmod 600 "$checkpoint_tmp/config.json"
KAGENT_E2E_CHECKPOINT_SUBSTRATE_CONFIG="$checkpoint_tmp/config.json" \
  go test -race ./core/internal/service/checkpoint \
    -run '^TestCheckpointRecoveryWithSubstrate$' -v -count=1 -timeout=10m
