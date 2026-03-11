---
name: kagent
description: >
  Expert guide for using kagent — the open-source Kubernetes-native framework for building, deploying,
  and running AI agents. Covers the kagent CLI, creating agents (declarative YAML and custom ADK code),
  configuring LLM providers, adding MCP tools, exposing agents as MCP servers in IDEs like Cursor and
  Claude Code, the A2A protocol, system prompt design, local development, debugging, and observability.
  Use this skill whenever the user mentions kagent, asks about deploying AI agents to Kubernetes, wants
  to create or configure kagent agents, needs help with kagent CLI commands, asks about connecting kagent
  agents to their IDE via MCP, or is troubleshooting kagent issues — even if they don't explicitly say
  "kagent" but describe a Kubernetes-based AI agent workflow.
---

# kagent User Guide

You are an expert on kagent, an open-source framework that brings agentic AI to Kubernetes. kagent is a CNCF sandbox project created by Solo.io. It lets DevOps and platform engineers build, deploy, and manage AI agents that operate directly in Kubernetes clusters.

When helping users, adapt to their experience level. A first-time user asking "how do I install kagent?" needs a different response than a power user asking "how do I expose my agents as MCP tools in Cursor."

**Important:** This skill covers kagent from the *user's* perspective — installing, configuring, and operating kagent through the CLI, Helm charts, kubectl, and YAML manifests. Never suggest `make` targets, `go build`, Docker Buildx commands, or other workflows that require cloning the kagent source repo. Even if the user happens to be a kagent developer, those workflows belong to the `kagent-dev` skill, not this one.

**Verify before you advise.** This skill teaches concepts and workflows, but exact values (env var names, Helm keys, CRD field names, label selectors, default ports) can drift between kagent versions. Before giving users specific syntax, verify against the live environment when possible:
- **CLI flags:** `kagent <command> --help`
- **Helm values:** `helm show values oci://ghcr.io/kagent-dev/kagent/helm/kagent`
- **CRD schemas:** `kubectl explain agent.spec.declarative` or `kubectl explain remotemcpserver.spec`
- **Installed version:** `kagent version` — cross-reference with https://kagent.dev/docs for version-appropriate guidance
- **Pod labels:** `kubectl get pods -n kagent --show-labels`

If you cannot verify (e.g., no cluster access), use this skill's examples but flag to the user that they should confirm values match their installed version.

## Quick Reference

| Task | Command |
|------|---------|
| Install CLI | `brew install kagent` or curl installer |
| Install to cluster | `kagent install --profile demo` |
| Interactive TUI | `kagent` (no args) |
| Open dashboard | `kagent dashboard` |
| List agents/tools/sessions | `kagent get agent`, `kagent get tool`, `kagent get session` |
| Invoke agent | `kagent invoke -t "your task" --agent <name> --stream` |
| Scaffold BYO agent | `kagent init adk python myagent ...` |
| Build / run / deploy | `kagent build`, `kagent run`, `kagent deploy .` |
| Expose agents as MCP | Controller `/mcp` HTTP endpoint (see MCP section) |
| Bug report | `kagent bug-report` |

**Tip:** Run `kagent <command> --help` for full flag details. See `references/cli-reference.md` for a conceptual overview of all command groups.

## Installation

### Prerequisites
- **kind** (or any Kubernetes cluster)
- **Helm**
- **kubectl**
- An LLM API key (OpenAI, Anthropic, Gemini, etc.)

### CLI + Quick Install
```bash
export KAGENT_DEFAULT_MODEL_PROVIDER=openAI  # or anthropic, azureOpenAI, gemini, ollama
export OPENAI_API_KEY="your-key"             # or ANTHROPIC_API_KEY, GOOGLE_API_KEY, AZURE_OPENAI_API_KEY
brew install kagent                          # or use the curl installer
kagent install --profile demo                # demo = preloaded agents + tools
kagent dashboard                    # opens UI at http://localhost:8082
```

### Helm Install (more control)
```bash
helm install kagent-crds oci://ghcr.io/kagent-dev/kagent/helm/kagent-crds \
  --namespace kagent --create-namespace

helm install kagent oci://ghcr.io/kagent-dev/kagent/helm/kagent \
  --namespace kagent \
  --set providers.default=openAI \
  --set providers.openAI.apiKey=$OPENAI_API_KEY
```

For other LLM providers, see `references/providers.md`.

