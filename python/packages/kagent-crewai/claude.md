# Kagent CrewAI Integration

## Overview

The `kagent-crewai` package provides integration between CrewAI and the Kagent framework. It enables CrewAI-based multi-agent crews and flows to run within Kagent's Kubernetes-native environment with A2A (Agent-to-Agent) protocol support, session-aware memory, and flow state persistence.

## Architecture

This package bridges CrewAI's multi-agent orchestration framework with Kagent's infrastructure by:
- Executing CrewAI Crews and Flows within A2A protocol
- Converting CrewAI execution events to A2A protocol events
- Providing session-aware long-term memory storage via Kagent API
- Persisting Flow states for resumable execution
- Providing FastAPI-based HTTP servers for agent endpoints
- Supporting distributed multi-agent execution with memory sharing

## Key Components

### Core Classes

#### KAgentApp (`_a2a.py`)
- **Purpose**: Main application builder for Kagent CrewAI agents
- **Key Method**: `build()` - Creates a FastAPI app for Kubernetes deployment (lines 52-93)
- **Features**:
  - A2A protocol server setup
  - Integrates with Kagent API for task management
  - Health check endpoint at `/health`
  - Thread dump endpoint at `/thread_dump` for debugging
  - OpenTelemetry tracing via `kagent.core.configure_tracing()`
  - CrewAI-specific instrumentation via `CrewAIInstrumentor()`
- **Constructor Parameters**:
  - `crew`: CrewAI `Crew` or `Flow` instance
  - `agent_card`: A2A protocol agent metadata
  - `config`: `KAgentConfig` with URL and app name (defaults to environment-based config)
  - `executor_config`: Optional `CrewAIAgentExecutorConfig`
  - `tracing`: Enable/disable OpenTelemetry tracing (default: True)
- **Tracing**: Controlled by `OTEL_TRACING_ENABLED` environment variable (lines 82-87)

#### CrewAIAgentExecutor (`_executor.py`)
- **Purpose**: Executes CrewAI Crews/Flows in response to A2A requests
- **Extends**: `AgentExecutor` from A2A server
- **Key Features**:
  - Supports both `Crew` and `Flow` execution modes
  - Streams CrewAI events as A2A events via listeners
  - Handles session-aware memory configuration
  - Manages flow state persistence
  - Timeout management for long-running crews
- **Configuration** (`CrewAIAgentExecutorConfig`):
  - `execution_timeout`: Max execution time (default: 300s)
- **Workflow** (execute method, lines 58-100+):
  1. Validate A2A request has message
  2. Emit task submission and working status events
  3. Register A2A event listener for CrewAI events
  4. Extract input from message parts
  5. Configure memory (Crew mode) or persistence (Flow mode)
  6. Execute crew/flow with inputs
  7. Stream events through listener
  8. Return final result
- **Memory Configuration**:
  - **Crew Mode**: Injects `KagentMemoryStorage` into `LongTermMemory`
  - **Flow Mode**: Injects `KagentFlowPersistence` for state management
- **Session Mapping**:
  - A2A context_id → CrewAI thread_id
  - User identified via request context

#### KagentMemoryStorage (`_memory.py`)
- **Purpose**: Session-aware long-term memory for CrewAI agents
- **Compatible With**: CrewAI's `LongTermMemory` interface
- **Key Features**:
  - Stores memory items in Kagent backend
  - Scoped by thread_id (session) and user_id
  - Semantic search across memory items
  - Compatible with CrewAI's memory scoring system
- **API Methods**:
  - `save()`: POST `/api/crewai/memory` - Store memory item (lines 29-54)
  - `load()`: GET `/api/crewai/memory?q={query}&limit={n}` - Retrieve memories (lines 56-93)
  - `reset()`: DELETE `/api/crewai/memory?thread_id={id}` - Clear session memory (lines 95-100+)
- **Memory Structure**:
  - task_description: Task context
  - score: Relevance score
  - metadata: Agent metadata
  - datetime: Timestamp
- **Scoping**:
  - **Session Scoping**: Memories shared within same session across agents
  - **User Scoping**: User-specific memory isolation via `X-User-ID` header
  - **Agent Volatility**: Agent IDs are volatile, session ID is stable identifier

#### KagentFlowPersistence (`_state.py`)
- **Purpose**: Flow state persistence for CrewAI Flows
- **Extends**: `FlowPersistence` from CrewAI
- **Key Features**:
  - Saves and loads Flow state to Kagent backend
  - Enables resumable flow execution
  - State scoped by thread_id and flow_uuid
  - Supports method-level state persistence
- **API Methods**:
  - `save_state()`: POST `/api/crewai/flows/state` - Store flow state (lines 36-53)
  - `load_state()`: GET `/api/crewai/flows/state?thread_id={id}&flow_uuid={uuid}` - Load flow state (lines 55-70)
  - `init_db()`: No-op, handled by Kagent backend (lines 32-34)
- **State Structure**:
  - thread_id: Session identifier
  - flow_uuid: Unique flow identifier
  - method_name: Current method in flow
  - state_data: Serialized state (dict or Pydantic model)
