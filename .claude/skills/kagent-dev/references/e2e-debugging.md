# E2E Test Debugging Guide

Comprehensive guide for debugging kagent end-to-end test failures.

## Quick Diagnosis Flowchart

```
E2E test fails
    ↓
Check error message
    ├─ "connection refused" → KAGENT_URL issue (see Setting KAGENT_URL)
    ├─ "context deadline exceeded" → Timeout (see Timeout Issues)
    ├─ "unexpected field" → CRD mismatch (see CRD Issues)
    ├─ "failed to create agent" → Validation error (see Validation Issues)
    └─ Other → Follow debugging steps below
```

## Setting KAGENT_URL Correctly

The most common E2E test failure is incorrect KAGENT_URL.

### The Correct Way

```bash
export KAGENT_URL="http://$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'):8083"
```

**Why this works:**
- Gets the LoadBalancer IP assigned by MetalLB (in Kind)
- Uses port 8083 (controller's port)
- Tests can reach the service from outside the cluster

### Common Mistakes

**❌ Using ClusterIP:**
```bash
# This doesn't work - ClusterIP is internal
export KAGENT_URL="http://$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.spec.clusterIP}'):8083"
```

**❌ Using localhost:**
```bash
# This doesn't work - controller is in cluster, not on host
export KAGENT_URL="http://localhost:8083"
```

**❌ Forgetting port:**
```bash
# Missing :8083
export KAGENT_URL="http://$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"
```

**❌ Not setting it:**
```bash
# Tests will try to autodiscover and likely fail
make -C go e2e  # Without KAGENT_URL set
```

### Verifying KAGENT_URL

```bash
# 1. Set it
export KAGENT_URL="http://$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'):8083"

# 2. Verify it's set correctly
echo $KAGENT_URL
# Should output something like: http://172.18.0.100:8083

# 3. Test connectivity
curl -v $KAGENT_URL/healthz
# Should return 200 OK
```

### CI Environment

In GitHub Actions, the setup is done in `.github/workflows/ci.yaml`:

```yaml
- name: Run e2e tests
  working-directory: go/core
  run: |
    HOST_IP=$(docker network inspect kind -f '{{range .IPAM.Config}}{{if .Gateway}}{{.Gateway}}{{"\n"}}{{end}}{{end}}' | grep -E '^[0-9]+\.' | head -1)
    export KAGENT_LOCAL_HOST=$HOST_IP
    export KAGENT_URL="http://$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'):8083"
    go test -v github.com/kagent-dev/kagent/go/core/test/e2e -failfast -shuffle=on
```

---

## Timeout Issues

**Symptom:** Test fails with `context deadline exceeded`

### Common Causes

1. **Agent pod not starting**
   ```bash
   # Check pod status
   kubectl get pods -n kagent | grep test-agent

   # Check pod events
   kubectl describe pod -n kagent <pod-name>
   ```

2. **Controller not processing**
   ```bash
   # Check controller logs
   kubectl logs -n kagent deployment/kagent-controller | tail -50

   # Check if controller is running
   kubectl get pods -n kagent -l app=kagent-controller
   ```

3. **MCP server not responding**
   ```bash
   # Check MCP server pods
   kubectl get pods -n kagent -l app=kagent-tools

   # Check MCP server logs
   kubectl logs -n kagent deployment/kagent-tools
   ```

### Increasing Timeout

If test legitimately needs more time:

```go
// In test file
require.Eventually(t, func() bool {
    // ... condition
}, 120*time.Second, 2*time.Second) // Increased from 60s to 120s
```

### Fast Failure for Debugging

Use `-failfast` to stop on first failure:

```bash
KAGENT_URL="..." go test -v github.com/kagent-dev/kagent/go/core/test/e2e -failfast
```

---

## CRD Issues

**Symptom:** `unexpected field` or `unknown field` in error

### Cause

CRD manifests in cluster don't match code.

### Fix

1. **Regenerate manifests:**
   ```bash
   make -C go generate
   cp go/api/config/crd/bases/*.yaml helm/kagent-crds/templates/
   ```

2. **Redeploy CRDs:**
   ```bash
   helm upgrade kagent-crds helm/kagent-crds --namespace kagent
   ```

3. **Verify CRD is updated:**
   ```bash
   kubectl get crd agents.kagent.dev -o yaml | grep -A 5 "newField"
   ```

### Prevention

Always run `make -C go generate` after modifying CRD types, and commit the generated files.

---

## Validation Issues

**Symptom:** `failed to create agent` with validation error

### Debugging

```bash
# Get validation error details
kubectl get events -n kagent | grep -i invalid

# Try creating agent manually to see validation error
kubectl apply -f - <<EOF
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: test-agent
  namespace: kagent
spec:
  # ... your spec
EOF
```

### Common Validation Errors

**Required field missing:**
```
Error: spec.declarative.modelConfig: Required value
```

**Fix:** Ensure all required fields are set in test agent spec

**Invalid enum value:**
```
Error: spec.declarative.logLevel: Unsupported value: "invalid": supported values: "debug", "info", "warn", "error"
```

**Fix:** Use valid enum values

**Pattern validation:**
```
Error: spec.declarative.name: Invalid value: "Invalid-Name": must match pattern ^[a-z0-9-]+$
```

**Fix:** Ensure value matches pattern in kubebuilder marker

---

## Reproducing Locally (Without Cluster)

For tight iteration without cluster overhead, follow `go/core/test/e2e/README.md`:

### Step 1: Extract Agent Config

**Option A: From cluster**
```bash
TEMP_DIR=$(mktemp -d)
kubectl exec -n kagent -ti deploy/test-agent -c kagent -- tar c -C / config | tar -x -C ${TEMP_DIR}
AGENT_CONFIG_DIR=${TEMP_DIR}/config
```

**Option B: Generate**
```bash
cd go/core
go run hack/makeagentconfig/main.go
AGENT_CONFIG_DIR=$PWD
```

### Step 2: Start Mock LLM Server

```bash
# Use response JSON from test
cd go/core
go run hack/mockllm/main.go invoke_api_test_response.json &
```

### Step 3: Run Agent Locally

```bash
export OPENAI_API_KEY=dummykey
cd python
uv run kagent-adk test --filepath ${AGENT_CONFIG_DIR} --task "Your test prompt"
```

**Benefits:**
- No cluster noise
- Fast iteration
- Easy to debug with print statements
- Can step through with debugger

---

## Specific Test Failures

### TestE2EInvokeInlineAgent Fails

**Common issues:**
1. Model config not found
2. API key not set
3. Agent pod crashlooping
4. Mock LLM server unreachable from pods

**Debug:**
```bash
# Check model config exists
kubectl get modelconfig -n kagent test-model-config

# Check API key secret
kubectl get secret -n kagent -l app=kagent

# Check agent pod
kubectl get pods -n kagent | grep test-agent
kubectl logs -n kagent <pod-name>

# Check mock LLM server connectivity
# From inside a pod, try to reach KAGENT_LOCAL_HOST
kubectl run -it --rm debug --image=curlimages/curl --restart=Never -- \
  curl -v http://$KAGENT_LOCAL_HOST:8090/v1/chat/completions
```

**Mock LLM server unreachable:**

The test starts a mock LLM server on the test host, and agent pods need to reach it via `KAGENT_LOCAL_HOST`.

**Symptoms:**
- Test hangs or times out
- Agent logs show "connection refused" to mock server

**Fix:**
```bash
# Verify KAGENT_LOCAL_HOST is set correctly
echo $KAGENT_LOCAL_HOST

# On Linux (should be Docker bridge IP)
export KAGENT_LOCAL_HOST=172.17.0.1

# On macOS (should be host.docker.internal)
export KAGENT_LOCAL_HOST=host.docker.internal

# Test connectivity from a Kind pod
docker exec -it kagent-control-plane ping $KAGENT_LOCAL_HOST
```

### TestE2EInvokeInlineAgentWithStreaming Fails

**Common issues:**
1. SSE connection issues
2. Timeout too short for streaming
3. Response buffering
4. Empty streaming response (agent not fully warmed up)

**Debug:**
```bash
# Check controller SSE endpoint
curl -N -H "Accept: text/event-stream" "$KAGENT_URL/api/agents/test-agent/stream"

# Check for buffering issues in logs
kubectl logs -n kagent deployment/kagent-controller | grep -i stream
```

**Empty streaming response:**

**Symptoms:**
- Stream connects but returns 0 events
- Error: "response does not contain expected text"

**Cause:** Agent pod not fully ready yet (still warming up)

**Fix:** Tests have retry logic for this. If it still fails after retries:
```bash
# Check if agent pod is crash-looping
kubectl get pods -n kagent | grep test-agent

# Check agent logs for errors
kubectl logs -n kagent <pod-name>

# Verify mock LLM server is reachable
# (See mock LLM server debugging above)
```

### TestE2EMCPEndpoint* Fails

**Common issues:**
1. MCP server not running
2. Tools not discovered
3. MCP protocol errors
4. "All connection attempts failed" from agent pod

**Debug:**
```bash
# Check MCP server
kubectl get remotemcpservers.kagent.dev -n kagent
kubectl get pods -n kagent -l app=kagent-tools

# Check tools are registered
curl "$KAGENT_URL/api/mcp/tools"

# Check MCP server logs
kubectl logs -n kagent deployment/kagent-tools
```

**"All connection attempts failed":**

**Cause:** MCP server pod isn't ready or its service isn't resolvable from agent pod

**Fix:**
```bash
# Check MCPServer status
kubectl get remotemcpserver -n kagent kagent-tool-server -o yaml

# Check service exists
kubectl get svc -n kagent kagent-tools

# Check endpoints are ready
kubectl get endpoints -n kagent kagent-tools

# Verify from agent pod
kubectl exec -n kagent <agent-pod> -- curl -v http://kagent-tools.kagent:8084/mcp
```

### TestE2EInvokeExternalAgent Fails

**Symptoms:**
- Test fails with "agent not found"
- Agent pod doesn't exist

**Cause:** The `kebab-agent` must be pre-deployed before running tests. This agent is not created by the test.

**Fix:**
```bash
# Deploy test agents and resources
make push-test-agent

# Verify kebab-agent exists
kubectl get agent -n kagent kebab-agent
```

### TestE2EInvokeOpenAIAgent or TestE2EInvokeCrewAIAgent Fails

**Symptoms:**
- ImagePullBackOff
- ErrImagePull

**Cause:** BYO (Bring Your Own) agent images must be built and pushed to local registry before running these tests.

**Fix:**
```bash
# Build and push test agent images
make push-test-agent

# Verify images exist in local registry
curl http://localhost:5001/v2/_catalog
# Should list: basic-openai, poem-flow, kebab

# Check specific image
curl http://localhost:5001/v2/basic-openai/tags/list
```

---

## Common Error Patterns

### "agent.kagent.dev/test-agent condition met" hangs

**Cause:** Agent never becomes Ready

**Debug:**
```bash
# Check agent status
kubectl get agent test-agent -n kagent -o jsonpath='{.status.conditions}' | jq

# Common conditions to check:
# - Accepted: False → CRD validation or template error
# - Ready: False → Pod not starting or failing health checks
```

### "Failed to invoke agent" in test

**Cause:** Agent invocation fails (500 error from controller)

**Debug:**
```bash
# Check controller logs for error
kubectl logs -n kagent deployment/kagent-controller | grep -A 10 "invoke"

# Check agent logs for error
kubectl logs -n kagent deployment/test-agent

# Try manual invocation
curl -X POST "$KAGENT_URL/api/agents/test-agent/invoke" \
  -H "Content-Type: application/json" \
  -d '{"message":"test"}'
```

### Test passes locally but fails in CI

**Common causes:**
1. **Timing differences** - CI may be slower
   - Solution: Increase timeouts in CI or use `require.Eventually` with longer duration

2. **Resource constraints** - CI has less memory/CPU
   - Solution: Check pod resource requests/limits

3. **Network configuration** - Different network setup
   - Solution: Verify KAGENT_URL setup in CI matches local

**Debug CI failures:**
```bash
# Get CI logs
gh pr checks <pr-number>
gh run view <run-id> --job <job-id> --log | grep -A 20 "FAIL"
```

---

## Advanced Debugging Techniques

### Enable Verbose Test Output

```bash
KAGENT_URL="..." go test -v -run TestE2ESpecificTest github.com/kagent-dev/kagent/go/core/test/e2e
```

### Run Single Test

```bash
KAGENT_URL="..." go test -v -run TestE2EInvokeInlineAgent/sync_invocation github.com/kagent-dev/kagent/go/core/test/e2e
```

### Run With Race Detector (for test code only)

```bash
# Note: E2E tests test the deployed system, so race detector only catches issues in test code itself
KAGENT_URL="..." go test -race -v github.com/kagent-dev/kagent/go/core/test/e2e
```

### Kubectl Port-Forward for Debugging

```bash
# Port-forward controller for easier access
kubectl port-forward -n kagent svc/kagent-controller 8083:8083

# Now can use localhost
export KAGENT_URL="http://localhost:8083"
make -C go e2e
```

### Check Controller Metrics

```bash
# Controller exposes metrics on :8080
kubectl port-forward -n kagent deployment/kagent-controller 8080:8080

# Check metrics
curl http://localhost:8080/metrics | grep kagent
```

### Dump All Resources

```bash
# Useful for post-mortem debugging
kubectl get all,agents,modelconfigs,remotemcpservers -n kagent -o yaml > kagent-dump.yaml
```

---

## Prevention Checklist

Before running E2E tests, verify:

- [ ] Kind cluster is running: `kind get clusters | grep kagent`
- [ ] Kagent is deployed: `kubectl get pods -n kagent`
- [ ] KAGENT_URL is set correctly: `echo $KAGENT_URL`
- [ ] CRDs are up to date: `make -C go generate` run recently
- [ ] Images are built: `make build` completed
- [ ] No pending changes that need helm upgrade

---

## Emergency Debugging Commands

When a test fails and you need quick answers:

```bash
# 1. What's the error?
KAGENT_URL="..." go test -v github.com/kagent-dev/kagent/go/core/test/e2e -run <FailingTest> 2>&1 | tail -50

# 2. Is the cluster healthy?
kubectl get pods -n kagent
kubectl get agents.kagent.dev -n kagent

# 3. What's the controller doing?
kubectl logs -n kagent deployment/kagent-controller --tail=100

# 4. Can I reach the controller?
curl -v $KAGENT_URL/healthz

# 5. What resources were created?
kubectl get all -n kagent | grep test-agent

# 6. What's the agent status?
kubectl get agent test-agent -n kagent -o yaml | grep -A 20 status
```

---

## CI-Specific Debugging

### Getting CI Logs

```bash
# List recent workflow runs
gh run list --repo kagent-dev/kagent

# View specific run
gh run view <run-id>

# Download logs
gh run download <run-id>

# View specific job log
gh run view <run-id> --job <job-id> --log
```

### Common CI Failures

**test-e2e matrix (sqlite/postgres):**
- If only one database fails, it's likely database-specific
- If both fail, it's a general test issue

**Flaky tests:**
- Tests that pass locally but fail in CI sometimes
- Usually timing-related
- Solution: Use `require.Eventually` with appropriate timeout/interval

---

## Getting Help

If you're stuck after trying the above:

1. **Collect diagnostic info:**
   ```bash
   # Save to file for sharing
   {
     echo "=== KAGENT_URL ==="
     echo $KAGENT_URL
     echo "=== Pods ==="
     kubectl get pods -n kagent
     echo "=== Agents ==="
     kubectl get agents.kagent.dev -n kagent
     echo "=== Controller Logs ==="
     kubectl logs -n kagent deployment/kagent-controller --tail=100
     echo "=== Test Output ==="
     KAGENT_URL="..." go test -v github.com/kagent-dev/kagent/go/core/test/e2e -run <FailingTest> 2>&1
   } > debug.log
   ```

2. **Check similar issues:** `gh issue list --repo kagent-dev/kagent --label e2e`

3. **Ask in PR or issue** with debug.log attached