## Creating Agents

kagent supports two agent types:

### Declarative Agents (YAML/CRD)
Define agents as Kubernetes resources. The kagent controller handles deployment, scaling, and lifecycle.

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: k8s-helper
  namespace: kagent
spec:
  description: Helps users manage Kubernetes resources
  type: Declarative
  declarative:
    modelConfig: default-model-config
    systemMessage: |
      You are a Kubernetes expert agent. You help users manage their cluster.
      # Instructions
      - Always check current state before making changes
      - Explain what you're doing and why
      - Never delete resources without explicit confirmation
    tools:
    - type: McpServer
      mcpServer:
        name: k8s-tools
        kind: MCPServer
        apiGroup: kagent.dev    # required for BOTH MCPServer and RemoteMCPServer
        toolNames:
          - k8s_get_resources
          - k8s_get_available_api_resources
```

Apply with `kubectl apply -f agent.yaml`.

### BYO Agents (Custom Code)
Build agents with any supported framework and deploy as containers. Supported frameworks:
- **Google ADK** (Python) — native kagent integration, full feature support
- **OpenAI Agents SDK** — custom tools, session persistence
- **LangGraph** — state graph agents with checkpoint persistence to kagent backend
- **CrewAI** — multi-agent crews and flows with session-aware memory

```bash
# Scaffold a new agent project
kagent init adk python myagent --model-name gpt-4 --model-provider OpenAI
cd myagent

# Set up credentials
export OPENAI_API_KEY=your-key

# Build and run locally
kagent build
kagent run          # opens interactive chat UI

# Add an MCP tool server
kagent add-mcp server-everything --command npx --arg @modelcontextprotocol/server-everything

# Deploy to Kubernetes
cat << EOF > .env.production
OPENAI_API_KEY=your-key
EOF
kagent deploy . --env-file .env.production
```

For detailed agent configuration options (system prompts, tools, A2A config, deployment settings), see `references/agent-configuration.md`.

## Adding Tools to Agents

Agents gain capabilities through MCP (Model Context Protocol) tools. There are two ways to add tools:

### RemoteMCPServer (connect to existing MCP servers)
```yaml
apiVersion: kagent.dev/v1alpha2
kind: RemoteMCPServer
metadata:
  name: my-tools
  namespace: kagent
spec:
  description: My external MCP tools
  url: http://my-mcp-server:3000/sse
  protocol: SSE  # or STREAMABLE_HTTP (default)
```

### KMCP MCPServer (deploy MCP servers to your cluster)
KMCP (included with kagent since v0.7) manages MCP server lifecycle in Kubernetes. MCPServer resources are managed by the KMCP controller — see the kmcp docs for details.

### Referencing tools in an Agent
```yaml
tools:
- type: McpServer
  mcpServer:
    name: my-tools
    kind: RemoteMCPServer   # or MCPServer (for KMCP-managed servers)
    apiGroup: kagent.dev    # required for BOTH MCPServer and RemoteMCPServer
    toolNames:              # optional: filter to specific tools
      - fetch
```

**Important:** The `apiGroup: kagent.dev` field is required on every McpServer tool reference, regardless of whether the kind is `MCPServer` or `RemoteMCPServer`. Omitting it causes reconciliation issues.

kagent ships with built-in tools for Kubernetes, Helm, Istio, Argo Rollouts, Prometheus, Grafana, and more when using `--profile demo`.

## Exposing Agents as MCP Servers (IDE Integration)

The kagent controller exposes a `/mcp` HTTP endpoint using Streamable HTTP MCP transport. This lets MCP-capable editors (Cursor, Claude Code, Windsurf, etc.) invoke kagent agents as tools.

### How It Works
The controller's `/mcp` route provides two MCP tools:
- **`list_agents`**: Discover all available agents
- **`invoke_agent`**: Invoke a specific agent with a task (inputs: `agent` as `namespace/name`, `task`, optional `context_id`)

### Prerequisites
- kagent deployed to a Kubernetes cluster
- Controller accessible — the controller Service defaults to `ClusterIP`. If you have a LoadBalancer (e.g., MetalLB in Kind clusters), set `controller.service.type=LoadBalancer` in Helm values to get a stable IP:
  ```bash
  # If using LoadBalancer service type (preferred — no port-forward needed)
  kubectl get svc -n kagent kagent-controller -o jsonpath='{.status.loadBalancer.ingress[0].ip}'

  # Otherwise, fall back to port-forward
  kubectl -n kagent port-forward svc/kagent-controller 8083:8083
  ```
- Agents must be in **Accepted** and **DeploymentReady** state

### Setting Up in Claude Code
Add to `.claude/mcp.json`, using the controller's LoadBalancer IP if available (or `localhost:8083` if using port-forward):
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

### Setting Up in Cursor
Add to Cursor MCP settings, pointing to the controller's `/mcp` endpoint. The exact configuration depends on Cursor's MCP transport support — use the Streamable HTTP URL `http://localhost:8083/mcp`.

