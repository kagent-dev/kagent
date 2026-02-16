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
│   └─────────────┘    └──────────────────┘    └──────────────┬──────────────┘    │
│                                                             │                   │
│                                                             ▼                   │
│                                                  ┌──────────────────────┐       │
│                                                  │   KAgentExecutor     │       │
│                                                  │   (a2asrv.Agent-     │       │
│                                                  │    Executor bridge)  │       │
│                                                  └──────────┬───────────┘       │
│                                                             │                   │
│   ┌─────────────┐    ┌──────────────────┐                   │                   │
│   │ AgentCard   │───▶│   A2AServer      │◀──────────────────┘                   │
│   │ (metadata)  │    │ (HTTP endpoints) │                                       │
│   └─────────────┘    └──────────────────┘                                       │
│                              │                                                  │
│                              ▼                                                  │
│                      ┌──────────────────┐    ┌────────────────────────────┐     │
│                      │a2asrv.Request-   │    │  KAgentTaskStoreAdapter    │     │
│                      │Handler (a2a-go)  │◀───│  (task persistence)        │     │
│                      │(task lifecycle)  │    └────────────────────────────┘     │
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
│  │                    a2asrv.RequestHandler (a2a-go)                        │   │
│  │  • Creates task with unique ID                                           │   │
│  │  • Manages task lifecycle (submitted → working → completed/failed)       │   │
│  │  • Persists tasks via KAgentTaskStoreAdapter                             │   │
│  └────┬─────────────────────────────────────────────────────────────────────┘   │
│       │ 2. KAgentExecutor.Execute(ctx, reqCtx, queue)                           │
│       │    → bridges to A2aAgentExecutor.Execute(ctx, params, queue, ...)       │
│       ▼                                                                         │
│  ┌──────────────────────────────────────────────────────────────────────────┐   │
│  │                       A2aAgentExecutor                                   │   │
│  │  • Extracts userID, sessionID from request                               │   │
│  │  • Prepares session (get or create via SessionService)                   │   │
│  │  • Appends system event, sends "submitted" → "working" status updates    │   │
│  │  • Converts A2A request to runner args (ConvertA2ARequestToRunArgs)      │   │
│  │  • Processes events, converts via ConvertEventsFunc, aggregates results  │   │
│  └────┬─────────────────────────────────────────────────────────────────────┘   │
│       │ 3. Runner.Run(ctx, args) → <-chan interface{}                           │
│       ▼                                                                         │
│  ┌──────────────────────────────────────────────────────────────────────────┐   │
│  │                           ADKRunner                                      │   │
│  │  • Lazy-initializes Google ADK Runner on first call                      │   │
│  │  • Converts A2A message → genai.Content (A2AMessageToGenAIContent)       │   │
│  │  • Streams ADK events to channel via processEventLoop                    │   │
│  │  • Persists non-partial events to session (persistEvent)                 │   │
│  └────┬─────────────────────────────────────────────────────────────────────┘   │
│       │ 4. adkRunner.Run(ctx, userID, sessionID, content, runConfig)            │
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
│  │               converter.ConvertEventToA2AEvents                          │   │
│  │  (called by A2aAgentExecutor for each event from ADKRunner channel)      │   │
│  │  • ADK Event → A2A Protocol Events (GenAIPartToA2APart)                  │   │
│  │  • Handles: text, function_call, function_response, executable_code,     │   │
│  │    code_execution_result, file_data, inline_data, errors                 │   │
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
│                         ┌──────────┴──────────┐                                 │
│                         │                     │                                 │
│                         ▼                     ▼                                 │
│                ┌────────────┐        ┌────────────────┐                         │
│                │   Model    │        │  MCP Tools     │                         │
│                │  (LLM)     │        │  (HTTP/SSE)    │                         │
│                └─────┬──────┘        └──┬──────────┬──┘                         │
│                      │                  │          │                            │
│                      ▼                  ▼          │                            │
│           ┌─────────────────────────────────────┐  │                            │
│           │          ModelAdapter               │  │                            │
│           │  • Wraps adkmodel.LLM               │  │                            │
│           │  • Injects MCP tools into requests  │  │                            │
│           │  • Delegates GenerateContent to LLM │  │                            │
│           └─────────────────┬───────────────────┘  │                            │
│                             │                      │                            │
│                             ▼                      ▼                            │
│           ┌─────────────────────────────────────────┐                           │
│           │         Google ADK Agent                │                           │
│           │  • llmagent.Agent with ModelAdapter     │                           │
│           │  • MCP toolsets for tool execution      │                           │
│           └─────────────────────┬───────────────────┘                           │
│                                 │                                               │
│                                 ▼                                               │
│           ┌─────────────────────────────────────────┐                           │
│           │         Google ADK Runner               │                           │
│           │  • runner.New(runner.Config{AppName,    │                           │
│           │    Agent, SessionService})              │                           │
│           │  • SessionServiceAdapter wraps our      │                           │
│           │    SessionService for ADK compatibility │                           │
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
│   │  • Get()    → GET from backend, parseEventsToADK                        │   │
│   │  • AppendEvent() → append to wrapper.session.Events + POST to backend   │   │
│   │  • List()   → returns empty (not fully implemented)                     │   │
│   │  • Delete() → DELETE via backend                                        │   │
│   └────────┬────────────────────────────────────────────────────────────────┘   │
│            │                                                                    │
│            ▼                                                                    │
│   ┌─────────────────────────────────────────────────────────────────────────┐   │
│   │                       SessionWrapper                                    │   │
│   │  Implements: google.golang.org/adk/session.Session                      │   │
│   │  • ID(), UserID(), AppName(), State(), Events(), LastUpdateTime()       │   │
│   │  • Events().All() → yields *adksession.Event from session.Events        │   │
│   │  • Events().Len(), Events().At(i) → indexed access                      │   │
│   │  • State().Get/Set/All() → reads/writes session.State map               │   │
│   └─────────────────────────────────────────────────────────────────────────┘   │
│                                                                                 │
│   Event Persistence (ADKRunner.processEventLoop):                               │
│   ┌─────────────────────────────────────────────────────────────────────────┐   │
│   │  for adkEvent, err := range eventSeq {                                  │   │
│   │    // persistEvent: append to session if non-partial or has tool content│   │
│   │    if !adkEvent.Partial || event.EventHasToolContent(adkEvent) {        │   │
│   │      sessionService.AppendEvent(ctx, session, adkEvent)  // persist     │   │
│   │    }                                                                    │   │
│   │    // sendEvent: stream to executor channel                             │   │
│   │    ch <- adkEvent                                                       │   │
│   │  }                                                                      │   │
│   └─────────────────────────────────────────────────────────────────────────┘   │
│                                                                                 │
└─────────────────────────────────────────────────────────────────────────────────┘
```

## Key Components

- **ADKRunner**: Implements `core.Runner`; lazy-creates Google ADK Runner and handles event streaming via `processEventLoop`
- **SessionServiceAdapter**: Adapts `session.SessionService` to Google ADK's `adksession.Service` (Create, Get, AppendEvent, List, Delete)
- **SessionWrapper**: Wraps `session.Session` to implement `adksession.Session` with `eventsWrapper` and `stateWrapper`
- **MCPToolRegistry**: Manages MCP toolsets for tool discovery and execution (HTTP and SSE servers)
- **ModelAdapter**: Wraps `adkmodel.LLM` to inject MCP tools into LLM requests via `cloneLLMRequestWithMCPTools`
- **converter.ConvertEventToA2AEvents**: Converts ADK events (`*adksession.Event`) and `RunnerErrorEvent` to A2A protocol events
- **converter.GenAIPartToA2APart**: Direct `genai.Part` → A2A `Part` conversion (text, file, function call/response, code)
- **KAgentExecutor**: Bridges `a2asrv.AgentExecutor` to `core.A2aAgentExecutor`
- **KAgentTaskStoreAdapter**: Adapts `taskstore.KAgentTaskStore` to `a2asrv.TaskStore` for task persistence

## MCP Tool Integration

MCPToolRegistry fetches and manages tools from MCP servers (both HTTP and SSE). It uses Google ADK's mcptoolset for tool discovery and execution, ensuring compatibility with the ADK's tool handling. Tools are injected into LLM requests via ModelAdapter and registered as ADK toolsets for function call execution.

## Event Conversion

Events from the ADK runner are converted to A2A protocol events for streaming to clients. The conversion handles:

- Text content
- Function calls and responses (with long-running tool metadata for HITL)
- Executable code and code execution results
- File data (URI and inline/base64)
- Error states and finish reasons
- Task state determination: working, input_required (long-running tools), auth_required (request_euc)

## Model Support

The models subpackage provides LLM implementations for various providers:

- OpenAI (including OpenAI-compatible endpoints like LiteLLM, Ollama)
- Azure OpenAI
- Google Gemini (native API and Vertex AI)
- Anthropic (native API via ANTHROPIC_API_KEY)
