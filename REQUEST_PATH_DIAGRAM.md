# Agent Request Path - Flow Diagram

This document maps out the complete request path when a request is made to the agent, specifically highlighting where the `build()` method (lines 70-153 in `_a2a.py`) fits into the flow.

## Request Flow Overview

```
┌─────────────────────────────────────────────────────────────────────────┐
│                         HTTP REQUEST ARRIVES                             │
│                    (e.g., POST /a2a/v1/tasks)                            │
└───────────────────────────────┬─────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                    FastAPI Application (app)                             │
│  ┌───────────────────────────────────────────────────────────────────┐  │
│  │ Routes registered by:                                             │  │
│  │   - app.add_route("/health", ...)                                 │  │
│  │   - app.add_route("/thread_dump", ...)                            │  │
│  │   - a2a_app.add_routes_to_app(app)  ← A2A protocol routes        │  │
│  └───────────────────────────────────────────────────────────────────┘  │
└───────────────────────────────┬─────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────────┐
│              A2AFastAPIApplication (a2a_app)                            │
│  ┌───────────────────────────────────────────────────────────────────┐  │
│  │ Routes added by add_routes_to_app():                             │  │
│  │   - POST /a2a/v1/tasks (create new task)                         │  │
│  │   - GET  /a2a/v1/tasks/{task_id} (get task status)               │  │
│  │   - POST /a2a/v1/tasks/{task_id}/resume (resume task)            │  │
│  │   - Other A2A protocol endpoints                                 │  │
│  └───────────────────────────────────────────────────────────────────┘  │
│                                                                         │
│  These routes delegate to: http_handler (DefaultRequestHandler)       │
└───────────────────────────────┬─────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────────┐
│         DefaultRequestHandler (request_handler)                         │
│  ┌───────────────────────────────────────────────────────────────────┐  │
│  │ Handles HTTP request parsing and delegates to agent_executor     │  │
│  │                                                                   │  │
│  │ 1. Parses HTTP request body                                      │  │
│  │ 2. Builds RequestContext using request_context_builder            │  │
│  │ 3. Creates/updates task in task_store                            │  │
│  │ 4. Creates EventQueue for streaming responses                    │  │
│  │ 5. Calls agent_executor.execute(context, event_queue)             │  │
│  └───────────────────────────────────────────────────────────────────┘  │
└───────────────────────────────┬─────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────────┐
│           A2aAgentExecutor (agent_executor)                              │
│  ┌───────────────────────────────────────────────────────────────────┐  │
│  │ execute(context: RequestContext, event_queue: EventQueue)        │  │
│  │                                                                   │  │
│  │ 1. Converts A2A request to ADK run args                          │  │
│  │ 2. Resolves runner: runner = await _resolve_runner()              │  │
│  │    └─> Calls create_runner() function (defined in build())       │  │
│  │ 3. Prepares session (creates if needed)                          │  │
│  │ 4. Executes: runner.run_async(**run_args)                        │  │
│  │ 5. Converts ADK events to A2A events                             │  │
│  │ 6. Publishes events to event_queue                                │  │
│  └───────────────────────────────────────────────────────────────────┘  │
└───────────────────────────────┬─────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────────┐
│              create_runner() Function (from build())                    │
│  ┌───────────────────────────────────────────────────────────────────┐  │
│  │ ⭐ THIS IS WHERE YOUR CODE BLOCK (lines 83-116) LIVES ⭐        │  │
│  │                                                                   │  │
│  │ 1. Creates root agent: root_agent_instance = self.root_agent()  │  │
│  │                                                                   │  │
│  │ 2. MCP Toolset Replacement (lines 85-108):                      │  │
│  │    - Checks if agent has tools                                   │  │
│  │    - For each McpToolset tool:                                   │  │
│  │      • Extracts connection_params, tool_filter, header_provider  │  │
│  │      • Creates new McpToolset instance                           │  │
│  │      • Replaces old tool with new instance                       │  │
│  │                                                                   │  │
│  │ 3. Creates ADK App:                                             │  │
│  │    adk_app = App(name=..., root_agent=..., plugins=...)         │  │
│  │                                                                   │  │
│  │ 4. Creates Runner:                                              │  │
│  │    return Runner(                                                │  │
│  │        app=adk_app,                                              │  │
│  │        session_service=session_service,                          │  │
│  │        artifact_service=InMemoryArtifactService()                 │  │
│  │    )                                                              │  │
│  └───────────────────────────────────────────────────────────────────┘  │
└───────────────────────────────┬─────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                    Runner.run_async()                                   │
│  ┌───────────────────────────────────────────────────────────────────┐  │
│  │ Executes the ADK agent workflow:                                 │  │
│  │                                                                   │  │
│  │ 1. Processes user message                                         │  │
│  │ 2. Invokes agent tools (including MCP toolsets)                  │  │
│  │ 3. Generates agent responses                                     │  │
│  │ 4. Yields ADK events (Event objects)                             │  │
│  └───────────────────────────────────────────────────────────────────┘  │
└───────────────────────────────┬─────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────────┐
│              Event Conversion & Publishing                              │
│  ┌───────────────────────────────────────────────────────────────────┐  │
│  │ A2aAgentExecutor converts ADK events → A2A events                │  │
│  │ and publishes to event_queue                                      │  │
│  │                                                                   │  │
│  │ Event types:                                                      │  │
│  │   - TaskStatusUpdateEvent (submitted, working, completed, etc.)  │  │
│  │   - TaskArtifactUpdateEvent (agent responses)                    │  │
│  └───────────────────────────────────────────────────────────────────┘  │
└───────────────────────────────┬─────────────────────────────────────────┘
                                │
                                ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                    HTTP Response Stream                                 │
│  ┌───────────────────────────────────────────────────────────────────┐  │
│  │ DefaultRequestHandler streams events back to client               │  │
│  │ via Server-Sent Events (SSE) or HTTP response                    │  │
│  └───────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────┘
```

