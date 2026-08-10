# Package Structure

Shared types, interfaces, and implementations for the Kagent Go ADK.

## Overview

- **a2a/** - A2A executor, event conversion (GenAI <-> A2A), error mappings, HITL; includes `server/` for the HTTP server and health checks
- **agent/** - Google ADK agent creation from `AgentConfig`
- **app/** - Application lifecycle (server startup, shutdown, task store wiring)
- **auth/** - KAgent API token management
- **config/** - Agent configuration loading and validation
- **mcp/** - MCP client toolset creation from HTTP/SSE server configs
- **models/** - LLM model adapters (OpenAI, Anthropic) implementing Google ADK's `model.LLM`
- **runner/** - Google ADK `runner.Config` creation from `AgentConfig`
- **session/** - Session management, persistence, and ADK session service adapter
- **skills/** - Agent skills discovery and shell execution
- **taskstore/** - A2A task persistence through the kagent controller API
- **telemetry/** - OpenTelemetry tracing utilities

## Event Processing

The executor (`KAgentExecutor`) is a thin kagent-specific wrapper around the
upstream `adka2a.Executor`:

```
main.go -> CreateRunnerConfig -> runner.Config
         |
KAgentExecutor.Execute(ctx, reqCtx)
  -> kagent auth, telemetry, skills, session state, HITL resume setup
  -> adka2a.Executor.Execute(ctx, reqCtx)
  -> artifact updates for task output
  -> status-only lifecycle and terminal events
```
