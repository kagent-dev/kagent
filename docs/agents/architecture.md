# Kagent Architecture

How kagent components interact end-to-end, from agent creation through message processing and tool execution.

**See also:** [go-guide.md](go-guide.md) (Go development), [python-guide.md](python-guide.md) (Python ADK), [ui-guide.md](ui-guide.md) (frontend), [testing-ci.md](testing-ci.md) (CI/CD)

---

## High-level architecture

```
┌─────────────┐   ┌──────────────┐   ┌─────────────┐
│ Controller  │   │  HTTP Server │   │     UI      │
│    (Go)     │──▶│   (Go)       │──▶│ (Next.js)   │
└─────────────┘   └──────────────┘   └─────────────┘
       │                  │
       ▼                  ▼
┌─────────────┐   ┌──────────────┐
│  Database   │   │ Agent Runtime│
│ (SQLite/PG) │   │   (Python)   │
└─────────────┘   └──────────────┘
```

## End-to-end flow (UI -> Controller -> Agent -> LLM -> Tools -> UI)

### UI (Next.js)

- Entry: `ui/src/app/` Next.js app router
- Sends A2A JSON-RPC messages to the controller HTTP server
- Renders streaming responses via SSE
- Manages agents, models, tool servers, and conversations

### Controller (Go)

- Entry: `go/core/cmd/controller/main.go`
- Kubernetes controllers reconcile Agent, ModelConfig, RemoteMCPServer, MCPServer CRDs
- Creates Deployments, ConfigMaps, Secrets, ServiceAccounts for agent pods
- Upserts metadata to database (SQLite or PostgreSQL)
- HTTP API server on port 8083 proxies A2A requests to agent pods

### Agent Runtime (Python ADK)

- Entry: `python/packages/kagent-adk/src/kagent/adk/cli.py`
- Each agent pod runs the Python ADK
- Reads `config.json` from mounted Secret to configure agent behavior
- Connects to MCP tool servers for tool execution
- Manages LLM conversation loop: system prompt + history + tool calls

### Agent Runtime (Go ADK)

- Entry: `go/adk/cmd/main.go`
- Alternative runtime for agents written in Go
- Supports BYO (Bring Your Own) agent pattern
- Uses Google ADK for agent creation and session management

## Key subsystem boundaries

| Subsystem | Language | Root path |
|-----------|----------|-----------|
| CRD Types & API | Go | `go/api/` |
| Controllers | Go | `go/core/internal/controller/` |
| HTTP Server | Go | `go/core/internal/httpserver/` |
| Database Layer | Go | `go/core/internal/database/` |
| A2A Protocol | Go | `go/core/internal/a2a/` |
| MCP Integration | Go | `go/core/internal/mcp/` |
| CLI | Go | `go/core/cli/` |
| Go ADK | Go | `go/adk/` |
| Python ADK | Python | `python/packages/kagent-adk/` |
| Python Core | Python | `python/packages/kagent-core/` |
| Python Skills | Python | `python/packages/kagent-skills/` |
| Web UI | TypeScript | `ui/src/` |
| Helm Charts | YAML | `helm/` |
| Pre-built Agents | YAML | `helm/agents/` |

## Critical dependency directions

Flag violations of these dependency rules:

```
# Allowed direction (arrow = "may depend on"). Reverse is forbidden.
go/core/  -> go/api/       (core may use api types, NOT the reverse)
go/adk/   -> go/api/       (adk may use api types, NOT the reverse)
go/core/internal/controller/  -> go/core/internal/database/  (controller may use db, NOT reverse)
go/core/internal/httpserver/  -> go/core/internal/database/  (http may use db, NOT reverse)

# Forbidden imports
go/api/   must NOT import go/core/ or go/adk/
ui/       must NOT import go/ or python/ directly
```

## Controller patterns

- **Shared reconciler**: All controllers share a single `kagentReconciler` instance (`go/core/internal/controller/reconciler/`)
- **Translator pattern**: CRD specs are translated to Kubernetes resources via translators (`go/core/internal/controller/translator/`)
- **Database-level concurrency**: Atomic upserts (`INSERT ... ON CONFLICT DO UPDATE`), no application-level locks
- **Network I/O outside transactions**: Prevents long-running operations from holding database locks
- **Event filtering**: Custom predicates filter Create/Delete/Update events to reduce unnecessary reconciliation

## CRD types (v1alpha2)

| CRD | Purpose | Definition |
|-----|---------|------------|
| Agent | AI agent configuration (declarative or BYO) | `go/api/v1alpha2/agent_types.go` |
| ModelConfig | LLM provider configuration | `go/api/v1alpha2/modelconfig_types.go` |
| RemoteMCPServer | Remote MCP tool server | `go/api/v1alpha2/remotemcpserver_types.go` |
| ModelProviderConfig | Provider-level configuration | `go/api/v1alpha2/modelproviderconfig_types.go` |

## Protocols

- **A2A (Agent-to-Agent)**: JSON-RPC 2.0 over HTTP with SSE streaming for inter-agent communication
- **MCP (Model Context Protocol)**: Standard protocol for tool server integration (SSE or Streamable HTTP)

## Database support

- **SQLite** (default): Local development, single-node deployments
- **PostgreSQL**: Production deployments, supports pgvector for memory

## Go module structure

Single workspace module at `go/go.mod` (module `github.com/kagent-dev/kagent/go`):

```
go/
├── api/         # Shared types (CRDs, DB models, HTTP API, client SDK)
├── core/        # Infrastructure (controllers, HTTP server, CLI, database)
└── adk/         # Go Agent Development Kit
```

## Python package structure

UV workspace at `python/`:

```
python/packages/
├── kagent-adk/       # Main Python ADK (agent executor, A2A server, MCP toolset)
├── kagent-core/      # Core utilities
├── kagent-skills/    # Skills framework
├── kagent-openai/    # OpenAI native integration
├── kagent-langgraph/ # LangGraph framework support
├── kagent-crewai/    # CrewAI framework support
└── agentsts-*/       # AgentSTS variants
```

## Helm deployment

Two charts (install CRDs first):

```bash
helm install kagent-crds helm/kagent-crds/   # CRD definitions
helm install kagent helm/kagent/             # Main application
```

Pre-built agents available in `helm/agents/` (k8s, istio, helm, prometheus, grafana, etc.).