## Where `build()` Method Fits In

The `build()` method (lines 70-153) is **called once at application startup** to set up the entire request handling infrastructure. It does NOT run on every request, but rather:

### Build-Time Setup (One-Time, at Startup)

```
┌─────────────────────────────────────────────────────────────────────────┐
│                    KAgentApp.build(local=False)                         │
│                                                                         │
│  ┌───────────────────────────────────────────────────────────────────┐ │
│  │ 1. Setup Services (lines 71-80)                                   │ │
│  │    - session_service = InMemorySessionService() or                 │ │
│  │      KAgentSessionService(http_client)                            │ │
│  │    - token_service = KAgentTokenService()                          │ │
│  │    - http_client = httpx.AsyncClient(...)                         │ │
│  └───────────────────────────────────────────────────────────────────┘ │
│                                                                         │
│  ┌───────────────────────────────────────────────────────────────────┐ │
│  │ 2. Define create_runner() Function (lines 83-116)                │ │
│  │    ⭐ THIS FUNCTION IS CALLED ON EVERY REQUEST ⭐                │ │
│  │                                                                   │ │
│  │    This closure captures:                                         │ │
│  │    - self.root_agent (agent factory)                             │ │
│  │    - self.app_name                                               │ │
│  │    - self.plugins                                                │ │
│  │    - session_service (from step 1)                                │ │
│  │                                                                   │ │
│  │    The MCP toolset replacement logic (lines 85-108) runs here    │ │
│  │    every time a new Runner is created for a request              │ │
│  └───────────────────────────────────────────────────────────────────┘ │
│                                                                         │
│  ┌───────────────────────────────────────────────────────────────────┐ │
│  │ 3. Create Agent Executor (lines 118-121)                          │ │
│  │    agent_executor = A2aAgentExecutor(                             │ │
│  │        runner=create_runner,  ← Passes function reference        │ │
│  │        config=...                                                 │ │
│  │    )                                                              │ │
│  └───────────────────────────────────────────────────────────────────┘ │
│                                                                         │
│  ┌───────────────────────────────────────────────────────────────────┐ │
│  │ 4. Setup Task Store (lines 123-125)                               │ │
│  │    task_store = InMemoryTaskStore() or KAgentTaskStore(...)      │ │
│  └───────────────────────────────────────────────────────────────────┘ │
│                                                                         │
│  ┌───────────────────────────────────────────────────────────────────┐ │
│  │ 5. Create Request Handler (lines 127-132)                         │ │
│  │    request_handler = DefaultRequestHandler(                        │ │
│  │        agent_executor=agent_executor,                              │ │
│  │        task_store=task_store,                                      │ │
│  │        request_context_builder=...                                 │ │
│  │    )                                                              │ │
│  └───────────────────────────────────────────────────────────────────┘ │
│                                                                         │
│  ┌───────────────────────────────────────────────────────────────────┐ │
│  │ 6. Create A2A Application (lines 134-137)                        │ │
│  │    a2a_app = A2AFastAPIApplication(                              │ │
│  │        agent_card=self.agent_card,                                │ │
│  │        http_handler=request_handler                                │ │
│  │    )                                                              │ │
│  └───────────────────────────────────────────────────────────────────┘ │
│                                                                         │
│  ┌───────────────────────────────────────────────────────────────────┐ │
│  │ 7. Create FastAPI App (lines 146-151)                             │ │
│  │    app = FastAPI(lifespan=lifespan_manager)                       │ │
│  │    app.add_route("/health", ...)                                  │ │
│  │    app.add_route("/thread_dump", ...)                             │ │
│  │    a2a_app.add_routes_to_app(app)  ← Registers A2A routes       │ │
│  └───────────────────────────────────────────────────────────────────┘ │
│                                                                         │
│  ┌───────────────────────────────────────────────────────────────────┐ │
│  │ 8. Return Configured App (line 153)                               │ │
│  │    return app                                                      │ │
│  └───────────────────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────────────────────┘
```