- **Use Case**: Enables `@persist()` decorator in CrewAI Flows for checkpoint recovery

#### A2ACrewAIListener (`_listeners.py`)
- **Purpose**: Converts CrewAI events to A2A events in real-time
- **Features**:
  - Listens to CrewAI execution events
  - Converts events to A2A format
  - Enqueues A2A events for streaming to clients
- **Event Mapping**:
  - Task start/completion → TaskStatusUpdateEvent
  - Agent actions → TaskArtifactUpdateEvent
  - Tool calls → Message parts
  - Errors → TaskStatusUpdateEvent with error state

## Data Models

### Memory Models

#### KagentMemoryPayload
- Represents a memory item to be stored
- Fields: thread_id, user_id, memory_data (task_description, score, metadata, datetime)

#### KagentMemoryResponse
- Response wrapper for memory queries
- Fields: data (list of KagentMemoryPayload)

### Flow State Models

#### KagentFlowStatePayload
- Represents flow state to be persisted
- Fields: thread_id, flow_uuid, method_name, state_data

#### KagentFlowStateResponse
- Response wrapper for flow state queries
- Fields: data (KagentFlowStatePayload)

## Integration Points

### Kagent API Integration
The package integrates with the Kagent controller via HTTP API:
- **Base URL**: Configured via `KAgentConfig.url` (environment-based by default)
- **Memory**: POST/GET/DELETE `/api/crewai/memory`
- **Flow State**: POST/GET `/api/crewai/flows/state`
- **Task Management**: Uses `KAgentTaskStore` for A2A task lifecycle
- **User Authentication**: `X-User-ID` header for user-scoped operations

### A2A Protocol Integration
- Uses `A2AStarletteApplication` for A2A protocol server
- Implements `DefaultRequestHandler` with custom executor
- Custom request context builder: `KAgentRequestContextBuilder`
- Task store: `KAgentTaskStore` (integrates with Kagent API)

### CrewAI Integration
- Supports both `Crew` and `Flow` modes
- Injects custom memory storage into `LongTermMemory`
- Injects custom persistence into Flow state management
- Uses CrewAI's event system for streaming
- Compatible with `memory=True` setting in Crews

### Tracing Integration
- OpenTelemetry tracing via `kagent.core.configure_tracing()`
- CrewAI-specific instrumentation via `opentelemetry.instrumentation.crewai.CrewAIInstrumentor`
- Environment variable control: `OTEL_TRACING_ENABLED=true`
- Traces crew execution, agent actions, and tool calls

## Execution Modes

### Crew Mode
- **Purpose**: Execute multi-agent crews with session-aware memory
- **Memory**: Long-term memory stored in Kagent backend
- **Inputs**: Simple string input or no input
- **Memory Sharing**: Agents in same session share long-term memory
- **Configuration**: Set `memory=True` when creating Crew
- **User Responsibility**: Configure short-term and entity memory providers

### Flow Mode
- **Purpose**: Execute CrewAI Flows with state persistence
- **State**: Flow state saved after each method execution
- **Persistence**: Enables `@persist()` decorator for resumable flows
- **Session Scope**: Each session is a single flow execution
- **Memory**: User must implement custom memory for crews within flows
- **Use Case**: Complex workflows with multiple crew orchestrations

## Dependencies

### External
- **crewai**: CrewAI multi-agent framework
- **a2a.server**: A2A protocol server implementation
- **fastapi**: HTTP server framework
- **httpx**: Async HTTP client
- **pydantic**: Data validation
- **opentelemetry**: Distributed tracing
- **opentelemetry.instrumentation.crewai**: CrewAI-specific tracing

### Internal
- **kagent.core**: Core Kagent utilities (A2A helpers, task store, config, tracing)

## Usage Example

### Crew Mode Example

```python
from kagent.crewai import KAgentApp
from kagent.core import KAgentConfig
from a2a.types import AgentCard
from crewai import Crew, Agent, Task

# Define your crew
agent = Agent(role="Researcher", goal="Research topics")
task = Task(description="Research {input}", agent=agent)
crew = Crew(agents=[agent], tasks=[task], memory=True)

# Create agent card
agent_card = AgentCard(
    name="research-crew",
    description="A research crew with memory"
)

# Build Kagent app (uses environment-based config by default)
kagent_app = KAgentApp(
    crew=crew,
    agent_card=agent_card,
    tracing=True
)

# Get FastAPI app for deployment
app = kagent_app.build()
```

### Flow Mode Example

```python
from kagent.crewai import KAgentApp
from crewai import Flow
from crewai.flow.flow import persist

class ResearchFlow(Flow):
    @persist()
    def research_step(self, input: str):
        # Flow state will be persisted
        return {"result": "research done"}

flow = ResearchFlow()
kagent_app = KAgentApp(crew=flow, agent_card=agent_card)
app = kagent_app.build()
```

## Key Features

