# Kagent Architecture Guide

This directory contains detailed architecture documentation for kagent. Start with this README for a system-wide overview, then dive into specific documents for deeper understanding.

## Documents

| Document | Description |
|----------|-------------|
| [README.md](README.md) | This file - system-wide architecture overview |
| [controller-reconciliation.md](controller-reconciliation.md) | Controller concurrency model, reconciliation flows, event filtering |
| [human-in-the-loop.md](human-in-the-loop.md) | Tool approval system (HITL), ask-user tool, UI integration |
| [prompt-templates.md](prompt-templates.md) | Prompt template system with ConfigMap includes and variable interpolation |
| [data-flow.md](data-flow.md) | End-to-end request flow from UI to agent and back |
| [crds-and-types.md](crds-and-types.md) | All Custom Resource Definitions and their relationships |

---

## System Overview

Kagent is a Kubernetes-native framework for building, deploying, and managing AI agents. Users define agents as Kubernetes Custom Resources (CRDs), and the system handles deployment, tool connectivity, conversation management, and UI.

```
                        ┌──────────────────────────────────┐
                        │           User / UI              │
                        │         (Next.js app)            │
                        └───────────────┬──────────────────┘
                                        │ HTTP (JSON-RPC / A2A)
                                        ▼
┌───────────────────────────────────────────────────────────────────────────┐
│                     kagent-controller (Go binary)                         │
│                                                                           │
│  ┌─────────────────────┐  ┌──────────────────┐  ┌─────────────────────┐  │
│  │  Controller Manager │  │   HTTP Server    │  │     Database        │  │
│  │                     │  │   (port 8083)    │  │   (SQLite/Postgres) │  │
│  │  - AgentController  │  │                  │  │                     │  │
│  │  - RemoteMCPServer  │  │  - REST API      │  │  - Agents           │  │
│  │    Controller       │  │  - A2A proxy     │  │  - ToolServers      │  │
│  │  - MCPServer (KMCP) │  │  - UI backend    │  │  - Tools            │  │
│  │  - ModelConfig      │  │                  │  │  - Sessions         │  │
│  │    Controller       │  │                  │  │  - Conversations    │  │
│  └─────────┬───────────┘  └────────┬─────────┘  └─────────────────────┘  │
│            │                       │                                      │
│            │ creates/updates       │ proxies A2A requests                 │
│            ▼                       ▼                                      │
│  ┌─────────────────────────────────────────────┐                         │
│  │          Kubernetes API Server              │                         │
│  │  Deployments, Services, Secrets, ConfigMaps │                         │
│  └──────────────────────┬──────────────────────┘                         │
└─────────────────────────┼────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                      Agent Pods (per agent)                             │
│                                                                         │
│  ┌───────────────────────────────────────────────────────────────────┐  │
│  │                  Python ADK Runtime (or Go ADK)                   │  │
│  │                                                                   │  │
│  │  - A2A server (receives messages from controller proxy)           │  │
│  │  - Google ADK Runner (manages LLM interaction loop)               │  │
│  │  - MCP clients (connect to tool servers)                          │  │
│  │  - Event converter (ADK events <-> A2A protocol)                  │  │
│  │  - Session management (in-memory or external)                     │  │
│  └───────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                       MCP Tool Servers                                  │
│                                                                         │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                  │
│  │ kagent-tools │  │ grafana-mcp  │  │  custom MCP  │                  │
│  │ (built-in)   │  │              │  │  servers     │                  │
│  └──────────────┘  └──────────────┘  └──────────────┘                  │
└─────────────────────────────────────────────────────────────────────────┘
```

---

## Core Components

### 1. Controller Manager (Go)

The controller manager runs inside the `kagent-controller` pod and manages multiple Kubernetes controllers that share a single `kagentReconciler` instance.

**Controllers:**

| Controller | Watches | Produces | Key File |
|-----------|---------|----------|----------|
| `AgentController` | `Agent` CRD | Deployment, Service, Secret (config), ServiceAccount | `go/core/internal/controller/agent_controller.go` |
| `RemoteMCPServerController` | `RemoteMCPServer` CRD | DB entries for tool servers + discovered tools | `go/core/internal/controller/remotemcpserver_controller.go` |
| `MCPServerController` | `MCPServer` CRD (via KMCP) | Managed MCP server pods | External KMCP controller |
| `ModelConfigController` | `ModelConfig` CRD | DB entries, secret hash tracking | `go/core/internal/controller/modelconfig_controller.go` |