### Request-Time Execution (Per Request)

When a request arrives:

1. **FastAPI** routes it to the appropriate handler
2. **A2AFastAPIApplication** routes delegate to **DefaultRequestHandler**
3. **DefaultRequestHandler** calls `agent_executor.execute(context, event_queue)`
4. **A2aAgentExecutor.execute()** calls `create_runner()` (defined in `build()`)
5. **create_runner()** executes:
   - Creates root agent instance
   - **Runs MCP toolset replacement logic (lines 85-108)** ⭐
   - Creates ADK App and Runner
   - Returns Runner
6. **Runner.run_async()** executes the agent workflow
7. Events flow back through the chain

## Key Points

1. **`build()` runs once** at application startup to configure the FastAPI app
2. **`create_runner()` (lines 83-116) runs on every request** to create a fresh Runner instance
3. **MCP toolset replacement (lines 85-108) happens on every request** when creating the Runner
4. The Runner instance is created fresh for each request, ensuring isolation
5. All the services (session_service, task_store, etc.) are created once in `build()` and reused across requests

## Component Responsibilities

| Component | Created In | Runs On | Purpose |
|-----------|-----------|---------|---------|
| `session_service` | `build()` line 71/80 | Startup | Manages conversation sessions |
| `token_service` | `build()` line 74 | Startup | Handles authentication tokens |
| `http_client` | `build()` line 75 | Startup | HTTP client for KAgent API |
| `create_runner()` | `build()` line 83 | **Every request** | Factory function for Runner instances |
| `agent_executor` | `build()` line 118 | Startup | Executes agent tasks |
| `task_store` | `build()` line 123/125 | Startup | Stores task state |
| `request_handler` | `build()` line 128 | Startup | Handles HTTP requests |
| `a2a_app` | `build()` line 134 | Startup | A2A protocol application |
| `app` (FastAPI) | `build()` line 146 | Startup | Main FastAPI application |
| `Runner` instance | `create_runner()` | **Every request** | Executes agent workflow |