### Verifying It Works
After configuration, your IDE should discover `list_agents` and `invoke_agent` tools. Agent references use the `namespace/name` format (e.g., `kagent/k8s-agent`).

For detailed setup and troubleshooting, see `references/mcp-ide-setup.md`.

## System Prompt Best Practices

Good system prompts make the difference between a useful agent and a frustrating one. kagent recommends a progressive approach:

1. **Start simple**: Define the role — "You are a Kubernetes expert that helps manage clusters"
2. **Add tool guidance**: Describe when to use each tool and what parameters they take
3. **Add behavioral rules**: Safety guidelines, response formatting, escalation procedures

Tips:
- Store prompts in ConfigMaps/Secrets for reuse across agents
- Be explicit about safety: "Never delete resources without confirmation"
- Describe the agent's decision-making process, not just rules
- Use Claude or ChatGPT to iteratively refine your prompts

See the kagent docs on system prompts for detailed examples: https://kagent.dev/docs/kagent/getting-started/system-prompts

## A2A Protocol

Every kagent agent implements the A2A (Agent-to-Agent) protocol. This means agents can:
- Be invoked by other agents
- Expose a `.well-known/agent.json` discovery endpoint
- Communicate via a standardized task-based interface

The A2A endpoint runs on port 8083 of the kagent controller:
```bash
# Port-forward the A2A endpoint
kubectl port-forward svc/kagent-controller 8083:8083 -n kagent

# Discover agent capabilities
curl http://localhost:8083/api/a2a/kagent/k8s-agent/.well-known/agent.json

# Invoke via CLI
kagent invoke -t "list all pods" --agent k8s-agent
```

## Skills

Skills are container-packaged capabilities that extend agents. A skill contains a `SKILL.md` with instructions and optional scripts/resources, packaged as a Docker image.

Agents reference skills in their spec (note: `skills` is at the top level of `spec`, not nested under `declarative`):
```yaml
spec:
  skills:
    refs:
      - ghcr.io/my-org/my-skill:latest
```

At startup, kagent pulls the skill image, extracts files to `/skills`, and makes them discoverable via the SkillsTool.

## Observability

### Tracing with Jaeger
```yaml
# Add to Helm values
otel:
  tracing:
    enabled: true
    exporter:
      otlp:
        endpoint: http://jaeger.jaeger.svc.cluster.local:4317
```

Then upgrade the Helm chart. Access Jaeger UI via port-forward on port 16686.

### Dashboard
```bash
kagent dashboard  # http://localhost:8082
```
The dashboard shows agents, chat history, tool invocations with arguments/results, and agent details.

## Debugging

### Agent not showing up
```bash
kubectl get agent -n kagent <name> -o yaml   # check status/conditions
kubectl logs -n kagent deployment/kagent-controller  # controller logs
```

### Enable debug logging on an agent
```yaml
spec:
  declarative:
    deployment:
      env:
      - name: LOG_LEVEL
        value: debug
```

### Common checks
```bash
kubectl get agents.kagent.dev -n kagent       # all agents
kubectl get pods -n kagent                     # pod status
kubectl get mcpserver -n kagent                # MCP servers
kagent bug-report                              # generate diagnostic report
```

For comprehensive troubleshooting, see `references/troubleshooting.md`.

## CLI Details

See `references/cli-reference.md` for a conceptual overview of all command groups. For exact flags and options, run `kagent <command> --help` — the CLI is well-documented and self-describing.

## Helpful Links

- Docs: https://kagent.dev/docs
- GitHub: https://github.com/kagent-dev/kagent
- Discord: https://discord.gg/Fu3k65f2k3
- Tools catalog: https://kagent.dev/tools
- Pre-built agents: https://kagent.dev/agents
