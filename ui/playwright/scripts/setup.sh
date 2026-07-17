#!/usr/bin/env bash
set -euo pipefail

# Set up a kind cluster running a real kagent build for the Playwright UI suite.
#
# Builds the local images and installs kagent via the repo's existing make targets
# (see ../README.md) — no new commands. After this finishes, run:
#
#     yarn run test:e2e        # (or: npm run test:e2e)
#
# from the ui/ directory. Playwright port-forwards the controller (playwright/setup.ts)
# and points its proxy at it; the proxy mocks only the chat A2A stream.
#
# Env:
#   OPENAI_API_KEY   required by `make helm-install` (chat is mocked, so a dummy
#                    value is fine — CI uses "fake").
#   KUBE_CONTEXT     kube context (default: kind-kagent)
#   KUBE_NAMESPACE   namespace (default: kagent)

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# ui/playwright/scripts -> repo root is three levels up.
REPO_ROOT="$(cd "${SCRIPT_DIR}/../../.." && pwd)"
KUBE_CONTEXT="${KUBE_CONTEXT:-kind-kagent}"
NAMESPACE="${KUBE_NAMESPACE:-kagent}"

cd "${REPO_ROOT}"

echo "=== E2E UI: cluster setup ==="
echo "Repo root: ${REPO_ROOT}"
echo "Context:   ${KUBE_CONTEXT}"
echo "Namespace: ${NAMESPACE}"

# Skip if the controller is already up (fast re-runs).
if kubectl get deployment kagent-controller -n "${NAMESPACE}" --context "${KUBE_CONTEXT}" &>/dev/null; then
  ready="$(kubectl get deployment kagent-controller -n "${NAMESPACE}" --context "${KUBE_CONTEXT}" \
    -o jsonpath='{.status.readyReplicas}' 2>/dev/null || echo 0)"
  if [ "${ready:-0}" -ge 1 ]; then
    echo "kagent-controller already running — skipping cluster setup."
    exit 0
  fi
fi

# helm-install fails fast without a provider key; chat is mocked so any value works.
export OPENAI_API_KEY="${OPENAI_API_KEY:-fake}"

# Create the cluster (idempotent: setup-kind.sh reuses an existing one) then build
# the local images and install kagent.
make create-kind-cluster
make helm-install

echo "Waiting for kagent-controller to become available..."
kubectl rollout status deployment/kagent-controller \
  -n "${NAMESPACE}" --context "${KUBE_CONTEXT}" --timeout=300s

echo ""
echo "=== E2E UI: cluster ready — run 'yarn run test:e2e' from ui/ ==="
