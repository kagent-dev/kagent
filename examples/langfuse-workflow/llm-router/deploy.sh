#!/usr/bin/env bash
# Deploy the llm-router declarative agent to the k3s cluster over ssh.
# Usage: ./deploy.sh [apply|delete]
set -euo pipefail

here="$(cd "$(dirname "$0")" && pwd)"
action="${1:-apply}"
ssh_k3s=(ssh -o BatchMode=yes -o ConnectTimeout=10 k3s)

case "$action" in
apply)
  # The controller validates the document on reconcile; a bad one is reported on the template.
  "${ssh_k3s[@]}" 'kubectl create configmap llm-router-workflow -n kagent --from-file=agent-system.yaml=/dev/stdin --dry-run=client -o yaml | kubectl apply -f -' <"$here/agent-system.yaml"
  "${ssh_k3s[@]}" 'kubectl apply -f -' <"$here/manifests.yaml"
  "${ssh_k3s[@]}" 'kubectl wait --for=condition=Ready agent.api.kagent.dev/llm-router -n kagent --timeout=180s'
  ;;
delete)
  "${ssh_k3s[@]}" 'kubectl delete -n kagent agent.api.kagent.dev/llm-router agenttemplate.api.kagent.dev/llm-router harness.api.kagent.dev/llm-router configmap/llm-router-workflow --ignore-not-found'
  ;;
*)
  echo "usage: $0 [apply|delete]" >&2
  exit 2
  ;;
esac
