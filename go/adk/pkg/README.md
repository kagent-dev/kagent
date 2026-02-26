# Package Structure

Shared types, interfaces, and implementations for the Kagent Go ADK.

## Overview

- **a2a/** - A2A executor, event conversion (GenAI <-> A2A), error mappings, HITL; includes `server/` for the HTTP server and health checks
- **agent/** - Google ADK agent creation from `AgentConfig`
- **app/** - Application lifecycle (server startup, shutdown, task store wiring)
- **auth/** - KAgent API token management
- **config/** - Agent configuration loading, validation, and type re-exports from `go/api/adk`
- **mcp/** - MCP client toolset creation from HTTP/SSE server configs
- **models/** - LLM model adapters (OpenAI, Anthropic) implementing Google ADK's `model.LLM`
- **runner/** - Google ADK `runner.Config` creation from `AgentConfig`
- **session/** - Session management, persistence, and ADK session service adapter
- **skills/** - Agent skills discovery and shell execution
- **taskstore/** - Task storage and A2A result aggregation
- **telemetry/** - OpenTelemetry tracing utilities

## Event Processing

The executor (`KAgentExecutor`) holds a `*runner.Runner` directly and implements `a2asrv.AgentExecutor`:

```
main.go -> CreateGoogleADKRunner -> *runner.Runner
         |
KAgentExecutor.Execute(ctx, reqCtx, queue)
  -> runner.Run(ctx, userID, sessionID, content, runConfig)
  -> iterate *adksession.Event
  -> ConvertADKEventToA2AEvents -> queue.Write
  -> inline aggregation -> final status/artifact
```

## Type Deduplication

Model and config types (e.g. `Model`, `AgentConfig`, `OpenAI`, `Anthropic`) are defined
canonically in `go/api/adk/` and re-exported via type aliases in `config/types.go`.
This avoids duplication while keeping the `config.` package qualifier that existing
code uses.