1. **Multi-Agent Orchestration**: Full CrewAI Crew and Flow support
2. **Session-Aware Memory**: Long-term memory scoped by session and user
3. **Flow State Persistence**: Resumable flow execution with state recovery
4. **Event Streaming**: Real-time streaming of crew execution events
5. **User Isolation**: Memory and state isolated per user
6. **Memory Sharing**: Agents in same session share long-term memory
7. **Observability**: OpenTelemetry tracing with CrewAI instrumentation
8. **Flexible Input**: String input mapping to crew kickoff parameters

## Task Input Convention

For this version, tasks should accept either:
- **Single `input` parameter**: String input from A2A message
- **No parameters**: Tasks with predefined inputs

Example YAML task definition:
```yaml
research_task:
  description: >
    Research topics on {input} and provide a summary.
```

This is equivalent to `crew.kickoff(inputs={"input": "your input text"})`.

## Memory Behavior

### Crew Mode Memory
- **Backend**: Kagent API (`/api/crewai/memory`)
- **Scope**: Session ID + User ID
- **Sharing**: All agents in same session share memory
- **Storage**: Compatible with CrewAI's `LTMSQLiteStorage` logic
- **Search**: Semantic search by task description
- **Scoring**: Supports CrewAI's relevance scoring

### Flow Mode Memory
- **Backend**: Kagent API (`/api/crewai/flows/state`)
- **Scope**: Thread ID + Flow UUID
- **Persistence**: Method-level state checkpoints
- **Resume**: Load state from previous executions
- **Crew Memory**: User must implement custom memory for flows

## Configuration

### Environment Variables
- `OTEL_TRACING_ENABLED`: Enable OpenTelemetry tracing (default: false)
- Additional config via `KAgentConfig` (see kagent.core)

### CrewAIAgentExecutorConfig
- `execution_timeout`: Maximum execution time (default: 300s)

### KAgentConfig
- `url`: Kagent API endpoint
- `app_name`: Application name for identification

## Comparison with Other Integrations

| Feature | kagent-adk | kagent-langgraph | kagent-crewai |
|---------|------------|------------------|---------------|
| Framework | Google ADK | LangGraph | CrewAI |
| Multi-Agent | Single agent | Graph nodes | Native multi-agent |
| State Management | ADK Sessions | LangGraph Checkpoints | Flow Persistence + LTM |
| Memory | Session events | Graph state | Long-term memory + Flow state |
| Primary Use Case | Single-agent apps | Graph workflows | Multi-agent collaboration |
| Team Collaboration | No | Via graph structure | Native crew structure |

## API Endpoints

### Memory Management
- `POST /api/crewai/memory` - Store memory item
- `GET /api/crewai/memory?q={query}&limit={n}&thread_id={id}` - Query memories
- `DELETE /api/crewai/memory?thread_id={id}` - Clear session memory

### Flow State Management
- `POST /api/crewai/flows/state` - Save flow state
- `GET /api/crewai/flows/state?thread_id={id}&flow_uuid={uuid}` - Load flow state

### A2A Protocol (inherited from A2AStarletteApplication)
- Standard A2A endpoints for task execution
- Task streaming and status updates
- Agent card discovery

## Related Components

- **Kagent Controller** (`go/internal/controller/`): Kubernetes controller managing agent lifecycle
- **Kagent Core** (`python/packages/kagent-core/`): Shared Python utilities
- **ADK Integration** (`python/packages/kagent-adk/`): Alternative using Google ADK
- **LangGraph Integration** (`python/packages/kagent-langgraph/`): Alternative using LangGraph

## Package Structure

```
kagent-crewai/
├── src/kagent/crewai/
│   ├── __init__.py           # Main exports (KAgentApp)
│   ├── _a2a.py               # FastAPI app builder (lines 36-93)
│   ├── _executor.py          # CrewAI executor (lines 38-100+)
│   ├── _memory.py            # Long-term memory storage (lines 18-100+)
│   ├── _state.py             # Flow state persistence (lines 21-70)
│   └── _listeners.py         # CrewAI → A2A event conversion
└── README.md                 # Package documentation
```

## Best Practices

1. **Memory Configuration**: Set `memory=True` for crews that need session-aware memory
2. **Short-Term Memory**: Configure STM and entity memory providers (e.g., OpenAI API key)
3. **Flow Persistence**: Use `@persist()` decorator for methods that need state checkpoints
4. **Input Convention**: Use `{input}` placeholder in task descriptions for dynamic inputs
5. **Session Consistency**: Keep same session_id to share memory across agent interactions
6. **User Isolation**: Leverage user_id for multi-tenant deployments
7. **Tracing**: Enable tracing in production for debugging multi-agent interactions
8. **Timeout Management**: Adjust `execution_timeout` for long-running crews

## Limitations

- **Input Format**: Currently limited to single `input` string parameter
- **Flow Memory**: No automatic LTM management for crews inside flows
- **Cancellation**: Cancellation not yet implemented

## Future Enhancements

- **Structured Inputs**: JSON/structured input for multiple task parameters
- **Advanced Memory**: Configurable memory backends and strategies
- **Cancellation Support**: Graceful crew execution cancellation
- **Memory Analytics**: Query and visualize memory usage patterns
- **Multi-Flow Orchestration**: Coordinate multiple flows within single app
