# Troubleshooting kagent

## Diagnostic Commands

```bash
# Cluster state
kubectl get agents.kagent.dev -n kagent          # all agents and their status
kubectl get mcpserver -n kagent                   # MCP server resources
kubectl get pods -n kagent                        # pod health
kubectl get events -n kagent --sort-by=.lastTimestamp

# Agent status details
kubectl get agent <name> -n kagent -o yaml        # full status including conditions

# Logs
kubectl logs -n kagent deployment/kagent-controller   # controller logs
kubectl logs -n kagent deployment/kagent-ui            # UI logs
kubectl logs -n kagent <agent-pod-name>                # specific agent logs

# Bug report (collects diagnostics)
kagent bug-report
```

## Common Issues

### Agent not appearing in dashboard

**Symptoms:** Applied agent YAML but it doesn't show in the UI.

**Diagnosis:**
```bash
kubectl get agent <name> -n kagent -o yaml
```
Check the `.status` field for rejection reasons.

**Common causes:**
- Missing or invalid `modelConfig` reference
- Invalid tool reference (MCPServer doesn't exist)
- Namespace mismatch between agent and referenced resources
- CRD version mismatch (using v1alpha1 fields in v1alpha2)

### Agent stuck in not-ready state

**Diagnosis:**
```bash
kubectl get agent <name> -n kagent -o jsonpath='{.status.conditions}' | jq
kubectl describe pod -n kagent -l app.kubernetes.io/name=<name>,app.kubernetes.io/managed-by=kagent
```

**Common causes:**
- Image pull failures (check imagePullSecrets)
- Insufficient resources (CPU/memory limits too low)
- MCP server pod not ready
- LLM API key secret missing or incorrect

### Agent not responding / timing out

**Diagnosis:**
```bash
kubectl logs -n kagent <agent-pod-name>
kubectl logs -n kagent deployment/kagent-controller | grep <agent-name>
```

**Common causes:**
- LLM API rate limiting or key expiration
- MCP tool server crashed or unreachable
- Agent pod OOMKilled (increase memory limits)
- Network policy blocking outbound traffic to LLM provider

### Failed to create MCP session (intermittent)

**Symptoms:** Agent intermittently logs "Failed to create MCP session" — it works sometimes but not always.

**Diagnosis:**
```bash
kubectl get mcpserver <name> -n kagent -o yaml
kubectl get pods -n kagent -l app.kubernetes.io/name=<mcpserver-name>,app.kubernetes.io/managed-by=kagent
kubectl logs -n kagent <mcpserver-pod-name>
kubectl logs -n kagent <agent-pod-name>
```

Check agent pod logs for context around the error — connection refused, timeout, DNS failure, etc.

**Common causes:**

1. **Timeout too short (most common for intermittent failures):** The default MCP session creation timeout may be too short for servers that take time to initialize. Increase the `timeout` field on the MCPServer or RemoteMCPServer resource:
   ```yaml
   # RemoteMCPServer example
   spec:
     url: http://my-mcp-server:3000/sse
     timeout: 60s           # increase from default
     sseReadTimeout: 120s   # for long-running SSE connections
   ```

2. **MCP server pod instability:** Pod restarts, OOMKills, or readiness probe flapping. Check restart count with `kubectl get pods` and previous logs with `kubectl logs --previous`.

3. **Startup race condition:** Agent attempts to connect before the MCP server is fully ready. Ensure proper readiness probes on the MCP server pod.

4. **Namespace mismatch:** MCPServer must be in the same namespace as the Agent.

5. **Missing `apiGroup: kagent.dev`** in the agent's tool reference — required for both MCPServer and RemoteMCPServer kinds.

### MCP tools not available to agent

**Diagnosis:**
```bash
kubectl get mcpserver <name> -n kagent -o yaml
kubectl get pods -n kagent -l app.kubernetes.io/name=<name>,app.kubernetes.io/managed-by=kagent
```

**Common causes:**
- MCPServer resource not in same namespace as Agent
- Tool name mismatch in `toolNames` filter
- MCP server binary not found (wrong `cmd` or `args`)
- Port conflict

### Dashboard not accessible

```bash
# Check UI pod
kubectl get pods -n kagent -l app=kagent-ui

# Manual port-forward
kubectl port-forward -n kagent svc/kagent-ui 8082:8080

# Or use CLI
kagent dashboard
```

### CLI can't connect to controller

```bash
# Verify controller is running
kubectl get pods -n kagent -l app=kagent-controller

# Check service
kubectl get svc -n kagent kagent-controller

# Test connectivity
kubectl port-forward svc/kagent-controller 8083:8083 -n kagent
curl http://localhost:8083/healthz
curl http://localhost:8083/version
```

### MCP IDE integration not working

See `mcp-ide-setup.md` for detailed troubleshooting. Quick checks:
```bash
# Verify agents are eligible
kagent get agent   # should show Accepted + DeploymentReady

# Test controller MCP endpoint
curl http://localhost:8083/mcp -X POST \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2024-11-05","capabilities":{},"clientInfo":{"name":"test","version":"0.0.0"}}}'
```

## Enabling Debug Logging

### On an agent pod
```yaml
spec:
  declarative:
    deployment:
      env:
      - name: LOG_LEVEL
        value: debug
```

### On the controller
```bash
helm upgrade kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --namespace kagent \
  --reuse-values \
  --set controller.loglevel=debug
```

## Getting Help

- **Discord:** https://discord.gg/Fu3k65f2k3
- **GitHub Issues:** https://github.com/kagent-dev/kagent/issues
- **Bug report:** `kagent bug-report` (review for sensitive data before sharing)
