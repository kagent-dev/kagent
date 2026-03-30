# Exposing kagent Agents as MCP Tools in Your IDE

## Overview

kagent's controller exposes a `/mcp` HTTP endpoint that makes all deployed agents available as MCP tools. This means your IDE's AI assistant (Cursor, Claude Code, Windsurf, etc.) can directly invoke kagent agents — ask your Kubernetes agent to check pod status, have your Helm agent list releases, or let your observability agent query Prometheus, all from within your editor.

## Architecture

The kagent controller exposes a `/mcp` route using **Streamable HTTP MCP** transport. This endpoint provides two tools:

- **`list_agents`**: Discover all available agents and their descriptions
- **`invoke_agent`**: Invoke a specific agent with a task

The `invoke_agent` tool accepts:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `agent` | string | yes | Agent reference in `namespace/name` format |
| `task` | string | yes | The prompt/task to send to the agent |
| `context_id` | string | no | A2A context ID for conversation continuity |

## Prerequisites

1. **kagent deployed** to a Kubernetes cluster
2. **Controller accessible** — the controller Service defaults to `ClusterIP`. To use a LoadBalancer IP (no port-forward needed), set `controller.service.type=LoadBalancer` in your Helm values (works with MetalLB in Kind, cloud LBs, etc.):
   ```bash
   # If using LoadBalancer service type
   KAGENT_IP=$(kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
   echo "Controller at: http://${KAGENT_IP}:8083"

   # If using default ClusterIP, fall back to port-forward
   kubectl -n kagent port-forward svc/kagent-controller 8083:8083
   ```
3. **Agents in ready state** — only agents that are both `Accepted` and `DeploymentReady` are exposed

## IDE Configuration

### Claude Code

Add to your project's `.claude/mcp.json` (use the LoadBalancer IP if available, or `localhost:8083` with port-forward):
```json
{
  "mcpServers": {
    "kagent": {
      "type": "streamable-http",
      "url": "http://<controller-ip>:8083/mcp"
    }
  }
}
```

Or add globally via Claude Code settings.

### Cursor

Add to your Cursor MCP configuration (Settings > MCP), pointing to the Streamable HTTP endpoint at `http://<controller-ip>:8083/mcp`.

### Other MCP-capable editors

Any MCP client that supports Streamable HTTP transport can connect to `http://localhost:8083/mcp`.

## Usage Examples

Once configured, you can ask your IDE's AI assistant things like:

- "Use the k8s agent to list all pods in the default namespace"
- "Ask the helm agent what charts are installed"
- "Have the observability agent check if there are any firing alerts"

The assistant will use `list_agents` to discover available agents, then `invoke_agent` to route the task to the actual agent running in your cluster.

## Troubleshooting

### No tools discovered
- Verify controller is accessible: `curl http://localhost:8083/healthz`
- Check that agents are ready: `kagent get agent`
- Ensure port-forward is active

### Tool invocation fails
- Check agent pod is running: `kubectl get pods -n kagent`
- Check controller logs: `kubectl logs -n kagent deployment/kagent-controller`
- Verify the agent reference format is `namespace/name` (e.g., `kagent/k8s-agent`)

### Agent not listed
- Agent must be in both `Accepted` and `DeploymentReady` state
- Check agent status: `kubectl get agent <name> -n kagent -o yaml`
