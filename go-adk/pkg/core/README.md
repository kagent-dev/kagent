# Package core

Shared types, interfaces, and implementations for the KAgent ADK.

## Overview

This package contains:

- **Session management** - `Session`, `SessionService`, `KAgentSessionService`
- **Task storage** - `KAgentTaskStore`
- **Event conversion and aggregation** - `EventConverter`, `TaskResultAggregator`
- **Agent execution** - `A2aAgentExecutor`, `Runner` interface
- **Token management** - KAgent API authentication
- **Tracing utilities** - OpenTelemetry integration
- **Configuration types** - Models and MCP servers

The core package is designed to be independent of specific ADK implementations (like Google ADK) to avoid circular dependencies. ADK-specific adapters are provided in the `adk` package.

## Session Management

Sessions track conversation state between the user and agent. The `SessionService` interface defines CRUD operations for sessions, while `KAgentSessionService` implements this interface using the KAgent REST API.

```go
type SessionService interface {
    CreateSession(ctx context.Context, appName, userID string, state map[string]interface{}, sessionID string) (*Session, error)
    GetSession(ctx context.Context, appName, userID, sessionID string) (*Session, error)
    DeleteSession(ctx context.Context, appName, userID, sessionID string) error
    AppendEvent(ctx context.Context, session *Session, event interface{}) error
    AppendFirstSystemEvent(ctx context.Context, session *Session) error
}
```

## Event Processing

Events flow from the runner through the executor to the A2A protocol handler:

```
Runner → A2aAgentExecutor → EventConverter → A2A Protocol Handler
```

The `EventConverter` interface converts internal events to A2A protocol events, and `TaskResultAggregator` accumulates events to determine final task state.

## Configuration

`AgentConfig` holds the complete configuration for an agent, including:

- **Model configuration** - OpenAI, Azure, Gemini, Anthropic, Ollama
- **MCP tool server configurations** - HTTP and SSE
- **Remote agent configurations** - Agent-to-agent communication

## Constants

The package defines several constants for timeouts and buffer sizes:

| Constant | Value | Description |
|----------|-------|-------------|
| `EventChannelBufferSize` | 10 | Buffer size for event channels |
| `EventPersistTimeout` | 30s | Timeout for persisting events |
| `MCPInitTimeout` | 2m | Default MCP initialization timeout |
| `MCPInitTimeoutMax` | 5m | Maximum MCP initialization timeout |
