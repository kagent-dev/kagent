#!/usr/bin/env bash

# Runs Weaver live-check as an OTLP listener for the telemetry the e2e suite
# replays (KAGENT_E2E_LIVE_CHECK_ENDPOINT), and summarizes its report.
#
#   telemetry/live-check.sh start   # listens on 127.0.0.1:4319
#   telemetry/live-check.sh stop    # writes telemetry/conformance/report/live_check.json

set -o errexit -o nounset -o pipefail

ROOT="$(git rev-parse --show-toplevel)"
cd "${ROOT}"

GRPC_PORT="${LIVE_CHECK_GRPC_PORT:-4319}"
ADMIN_PORT="${LIVE_CHECK_ADMIN_PORT:-4320}"
REPORT_DIR=telemetry/conformance/report

start() {
  rm -rf "${REPORT_DIR}"
  mkdir -p "${REPORT_DIR}"
  WEAVER_CONTAINER_ARGS="${WEAVER_CONTAINER_ARGS:-} -p 127.0.0.1:${GRPC_PORT}:${GRPC_PORT} -p 127.0.0.1:${ADMIN_PORT}:${ADMIN_PORT}" \
    nohup telemetry/weaver.sh registry live-check -r telemetry/registry --v2 \
    --config telemetry/conformance/live-check.toml \
    --otlp-grpc-address 0.0.0.0 --otlp-grpc-port "${GRPC_PORT}" --admin-port "${ADMIN_PORT}" \
    --inactivity-timeout 7200 --format json --output "${REPORT_DIR}" \
    >"${REPORT_DIR}/live-check.log" 2>&1 &
  for _ in $(seq 1 300); do
    if curl -sf "http://127.0.0.1:${ADMIN_PORT}/health" >/dev/null; then
      echo "live-check listens on 127.0.0.1:${GRPC_PORT}"
      return
    fi
    sleep 1
  done
  echo "FAIL: live-check did not start" >&2
  cat "${REPORT_DIR}/live-check.log" >&2
  exit 1
}

stop() {
  curl -sf -X POST "http://127.0.0.1:${ADMIN_PORT}/stop" >/dev/null
  for _ in $(seq 1 60); do
    curl -sf "http://127.0.0.1:${ADMIN_PORT}/health" >/dev/null || break
    sleep 1
  done
  local report="${REPORT_DIR}/live_check.json"
  if [[ ! -s "${report}" ]]; then
    echo "FAIL: no live-check report" >&2
    cat "${REPORT_DIR}/live-check.log" >&2
    exit 1
  fi
  jq -r '
    "samples: \(.statistics.total_entities), advice: \(.statistics.advice_level_counts)",
    ([.. | objects | select(has("all_advice")) | .all_advice[] | select(.level == "violation")]
      | group_by(.message) | sort_by(-length)[]
      | "violation x\(length): \(.[0].message) (\(map(.signal_name) | unique | join(", ")))")
  ' "${report}"
}

case "${1:-}" in
  start) start ;;
  stop) stop ;;
  *) echo "usage: $0 start|stop" >&2; exit 2 ;;
esac
