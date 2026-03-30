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

```bash
export KAGENT_DEFAULT_MODEL_PROVIDER=openAI  # or anthropic, azureOpenAI, gemini, ollama
export OPENAI_API_KEY="your-key"             # or ANTHROPIC_API_KEY, GOOGLE_API_KEY, AZURE_OPENAI_API_KEY
brew install kagent                          # or use the curl installer
kagent install --profile demo                # demo = preloaded agents + tools
kagent dashboard                             # opens UI at http://localhost:8082
```

For Helm install, other LLM providers, and provider-specific configuration, see `references/providers.md`.

## Core Concepts

kagent uses Kubernetes CRDs to manage agents, models, and tools:

- **Agent** (`kagent.dev/v1alpha2`) — Defines an AI agent. Two types: **Declarative** (YAML-defined, controller-managed) and **BYO** (custom container image with any framework: Google ADK, OpenAI Agents SDK, LangGraph, CrewAI).
- **ModelConfig** (`kagent.dev/v1alpha2`) — Configures LLM provider and model. Agents reference a ModelConfig by name.
- **RemoteMCPServer** (`kagent.dev/v1alpha2`) — Connects agents to external MCP tool servers via HTTP.
- **MCPServer** (KMCP) — Deploys and manages MCP server pods in the cluster.

**Key rules:**
- Tool references in agents **must** include `apiGroup: kagent.dev` for both MCPServer and RemoteMCPServer kinds.
- `skills.refs` is a list of strings (image refs), not objects.
- `skills` is at `spec` level, not nested under `declarative`.

For full CRD examples, system prompt design, prompt templates, and deployment options, see `references/agent-configuration.md`.

## Adding Tools to Agents

Agents gain capabilities through MCP (Model Context Protocol) tools. Create a `RemoteMCPServer` to connect to an existing server, or use KMCP `MCPServer` to deploy one in-cluster. Then reference it in the Agent's `tools` list.

For RemoteMCPServer YAML, auth headers, tool filtering, and complete examples, see `references/agent-configuration.md`.

## Exposing Agents as MCP Servers (IDE Integration)

The kagent controller exposes a `/mcp` HTTP endpoint (Streamable HTTP transport) that lets MCP-capable editors (Cursor, Claude Code, Windsurf, etc.) invoke kagent agents as tools. It provides two MCP tools: `list_agents` and `invoke_agent`.

Point your IDE's MCP config at the controller's `/mcp` endpoint on port 8083 (via LoadBalancer IP or `kubectl port-forward`). For detailed setup, IDE-specific configuration, and troubleshooting, see `references/mcp-ide-setup.md`.

## A2A Protocol

Every kagent agent implements A2A (Agent-to-Agent), exposing a `.well-known/agent.json` discovery endpoint and a task-based invocation interface on port 8083 of the controller. Use `kagent invoke` or `curl` against `http://localhost:8083/api/a2a/kagent/<agent-name>/`.

## Observability

- **Dashboard:** `kagent dashboard` opens the UI at `http://localhost:8082` — shows agents, chat history, and tool invocations.
- **Tracing:** Enable OpenTelemetry export to Jaeger via `otel.tracing.enabled=true` in Helm values.

## Debugging & Troubleshooting

Quick checks:
```bash
kubectl get agent -n kagent <name> -o yaml     # check status/conditions
kubectl logs -n kagent deployment/kagent-controller  # controller logs
kagent bug-report                              # generate diagnostic report
```

For systematic debugging (crash-looping pods, MCP session failures, common errors), see `references/troubleshooting.md`.

## Helpful Links

- Docs: https://kagent.dev/docs
- GitHub: https://github.com/kagent-dev/kagent
- Discord: https://discord.gg/Fu3k65f2k3
- Tools catalog: https://kagent.dev/tools
- Pre-built agents: https://kagent.dev/agents
