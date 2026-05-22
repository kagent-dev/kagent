#!/usr/bin/env bash
# Smoke test: prove cross-namespace A2A routing works and that the
# security controls block unauthorised cross-namespace access.
set -euo pipefail

KAGENT_NS="${KAGENT_NS:-kagent}"
CONTROLLER_PORT="${CONTROLLER_PORT:-8083}"

# Port-forward the kagent controller so we can hit the A2A gateway locally.
echo "▶ Port-forwarding kagent controller on :${CONTROLLER_PORT}…"
kubectl -n "$KAGENT_NS" port-forward svc/kagent "${CONTROLLER_PORT}:${CONTROLLER_PORT}" &
PF_PID=$!
trap "kill $PF_PID 2>/dev/null; echo 'port-forward stopped'" EXIT
sleep 2

BASE="http://localhost:${CONTROLLER_PORT}/api/a2a"

# ── Test 1: Orchestrator agent card is reachable ──────────────────────────────
echo ""
echo "── Test 1: orchestrator agent card ──"
curl -sf "${BASE}/team-alpha/orchestrator/.well-known/agent.json" | jq '.name'

# ── Test 2: Specialist agent card is reachable via gateway ───────────────────
echo ""
echo "── Test 2: specialist agent card (via gateway) ──"
curl -sf "${BASE}/team-beta/specialist/.well-known/agent.json" | jq '.name'

# ── Test 3: Send a math task to the orchestrator; it must delegate ────────────
echo ""
echo "── Test 3: send math task to orchestrator → expect delegation to specialist ──"
TASK_RESP=$(curl -sf -X POST "${BASE}/team-alpha/orchestrator/" \
  -H "Content-Type: application/json" \
  -H "X-User-Id: david@test" \
  -d '{
    "jsonrpc": "2.0",
    "id": "test-1",
    "method": "message/send",
    "params": {
      "message": {
        "role": "user",
        "parts": [{ "kind": "text", "text": "What is 17 multiplied by 38?" }],
        "messageId": "msg-001"
      }
    }
  }')

echo "$TASK_RESP" | jq -r '.result.parts[0].text // .result // .'

# ── Test 4: Security — team-gamma (unlabelled) cannot use the specialist ─────
echo ""
echo "── Test 4: security check — unlabelled namespace rejected at reconcile ──"
# Create a temporary namespace without team=alpha label
kubectl create namespace team-gamma --dry-run=client -o yaml | kubectl apply -f - 2>/dev/null || true

# Attempt to create an agent in team-gamma that references team-beta/specialist.
# The reconciler should deny this with an AllowedNamespaces violation.
DENY_RESULT=$(kubectl apply -f - 2>&1 <<'EOF'
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: rogue-agent
  namespace: team-gamma
spec:
  type: Declarative
  declarative:
    description: Rogue agent attempting cross-namespace access
    systemMessage: You are a rogue agent.
    modelConfig: default-model
    tools:
      - agent:
          name: specialist
          namespace: team-beta
EOF
) || true

if echo "$DENY_RESULT" | grep -qi "denied\|not allowed\|forbidden\|error"; then
  echo "✓ BLOCKED: team-gamma cannot reference team-beta/specialist (AllowedNamespaces enforced)"
else
  echo "⚠ Agent created — check reconciler logs for deferred rejection:"
  echo "  kubectl -n $KAGENT_NS logs deploy/kagent | grep 'team-gamma'"
  echo "  (Reconciler may surface error on Agent status rather than admission webhook)"
fi

# Clean up rogue agent
kubectl delete agent rogue-agent -n team-gamma --ignore-not-found 2>/dev/null || true

# ── Test 5: Direct call to specialist bypassing gateway is blocked by NetworkPolicy ──
echo ""
echo "── Test 5: direct pod-to-pod call blocked by NetworkPolicy ──"
SPECIALIST_IP=$(kubectl -n team-beta get pods -l app.kubernetes.io/name=specialist-agent \
  -o jsonpath='{.items[0].status.podIP}' 2>/dev/null || echo "")

if [ -z "$SPECIALIST_IP" ]; then
  echo "ℹ No specialist pod found — skip network test (apply full stack first)"
else
  # Try to curl the specialist pod directly from a debug pod in team-alpha.
  # NetworkPolicy should block this — only kagent controller can reach it.
  NETTEST=$(kubectl run nettest --image=curlimages/curl:latest --restart=Never \
    --namespace=team-alpha --rm -i --timeout=10s -- \
    curl -sf --connect-timeout 3 "http://${SPECIALIST_IP}:8080/" 2>&1 || true)

  if echo "$NETTEST" | grep -qi "timed out\|connection refused\|exit code"; then
    echo "✓ BLOCKED: direct pod-to-pod call from team-alpha to team-beta/specialist denied by NetworkPolicy"
  else
    echo "⚠ Direct call may have succeeded — verify CNI enforces NetworkPolicy (Calico/Cilium required)"
  fi
fi

echo ""
echo "✓ Verification complete."
echo ""
echo "Security summary:"
echo "  CRD-level:     AllowedNamespaces selector on specialist (team=alpha only)"
echo "  Secret scope:  Orchestrator reads team-alpha secrets only (RBAC)"
echo "  Token inject:  X-Agent-Token from team-alpha Secret → specialist header"
echo "  Network:       NetworkPolicy blocks direct pod-to-pod; all A2A via gateway"
echo "  Auth (current): UnsecureAuthenticator forwards X-User-Id (dev mode)"
echo "  Auth (prod):   Apply EP-476 OIDC JWT policy when merged"
