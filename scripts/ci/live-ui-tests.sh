#!/usr/bin/env bash
# Runs the live Playwright specs against the deployed UI image.
# Waits for the toolchain install that the workflow starts in the background.
set -euo pipefail

status="$RUNNER_TEMP/live-ui-setup.status"
for _ in $(seq 1 120); do test -s "$status" && break; sleep 5; done
if [ "$(cat "$status" 2>/dev/null)" != 0 ]; then
  echo "::error::Live UI toolchain install failed or did not finish"
  cat "$RUNNER_TEMP/live-ui-setup.log"
  exit 1
fi

kubectl -n kagent rollout status deploy/kagent-ui --timeout=2m
UI_IP=""
for _ in $(seq 1 30); do
  UI_IP="$(kubectl get svc -n kagent kagent-ui -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"
  [ -n "$UI_IP" ] && break
  sleep 2
done
test -n "$UI_IP"
export KAGENT_E2E_UI_LOOP_LIVE_URL="http://${UI_IP}:8080"
echo "KAGENT_E2E_UI_LOOP_LIVE_URL: $KAGENT_E2E_UI_LOOP_LIVE_URL"

cd "$(dirname "$0")/../../ui"
# agent-lifecycle and agent-template-skills need a model that answers.
yarn test:pw:live --workers=1 --retries=1 --global-timeout=600000 \
  --output "$RUNNER_TEMP/e2e-logs/ui" \
  live/pages live/substrate live/schedules