**Shared Reconciler**: All controllers delegate to `kagentReconciler` (`go/core/internal/controller/reconciler/reconciler.go`), which holds references to the translator, kube client, and database client.

**Translator**: The `adkApiTranslator` (`go/core/internal/controller/translator/agent/adk_api_translator.go`) converts Agent CRD specs into:
- A **Deployment** (with the agent container image, environment, volumes)
- A **Service** (ClusterIP, exposing the agent's HTTP port)
- A **Secret** (containing `config.json` — the serialized agent configuration read by the Python/Go ADK runtime)
- A **ServiceAccount** (unless a custom one is specified)

See [controller-reconciliation.md](controller-reconciliation.md) for concurrency model details.

### 2. HTTP Server (Go)

The HTTP server runs in the same `kagent-controller` binary, listening on port 8083.

**Key responsibilities:**
- **REST API** for the UI (CRUD operations on agents, conversations, sessions)
- **A2A proxy** that forwards Agent-to-Agent protocol messages from the UI to agent pods
- **A2A server** that exposes agents configured with `a2aConfig` to external callers

**Key file:** `go/core/internal/httpserver/server.go`

**Important endpoints:**

| Path Pattern | Method | Description |
|-------------|--------|-------------|
| `/api/agents` | GET | List agents (from DB) |
| `/api/agents/{namespace}/{name}` | GET | Get agent details |
| `/api/sessions` | GET/POST/DELETE | Session management |
| `/api/sessions/{id}/events` | POST | Persist session events |
| `/api/tasks` | GET/POST | A2A task management |
| `/api/a2a/{namespace}/{name}` | POST | A2A JSON-RPC endpoint (proxied to agent pod) |
| `/api/toolservers` | GET | List tool servers |
| `/api/tools` | GET | List available tools |
| `/api/models` | GET | List model configs |
| `/api/modelconfigs` | GET/POST | Model configuration CRUD |
| `/api/memories` | GET/POST | Vector search & storage |
| `/api/runs` | GET | Agent run tracking |
| `/api/feedback` | POST | User feedback collection |
| `/mcp` | POST | MCP protocol proxy |
| `/health` | GET | Health check |

### 3. Database Layer

The controller uses SQLite (default) or PostgreSQL for persistent state that supplements what Kubernetes stores in etcd.

**Key models** (`go/api/database/models.go`):

| Model | Purpose |
|-------|---------|
| `Agent` | Cached agent metadata (name, namespace, description, config) |
| `ToolServer` | Tool server metadata (name, URL, protocol) |
| `Tool` | Individual tools discovered from MCP servers |
| `Conversation` | Chat conversation (linked to an agent) |
| `Session` | Agent session (linked to a conversation) |

**Why a separate DB?** The Kubernetes API is not designed for high-frequency read patterns like listing conversations or searching tools. The DB provides fast lookups for the HTTP API and UI, while the CRDs remain the source of truth for agent configuration.

**Key files:**
- `go/api/database/models.go` — database models
- `go/core/internal/database/client.go` — Database client implementation
- `go/core/internal/database/service.go` — Business logic with atomic upserts

### 4. Agent Runtime (Python ADK)

Each agent runs as a separate Kubernetes pod with the Python ADK runtime (or optionally Go ADK).

**Startup flow:**
1. Pod starts with the `kagent-adk` container image
2. Reads `config.json` from a mounted Secret (created by the translator)
3. `config.json` contains: system message, model config, MCP server connections, tool lists, memory config, etc.
4. Starts a [uvicorn](https://www.uvicorn.org/) HTTP server implementing the A2A protocol
5. Connects to MCP tool servers listed in the config

**Request handling flow:**
1. Controller HTTP server receives a message from the UI
2. Proxies it via A2A JSON-RPC to the agent pod's service
3. Agent executor creates/resumes a session and runs the Google ADK `Runner`
4. ADK runner manages the LLM conversation loop (prompt → response → tool calls → tool results → repeat)
5. Events are converted from ADK format to A2A format via the event converter
6. A2A events are streamed back through the controller proxy to the UI

**Built-in tools (added to every agent):**
- `AskUserTool` — lets the LLM ask the user structured questions (uses HITL plumbing)
- `SkillsTool` — discovers and loads skills from the `/skills` directory
- Memory tools (if memory enabled) — `LoadMemoryTool`, `SaveMemoryTool`, `PrefetchMemoryTool`

**Key files:**
- `python/packages/kagent-adk/src/kagent/adk/_a2a.py` — `KAgentApp` FastAPI application factory
- `python/packages/kagent-adk/src/kagent/adk/_agent_executor.py` — Core executor (handles A2A requests)
- `python/packages/kagent-adk/src/kagent/adk/types.py` — Config types (mirrors Go ADK types)
- `python/packages/kagent-adk/src/kagent/adk/converters/` — ADK event <-> A2A protocol converters
- `python/packages/kagent-adk/src/kagent/adk/_session_service.py` — Session persistence via controller API
- `python/packages/kagent-adk/src/kagent/adk/_mcp_toolset.py` — MCP toolset wrapper
- `python/packages/kagent-adk/src/kagent/adk/models/` — LLM provider implementations (OpenAI native, LiteLLM)
- `python/packages/kagent-adk/src/kagent/adk/_token.py` — K8s service account token refresh

### 5. UI (Next.js)

The web interface is a Next.js application that communicates with the controller HTTP server.

**Key features:**
- Agent list and management
- Chat interface with streaming responses
- Tool call visualization (requested → executing → completed)
- Human-in-the-loop tool approval UI
- Model configuration management
- MCP server management

**Communication:** The UI uses a custom `KagentA2AClient` that sends A2A JSON-RPC messages over HTTP streaming to the controller's API.

**Key files:**
- `ui/src/lib/kagentA2AClient.ts` — A2A client
- `ui/src/lib/messageHandlers.ts` — Message parsing and event handling
- `ui/src/components/chat/ChatInterface.tsx` — Main chat component
- `ui/src/components/ToolDisplay.tsx` — Tool call rendering

---

## Custom Resource Definitions (CRDs)

Kagent defines four main CRDs (all in `apiVersion: kagent.dev/v1alpha2`):

### Agent

The primary resource. Defines an AI agent with its system prompt, model, tools, and deployment configuration.

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: my-agent
spec:
  type: Declarative  # or BYO (Bring Your Own)
  description: "Agent description"
  declarative:
    runtime: python    # or go
    systemMessage: "You are a helpful agent..."
    modelConfig: my-model-config  # reference to ModelConfig
    stream: true
    tools:
      - type: McpServer
        mcpServer:
          name: my-tool-server
          kind: RemoteMCPServer
          apiGroup: kagent.dev
          toolNames: [tool1, tool2]
          requireApproval: [tool2]  # HITL
      - type: Agent
        agent:
          name: sub-agent  # agent-to-agent
    deployment:
      replicas: 1
      resources: ...
    memory:
      modelConfig: embedding-model
    context:
      compaction:
        compactionInterval: 5
    a2aConfig:
      skills:
        - name: "skill-name"
          description: "..."
  skills:
    refs: ["ghcr.io/org/skill-image:latest"]
```

**Two agent types:**
- **Declarative** — Kagent manages the entire agent lifecycle. The controller creates a Deployment with the ADK runtime container, injects configuration, and manages MCP connections.
- **BYO (Bring Your Own)** — User provides a custom container image. Kagent creates the Deployment but the user controls the agent runtime.

### ModelConfig

Configures LLM provider credentials and settings.

```yaml
apiVersion: kagent.dev/v1alpha2
kind: ModelConfig
metadata:
  name: my-model
spec:
  provider: OpenAI  # Anthropic, AzureOpenAI, Ollama, Gemini, GeminiVertexAI, AnthropicVertexAI, Bedrock
  model: gpt-4o
  apiKeySecret: my-api-key-secret
  apiKeySecretKey: api-key
  openAI:
    temperature: "0.7"
    maxTokens: 4096
```

### RemoteMCPServer

Declares a remote MCP tool server that agents can reference.

```yaml
apiVersion: kagent.dev/v1alpha2
kind: RemoteMCPServer
metadata:
  name: my-tool-server
spec:
  description: "My tool server"
  protocol: STREAMABLE_HTTP  # or SSE
  url: http://my-tools.default:8084/mcp
  timeout: 30s
```

When reconciled, the controller connects to the MCP server, discovers available tools, and stores them in the database. The discovered tools appear in the `status.discoveredTools` field.

### MCPServer (via KMCP)

Managed MCP servers — the KMCP controller handles deploying and running these as pods. Not directly part of kagent's codebase but integrated via the KMCP operator.

---

## Key Data Flows

### Agent Creation Flow

```
User applies Agent YAML
    → K8s API Server stores Agent CR
    → AgentController receives Create event
    → kagentReconciler.reconcile()
        → adkApiTranslator.translateInlineAgent()
            → Resolves ModelConfig (fetches API key from Secret)
            → Resolves prompt template (if configured)
            → Resolves MCP server connections
            → Builds config.json
            → Returns: Deployment, Service, Secret, ServiceAccount
        → Reconcile each resource (create/update via K8s API)
        → Store agent in database (atomic upsert)
        → Update Agent status (Accepted=True)
    → Deployment creates agent Pod
    → Pod starts Python ADK, reads config.json, connects to MCP servers
    → Agent pod becomes Ready
    → AgentController updates status (Ready=True)
```

### Message Flow (UI → Agent → UI)

```
User types message in UI
    → KagentA2AClient.sendMessageStream()
    → POST /api/agents/{ns}/{name}/conversations/{id}/messages
    → Controller HTTP server
        → Creates/gets conversation + session in DB
        → Proxies A2A JSON-RPC to agent pod Service
    → Agent pod receives A2A message
        → AgentExecutor._handle_request()
            → Creates ADK Runner with session
            → Runner calls LLM with system prompt + history + tools
            → LLM responds (text, tool calls, or both)
            → If tool call: execute via MCP client → get result → loop
            → Event converter: ADK events → A2A TaskStatusUpdateEvents
            → Stream events back via HTTP
    → Controller proxy streams A2A events to UI
    → UI renders messages, tool calls, results in real-time
```

### Tool Approval Flow (HITL)

See [human-in-the-loop.md](human-in-the-loop.md) for the full flow. Summary:
1. Agent calls tool marked with `requireApproval`
2. `before_tool_callback` calls `request_confirmation()`, pauses execution
3. UI shows Approve/Reject buttons on tool card
4. User decides → UI sends decision → executor resumes with `ToolConfirmation`
5. Approved: tool executes normally. Rejected: tool returns rejection message to LLM.

---

## Protocol: A2A (Agent-to-Agent)

Kagent uses the [A2A protocol](https://github.com/google/A2A) as the communication protocol between the controller and agent pods. A2A uses JSON-RPC 2.0 over HTTP with streaming support.

**Key A2A concepts in kagent:**
- **Task**: Represents a unit of work (a user message and the agent's response)
- **Message**: Contains `parts` (TextPart, DataPart, FilePart)
- **DataPart**: Used for structured data like tool calls/results
- **TaskState**: `submitted` → `working` → `completed` (or `input_required`, `auth_required`, `failed`)
- **Streaming**: Events are streamed via Server-Sent Events (SSE) within the JSON-RPC response

---

## Protocol: MCP (Model Context Protocol)

Agents connect to tool servers using the [MCP protocol](https://modelcontextprotocol.io/). MCP provides a standardized way for agents to discover and invoke tools.

**Two transport types supported:**
- **Streamable HTTP** (preferred) — Single HTTP endpoint, multiplexed
- **SSE** — Server-Sent Events based (legacy)

**MCP in kagent:**
- `RemoteMCPServer` CRD defines where tool servers live
- Controller discovers tools at reconciliation time (stored in DB)
- Agent runtime connects to MCP servers at startup using config from `config.json`
- Tool calls during conversation are sent via MCP to the tool server

---

## Go Module Structure

The Go code is organized as a Go workspace (`go.work`) with three modules:

```
go/
├── go.work
├── api/        # github.com/kagent-dev/kagent/go/api
│   ├── v1alpha2/         # CRD type definitions
│   ├── database/         # database models
│   ├── httpapi/          # HTTP API request/response types
│   ├── client/           # REST client SDK for the HTTP API
│   └── config/crd/       # Generated CRD manifests
│
├── core/       # github.com/kagent-dev/kagent/go/core
│   ├── cmd/
│   │   ├── controller/   # Main controller binary entry point
│   │   └── kagent/       # CLI tool entry point
│   ├── internal/
│   │   ├── controller/   # K8s controllers and reconciler
│   │   │   ├── reconciler/   # Shared kagentReconciler
│   │   │   └── translator/   # CRD → K8s resource translators
│   │   │       └── agent/    # Agent-specific translator
│   │   ├── httpserver/   # HTTP API server
│   │   └── database/     # Database client implementation
│   └── test/e2e/         # E2E tests
│
└── adk/        # github.com/kagent-dev/kagent/go/adk
    ├── types.go          # ADK config types (shared with Python)
    ├── pkg/
    │   ├── app/          # KAgentApp - main application wiring
    │   ├── a2a/server/   # A2A HTTP server with health endpoints
    │   ├── agent/        # Google ADK agent creation + LLM providers
    │   ├── models/       # LLM provider implementations (OpenAI, Anthropic, etc.)
    │   ├── mcp/          # MCP toolset creation
    │   ├── session/      # Session service (connects to controller API)
    │   ├── config/       # Config loading from files/env
    │   ├── auth/         # K8s service account token auth
    │   └── telemetry/    # OpenTelemetry tracing
    └── examples/         # BYO and one-shot agent examples
```

**Import rules:**
- `api` → imported by both `core` and `adk` (shared types)
- `core` → imports `api`, must NOT import `adk`
- `adk` → imports `api`, must NOT import `core`

---

## Python Package Structure

The Python code uses a UV workspace with multiple packages:

```
python/
├── packages/
│   ├── kagent-adk/           # Main ADK package
│   │   └── src/kagent/adk/
│   │       ├── __init__.py
│   │       ├── _agent_executor.py    # Core executor
│   │       ├── _approval.py          # HITL approval callback
│   │       ├── types.py              # Config types (mirrors Go)
│   │       ├── converters/           # Event/part converters
│   │       │   ├── event_converter.py
│   │       │   └── part_converter.py
│   │       └── ...
│   ├── kagent-core/          # Core utilities
│   └── kagent-skills/        # Skills framework
└── samples/                  # Example agents
```

---

## Helm Deployment

Kagent is deployed via two Helm charts:

1. **kagent-crds** (`helm/kagent-crds/`) — CRD definitions, installed first
2. **kagent** (`helm/kagent/`) — Main application including:
   - `kagent-controller` Deployment (controller + HTTP server)
   - `kagent-ui` Deployment (Next.js app)
   - `kagent-kmcp-controller` Deployment (KMCP operator for managed MCP servers)
   - `kagent-tools` Deployment (built-in tool server, optional)
   - Various Services, ConfigMaps, ServiceAccounts, RBAC

Agent Helm charts in `helm/agents/` provide pre-configured agents (k8s-agent, helm-agent, istio-agent, etc.) that can be installed alongside the core chart.

---

## Key Architectural Decisions

1. **CRDs as source of truth** — Agent configuration lives in Kubernetes CRDs. The database is a read-optimized cache, not the source of truth.

2. **A2A as the agent communication protocol** — Rather than a custom protocol, kagent uses the open A2A standard for all controller-to-agent communication.

3. **Controller-as-proxy** — The controller HTTP server proxies A2A requests to agent pods. The UI never talks directly to agent pods. This centralizes auth, routing, and observability.

4. **Config via Secret** — Agent configuration (system prompt, model credentials, MCP connections) is serialized as `config.json` in a Kubernetes Secret, mounted into the agent pod. This decouples CRD reconciliation from runtime configuration.

5. **Dual runtime** — Agents can use either Python ADK (full features, Google ADK-based) or Go ADK (faster startup, most features). The `runtime` field on the CRD controls which container image and readiness probe are used.

6. **Template resolution at reconciliation time** — Prompt templates are resolved by the controller, not at runtime. The agent receives a fully resolved string. This makes debugging easier and keeps the runtime simple.

7. **HITL via ADK's built-in mechanism** — Tool approval uses the Google ADK's `request_confirmation()` rather than custom logic. This minimizes custom code and ensures compatibility with ADK updates.
