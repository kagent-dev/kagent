# Kagent Go Execution Model

## Current Architecture

### Execution Flow

```
User Request (A2A)
  |
  v
A2A Server (HTTP) -- go/adk/pkg/a2a/server/server.go
  |
  v
AdkA2AExecutor.Execute()
  |
  v
beforeExecute callback -- go/adk/pkg/a2a/executor.go:74
  |-- Create/get session (KAgentSessionService)
  |-- Initialize skills
  |-- Set OTel attributes
  v
Google ADK Agent.Run() [BLOCKING] -- google.golang.org/adk
  |-- LLM chat completion
  |-- Tool calls via MCP (blocking, 30min timeout)
  |-- Tool responses -> back to LLM
  |-- Loop until terminal
  v
afterExecute callback -- go/adk/pkg/a2a/executor.go:125
  |-- Enrich HITL approval messages
  v
Task persistence (synchronous)
  |
  v
Response Event -> SessionService.AppendEvent()
```

### Agent Deployment Flow (CRD -> Running Pod)

```
Agent CRD created
  |
  v
AgentController.Reconcile() -- go/core/internal/controller/agent_controller.go:62
  |
  v
AdkTranslator.TranslateAgent() -- go/core/internal/controller/translator/agent/adk_api_translator.go:269
  |
  v
Creates: Deployment + Service + ConfigSecret (config.json + agent-card.json)
  |
  v
Pod starts ADK binary -- go/adk/cmd/main.go
  |-- Reads /config/config.json
  |-- CreateRunnerConfig() -> CreateGoogleADKAgent() + SessionService
  |-- A2AServer.Run() on port 8080
```

### Key Components

| Component | File | Role |
|-----------|------|------|
| Agent creation | `go/adk/pkg/agent/agent.go:32-86` | Creates Google ADK agent with LLM + MCP tools |
| A2A executor | `go/adk/pkg/a2a/executor.go:30-62` | before/afterExecute callbacks, session/skill init |
| Session service | `go/adk/pkg/session/session.go` | REST client to backend for session CRUD |
| Runner adapter | `go/adk/pkg/runner/adapter.go:23-45` | Creates runner.Config with agent + session |
| MCP registry | `go/adk/pkg/mcp/registry.go:40-83` | Creates MCP toolsets from HTTP/SSE servers |
| Task store | `go/adk/pkg/taskstore/store.go` | REST client for A2A task persistence |
| App wiring | `go/adk/pkg/app/app.go:84-145` | Bootstraps all components, starts server |
| ADK entrypoint | `go/adk/cmd/main.go:83-168` | Loads config, creates app, runs server |
| Config types | `go/api/adk/types.go` | AgentConfig with model, tools, instructions |
| DB models | `go/api/database/models.go:60-99` | Session, Event, Task GORM models |
| Telemetry | `go/adk/pkg/telemetry/tracing.go` | OTel span attribute injection |
| LLM providers | `go/adk/pkg/models/` | OpenAI, Anthropic, Gemini, Ollama, Bedrock wrappers |

### Critical Limitations

1. **Fully synchronous** -- `Agent.Run()` blocks until completion
2. **No durability** -- crash loses in-flight execution state
3. **No retry orchestration** -- tool/LLM failures propagate immediately
4. **No distributed coordination** -- single-threaded per request
5. **Session immutable during execution** -- state committed after completion
6. **No long-running workflow support** -- HITL approval is polled, not event-driven

### HTTP Server Session API

- `POST /api/sessions` -- HandleCreateSession (sessions.go:98)
- `GET /api/sessions/{id}` -- HandleGetSession (sessions.go:161)
- `POST /api/sessions/{id}/events` -- HandleAddEventToSession (sessions.go:341)
- `GET /api/sessions/{id}/tasks` -- HandleListTasksForSession (sessions.go:305)
- `DELETE /api/sessions/{id}` -- HandleDeleteSession

### MCP Agent-to-Agent Invocation

`go/core/internal/mcp/mcp_handler.go`:
- `handleListAgents()` (line 107) -- MCP tool listing available agents
- `handleInvokeAgent()` (line 174) -- Invokes agent via A2A protocol, gets/creates client, sends message, returns response
