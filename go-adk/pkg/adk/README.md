# Package adk

Adapters and integrations between KAgent and Google ADK.

This package bridges the KAgent A2A flow with Google's Agent Development Kit (ADK), handling session management, event conversion, model adapters, and MCP tool integration.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                              Agent Wiring (main.go)                             │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                 │
│   ┌─────────────┐    ┌──────────────────┐    ┌─────────────────────────────┐    │
│   │ AgentConfig │───▶│   ADKRunner      │───▶│   A2aAgentExecutor          │    │
│   │ (from JSON) │    │ (core.Runner)    │    │   (orchestrates execution)  │    │
│   └─────────────┘    └──────────────────┘    └─────────────────────────────┘    │
│                                                          │                      │
│   ┌─────────────┐    ┌──────────────────┐               │                       │
│   │ AgentCard   │───▶│   A2AServer      │◀──────────────┘                       │
│   │ (metadata)  │    │ (HTTP endpoints) │                                       │
│   └─────────────┘    └──────────────────┘                                       │ 
│                              │                                                  │
│                              ▼                                                  │
│                      ┌──────────────────┐                                       │
│                      │  A2ATaskManager  │                                       │
│                      │ (task lifecycle) │                                       │
│                      └──────────────────┘                                       │
│                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
```

## Execution Flow

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                           Request → Response Flow                               │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                 │
│  ┌──────────┐                                                                   │
│  │  Client  │                                                                   │
│  │ (A2A)    │                                                                   │
│  └────┬─────┘                                                                   │
│       │ 1. SendMessage (A2A Protocol)                                           │
│       ▼                                                                         │
│  ┌──────────────────────────────────────────────────────────────────────────┐   │
│  │                         A2ATaskManager                                   │   │
│  │  • Creates task with unique ID                                           │   │
│  │  • Manages task lifecycle (submitted → working → completed/failed)       │   │
│  │  • Handles push notifications                                            │   │
│  └────┬─────────────────────────────────────────────────────────────────────┘   │
│       │ 2. Execute(req, queue, taskID, contextID)                               │
│       ▼                                                                         │
│  ┌──────────────────────────────────────────────────────────────────────────┐   │
│  │                       A2aAgentExecutor                                   │   │
│  │  • Extracts userID, sessionID from request                               │   │
│  │  • Prepares session (get or create via SessionService)                   │   │
│  │  • Sends "submitted" → "working" status updates                          │   │
│  │  • Converts A2A request to runner args                                   │   │
│  │  • Processes events and aggregates results                               │   │
│  └────┬─────────────────────────────────────────────────────────────────────┘   │
│       │ 3. Run(ctx, args) → <-chan interface{}                                  │
│       ▼                                                                         │
│  ┌──────────────────────────────────────────────────────────────────────────┐   │
│  │                           ADKRunner                                      │   │
│  │  • Lazy-initializes Google ADK Runner on first call                      │   │
│  │  • Converts A2A message → genai.Content                                  │   │
│  │  • Streams ADK events to channel                                         │   │
│  │  • Persists events to session                                            │   │
│  └────┬─────────────────────────────────────────────────────────────────────┘   │
│       │ 4. adkRunner.Run(ctx, userID, sessionID, content, config)               │
│       ▼                                                                         │
│  ┌──────────────────────────────────────────────────────────────────────────┐   │
│  │                      Google ADK Runner                                   │   │
│  │  ┌─────────────────────────────────────────────────────────────────────┐ │   │
│  │  │                    runOneStep Loop                                  │ │   │
│  │  │                                                                     │ │   │
│  │  │   ┌──────────────┐    ┌──────────────┐    ┌──────────────────────┐  │ │   │
│  │  │   │ 1.preprocess │───▶│ 2.callLLM    │───▶│ 3.handleFunctionCalls│  │ │   │
│  │  │   │ (build req   │    │ (model.Gen-  │    │ (execute tools,      │  │ │   │
│  │  │   │  from events)│    │  erateContent│    │  yield tool events)  │  │ │   │
│  │  │   └──────────────┘    └──────────────┘    └──────────────────────┘  │ │   │
│  │  │         ▲                                           │               │ │   │
│  │  │         │                                           │               │ │   │
│  │  │         └───────────────────────────────────────────┘               │ │   │
│  │  │                    (loop until IsFinalResponse)                     │ │   │
│  │  └─────────────────────────────────────────────────────────────────────┘ │   │
│  └────┬─────────────────────────────────────────────────────────────────────┘   │
│       │ 5. iter.Seq2[*adksession.Event, error]                                  │
│       ▼                                                                         │
│  ┌──────────────────────────────────────────────────────────────────────────┐   │
│  │                        EventConverter                                    │   │
│  │  • ADK Event → A2A Protocol Events                                       │   │
│  │  • Handles: text, function_call, function_response, errors               │   │
│  │  • Sets task state: working, input_required, auth_required               │   │
│  └────┬─────────────────────────────────────────────────────────────────────┘   │
│       │ 6. TaskStatusUpdateEvent / TaskArtifactUpdateEvent                      │
│       ▼                                                                         │
│  ┌──────────┐                                                                   │
│  │  Client  │ ◀── SSE stream or final response                                  │
│  └──────────┘                                                                   │
│                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
```

