# Package Structure

Shared types, interfaces, and implementations for the KAgent ADK.

## Overview

- **a2a/** - Core A2A agent logic: executor (implements `a2asrv.AgentExecutor`), event conversion (GenAI ↔ A2A), error mappings, HITL
- **a2a/agent/** - Google ADK agent and runner creation from config
- **a2a/server/** - A2A HTTP server and task store adapter
- **auth/** - KAgent API token management
- **config/** - Agent configuration loading
- **mcp/** - MCP client toolset management
- **models/** - LLM model adapters (OpenAI, Anthropic, etc.)
- **session/** - Session management and persistence
- **skills/** - Agent skills discovery and shell execution
- **taskstore/** - Task storage and result aggregation
- **telemetry/** - OpenTelemetry tracing utilities
- **types/** - Shared configuration types

## Event Processing

The executor (`KAgentExecutor`) holds a `*runner.Runner` directly and implements `a2asrv.AgentExecutor`:

```
main.go → CreateGoogleADKRunner → *runner.Runner
         ↓
KAgentExecutor.Execute(ctx, reqCtx, queue)
  → runner.Run(ctx, userID, sessionID, content, runConfig)
  → iterate *adksession.Event
  → ConvertADKEventToA2AEvents → queue.Write
  → inline aggregation → final status/artifact
```

No intermediate `Runner` interface, no event channels, no bridge adapters.
