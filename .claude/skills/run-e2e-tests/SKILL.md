---
name: run-e2e-tests
description: Run and debug kagent end-to-end tests. Use when the user asks to run e2e tests, debug e2e test failures, or understand the e2e test setup.
allowed-tools: Bash, Read, Grep, Glob
argument-hint: "[test-name-pattern]"
---

# Running Kagent E2E Tests

## Prerequisites

1. A running Kind cluster with kagent deployed:
   ```bash
   make create-kind-cluster
   make helm-install
   ```

2. Test agent images built and pushed to the local registry (for BYO agent tests):
   - `localhost:5001/basic-openai:latest`
   - `localhost:5001/poem-flow:latest`
   - `kind-registry:5000/kebab-maker:latest` (for skill tests)

3. The `kagent-tool-server` RemoteMCPServer must exist in the `kagent` namespace.

4. The `kebab-agent` must be deployed in the `kagent` namespace (used by `TestE2EInvokeExternalAgent`).

## Environment Variables

Set `KAGENT_URL` using the controller's load balancer IP (no port-forward needed):
```bash
export KAGENT_URL="http://$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'):8083"
```

| Variable | Default | Description |
|----------|---------|-------------|
| `KAGENT_URL` | `http://localhost:8083` | Base URL of the kagent controller. Use the load balancer command above rather than port-forwarding. |
| `KAGENT_LOCAL_HOST` | Auto-detected (`host.docker.internal` on macOS, `172.17.0.1` on Linux) | Host IP accessible from inside Kind pods (for mock LLM server) |
| `SKIP_CLEANUP` | (unset) | If set and test fails, skip deleting test resources for debugging |

## Running Tests

If the user provides a test name pattern via `$ARGUMENTS`, run that specific test. Otherwise run all e2e tests.

All commands must be run from the `go/` directory.

Set `KAGENT_URL` before every test command:
```bash
export KAGENT_URL="http://$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'):8083"
```

Run all e2e tests:
```bash
KAGENT_URL="$KAGENT_URL" go test -v -count=1 ./test/e2e/ -failfast
```

Run a single test (using `$ARGUMENTS` as the pattern):
```bash
KAGENT_URL="$KAGENT_URL" go test -v -count=1 -run "$ARGUMENTS" ./test/e2e/
```

Run with shuffle (to detect ordering dependencies):
```bash
KAGENT_URL="$KAGENT_URL" go test -v -count=1 ./test/e2e/ -failfast -shuffle=on
```

## Test Structure

Tests live in `go/test/e2e/invoke_api_test.go`. Each test:
1. Starts a mock LLM server (via `mockllm`) that listens on the host
2. Creates K8s resources (ModelConfig, Agent, optionally MCPServer)
3. Waits for `condition=Ready` on the Agent CR
4. Polls the A2A endpoint via `waitForEndpoint()` until it returns non-5xx
5. Sends A2A messages (sync and/or streaming) and checks responses
6. Cleans up resources (or logs debug info on failure)

The mock LLM server runs on the test host machine and must be reachable from inside Kind pods. The `KAGENT_LOCAL_HOST` variable controls what IP is used — auto-detected as `172.17.0.1` on Linux and `host.docker.internal` on macOS.

## Debugging Failures

When a test fails, the cleanup function automatically logs:
- Agent CR YAML
- Pod logs for all pods matching the agent
- Pod describe output
- Deployment describe output
- Service describe output

To manually investigate after a failed test (run with `SKIP_CLEANUP=1` to preserve resources):

```bash
SKIP_CLEANUP=1 KAGENT_URL="$KAGENT_URL" go test -v -count=1 -run TestName ./test/e2e/
```

Then inspect:
```bash
kubectl get agents -n kagent
kubectl describe agent <agent-name> -n kagent
kubectl get pods -n kagent -l app.kubernetes.io/managed-by=kagent
kubectl logs -n kagent -l app.kubernetes.io/name=<agent-name>
kubectl get events -n kagent --sort-by=.lastTimestamp
kubectl logs -n kagent deployments/kagent-controller
kubectl get endpoints <agent-name> -n kagent
```

## Common Failure Modes

- **Empty streaming response / "does not contain"**: The stream connected but the agent returned 0 events (not fully warmed up). The retry logic handles this — if it still fails after retries, check if the agent pod is crash-looping or if the mock LLM server is unreachable from inside the cluster.
- **"connection refused" to agent pod**: The agent deployment/service hasn't propagated yet. `waitForEndpoint()` should handle this. If persistent, check `kubectl get endpoints <agent-name> -n kagent`.
- **"serviceaccount not found"**: The controller hasn't finished creating all child resources for the agent. This is a timing issue handled by `waitForEndpoint()`.
- **"All connection attempts failed" (MCP server)**: The MCPServer's pod isn't ready or its service isn't resolvable from the agent pod. Check `kubectl get mcpservers -n kagent` and `kubectl describe mcpserver <name> -n kagent`.
- **Mock LLM server unreachable from pods**: The `KAGENT_LOCAL_HOST` is wrong for your environment. On Linux it defaults to `172.17.0.1` (the docker bridge IP). Set it explicitly if your setup differs.
- **`TestE2EInvokeExternalAgent` fails**: The `kebab-agent` must be pre-deployed. This agent is not created by the test — it must exist beforehand.
- **`TestE2EInvokeOpenAIAgent` or `TestE2EInvokeCrewAIAgent` fails with image pull error**: The BYO agent images must be built and pushed to the local registry (`localhost:5001`) before running these tests.