## Component Interaction

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                         Component Dependencies                                  │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                 │
│                          ┌───────────────────┐                                  │
│                          │   AgentConfig     │                                  │
│                          │   (types.Agent-   │                                  │
│                          │    Config)        │                                  │
│                          └─────────┬─────────┘                                  │
│                                    │                                            │
│                    ┌───────────────┼───────────────┐                            │
│                    │               │               │                            │
│                    ▼               ▼               ▼                            │
│           ┌────────────┐  ┌────────────┐  ┌────────────────┐                    │
│           │   Model    │  │  MCP Tools │  │  Remote Agents │                    │
│           │  (LLM)     │  │  (HTTP/SSE)│  │  (A2A clients) │                    │
│           └─────┬──────┘  └─────┬──────┘  └────────────────┘                    │
│                 │               │                                               │
│                 ▼               ▼                                               │
│           ┌─────────────────────────────────────────┐                           │
│           │            ModelAdapter                 │                           │
│           │  • Wraps LLM implementation             │                           │
│           │  • Injects MCP tools into requests      │                           │
│           │  • Delegates GenerateContent to LLM     │                           │
│           └─────────────────────┬───────────────────┘                           │
│                                 │                                               │
│                                 ▼                                               │
│           ┌─────────────────────────────────────────┐                           │
│           │         Google ADK Agent                │                           │
│           │  • llmagent.Agent with ModelAdapter     │                           │
│           │  • MCP toolsets for tool execution      │                           │
│           └─────────────────────┬───────────────────┘                           │
│                                 │                                               │
│                                 ▼                                               │
│           ┌─────────────────────────────────────────┐                           │
│           │         Google ADK Runner               │                           │
│           │  • runner.New(agent, sessionService)    │                           │
│           │  • Manages agent execution loop         │                           │
│           └─────────────────────────────────────────┘                           │
│                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
```

## Session & Event Flow

```
┌─────────────────────────────────────────────────────────────────────────────────┐
│                      Session Management Flow                                    │
├─────────────────────────────────────────────────────────────────────────────────┤
│                                                                                 │
│   ┌─────────────────┐         ┌─────────────────────────────────────────────┐   │
│   │ SessionService  │◀───────▶│           KAgent Backend API                │   │
│   │ (KAgent)        │         │  POST /api/sessions (create)                │   │
│   └────────┬────────┘         │  GET  /api/sessions/{id} (get + events)     │   │
│            │                  │  POST /api/sessions/{id}/events (append)    │   │
│            │                  └─────────────────────────────────────────────┘   │
│            ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────────┐   │
│   │                    SessionServiceAdapter                                │   │
│   │  Implements: google.golang.org/adk/session.Service                      │   │
│   │  • Create() → POST to backend, wrap in SessionWrapper                   │   │
│   │  • Get()    → GET from backend, parse ADK events                        │   │
│   │  • AppendEvent() → POST event, update local session.Events              │   │
│   └────────┬────────────────────────────────────────────────────────────────┘   │
│            │                                                                    │
│            ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────────┐   │
│   │                       SessionWrapper                                    │   │
│   │  Implements: google.golang.org/adk/session.Session                      │   │
│   │  • ID(), UserID(), AppName(), State(), Events()                         │   │
│   │  • Events().All() → yields *adksession.Event from session.Events        │   │
│   │  • State().Get/Set() → reads/writes session.State map                   │   │
│   └─────────────────────────────────────────────────────────────────────────┘   │
│                                                                                 │
│   Event Persistence:                                                            │
│   ┌─────────────────────────────────────────────────────────────────────────┐   │
│   │  ADKRunner.processEventLoop():                                          │   │
│   │    for adkEvent := range eventSeq {                                     │   │
│   │      if !adkEvent.Partial || hasToolContent(adkEvent) {                 │   │
│   │        sessionService.AppendEvent(ctx, session, adkEvent)  // persist   │   │
│   │      }                                                                  │   │
│   │      ch <- adkEvent  // stream to executor                              │   │
│   │    }                                                                    │   │
│   └─────────────────────────────────────────────────────────────────────────┘   │
│                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
```

## Key Components

- **ADKRunner**: Implements `core.Runner`; lazy-creates Google ADK Runner and handles event streaming
- **SessionServiceAdapter**: Adapts `core.SessionService` to Google ADK's `session.Service`
- **MCPToolRegistry**: Manages MCP toolsets for tool discovery and execution
- **EventConverter**: Converts ADK events to A2A protocol events
- **ModelAdapter**: Injects MCP tools into LLM requests

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
