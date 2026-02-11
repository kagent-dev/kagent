# Package adk

Adapters and integrations between KAgent and Google ADK.

This package bridges the KAgent A2A flow with Google's Agent Development Kit (ADK), handling session management, event conversion, model adapters, and MCP tool integration.

## Architecture

The package follows an adapter pattern:

```
KAgent A2A → A2aAgentExecutor → GoogleADKRunnerWrapper → Google ADK Runner → LLM + Tools
```

Key components:

- **SessionServiceAdapter**: Adapts core.SessionService to Google ADK's session.Service
- **GoogleADKRunnerWrapper**: Wraps Google ADK Runner to implement core.Runner
- **MCPToolRegistry**: Manages MCP toolsets for tool discovery and execution
- **EventConverter**: Converts ADK events to A2A protocol events
- **ModelAdapter**: Injects MCP tools into LLM requests

## Session Management

SessionServiceAdapter implements Google ADK's session.Service interface by delegating to a core.SessionService (typically KAgentSessionService). This allows the ADK runner to use KAgent's session storage while maintaining ADK compatibility.

Sessions store events as ADK Event JSON, matching the Python kagent-adk implementation. The adapter handles parsing events from the backend and converting them to ADK types.

## MCP Tool Integration

MCPToolRegistry fetches and manages tools from MCP servers (both HTTP and SSE). It uses Google ADK's mcptoolset for tool discovery and execution, ensuring compatibility with the ADK's tool handling.

## Event Conversion

Events from the ADK runner are converted to A2A protocol events for streaming to clients. The conversion handles:

- Text content
- Function calls and responses
- Code execution results
- Error states and finish reasons

## Model Support

The models subpackage provides LLM implementations for various providers:

- OpenAI (including OpenAI-compatible endpoints like LiteLLM, Ollama)
- Azure OpenAI
- Google Gemini (native API and Vertex AI)
- Anthropic (native API via ANTHROPIC_API_KEY)
