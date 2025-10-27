# Kagent LangGraph Integration

## Overview

The `kagent-langgraph` package provides integration between LangGraph and the Kagent framework. It enables LangGraph-based agents to run within Kagent's Kubernetes-native environment with A2A (Agent-to-Agent) protocol support, distributed state persistence, and session management.

## Architecture

This package bridges LangGraph's graph-based agent framework with Kagent's infrastructure by:
- Implementing a custom checkpointer for remote state persistence via Kagent API
- Converting LangGraph execution events to A2A protocol events
- Managing sessions through Kagent's REST API
- Providing FastAPI-based HTTP servers for agent endpoints
- Supporting distributed graph execution with checkpoint recovery

## Key Components

### Core Classes

#### KAgentApp (`_a2a.py`)
- **Purpose**: Main application builder for Kagent LangGraph agents
- **Key Method**: `build()` - Creates a FastAPI app for Kubernetes deployment (lines 75-119)
- **Features**:
  - A2A protocol server setup
  - Integrates with Kagent API for task and session management
  - Health check endpoint at `/health`
  - Thread dump endpoint at `/thread_dump` for debugging
  - OpenTelemetry tracing configuration via `kagent.core.configure_tracing()`
- **Constructor Parameters**:
  - `graph`: Pre-compiled `CompiledStateGraph` from LangGraph
  - `agent_card`: A2A protocol agent metadata
  - `config`: `KAgentConfig` with URL and app name
  - `executor_config`: Optional `LangGraphAgentExecutorConfig`
  - `tracing`: Enable/disable OpenTelemetry tracing (default: True)

#### KAgentCheckpointer (`_checkpointer.py`)
- **Purpose**: Remote checkpointer for LangGraph state persistence
- **Extends**: `BaseCheckpointSaver[str]` from LangGraph
- **Key Features**:
  - Stores LangGraph checkpoints in Kagent via REST API
  - Enables distributed execution and session recovery
  - Supports checkpoint history traversal
  - Handles pending writes and channel versions
- **Key Methods**:
  - `aget()`: Retrieve latest checkpoint (async)
  - `aget_tuple()`: Retrieve specific checkpoint tuple
  - `alist()`: List checkpoint history
  - `aput()`: Store new checkpoint
  - `aput_writes()`: Store pending writes
- **API Endpoints Used**:
  - GET `/api/checkpoints?thread_id={id}` - List checkpoints
  - POST `/api/checkpoints` - Create checkpoint
  - POST `/api/checkpoint_writes` - Store pending writes
- **Serialization**:
  - Uses `JsonPlusSerializer` for checkpoint data
  - Base64 encoding for binary data (lines 34-50: payload models)
  - UTF-8 string representation for API transport

#### LangGraphAgentExecutor (`_executor.py`)
- **Purpose**: Executes LangGraph workflows in response to A2A requests
- **Extends**: `AgentExecutor` from A2A server
- **Key Features**:
  - Converts A2A requests to LangGraph inputs
  - Streams LangGraph events as A2A events
  - Handles session management and checkpoint configuration
  - Timeout management for long-running graphs
- **Configuration** (`LangGraphAgentExecutorConfig`):
  - `execution_timeout`: Max execution time (default: 300s)
  - `enable_streaming`: Stream intermediate results (default: True)
- **Workflow** (execute method):
  1. Extract session info from request context (lines 74-100)
  2. Build LangGraph config with thread_id, metadata, tags
  3. Stream graph execution events
  4. Convert events to A2A format via `_convert_langgraph_event_to_a2a()`
  5. Aggregate final result
  6. Handle errors and timeouts
- **Context Mapping**:
  - A2A session_id → LangGraph thread_id
  - A2A context_id → LangGraph project metadata
  - A2A task_id → LangGraph tags

### Converters (`_converters.py`)

- **Purpose**: Converts LangGraph execution events to A2A protocol events
- **Function**: `_convert_langgraph_event_to_a2a()`
- **Handles**:
  - Node execution events
  - Message updates
  - Tool calls and results
  - Error events
  - Custom events

## Data Models

### Checkpoint Payloads

#### KAgentCheckpointPayload
- Represents a single checkpoint stored in Kagent
- Fields: thread_id, checkpoint_ns, checkpoint_id, parent_checkpoint_id, checkpoint (serialized), metadata, type, version

#### KagentCheckpointWrite
- Represents a pending write operation
- Fields: idx, channel, type, value (serialized)

#### KAgentCheckpointWritePayload
- Batch of pending writes for a checkpoint
- Fields: thread_id, checkpoint_ns, checkpoint_id, task_id, writes

#### KAgentCheckpointTuple
- Complete checkpoint with associated writes
- Combines checkpoint data with pending writes

## Integration Points

### Kagent API Integration
The package integrates with the Kagent controller via HTTP API:
- **Base URL**: Configured via `KAgentConfig.url`
- **Checkpointing**: POST/GET `/api/checkpoints`, POST `/api/checkpoint_writes`
- **Task Management**: Uses `KAgentTaskStore` for A2A task lifecycle
- **Session Mapping**: A2A sessions map to LangGraph thread_ids

### A2A Protocol Integration
- Uses `A2AStarletteApplication` for A2A protocol server
- Implements `DefaultRequestHandler` with custom executor
- Custom request context builder: `KAgentRequestContextBuilder`
- Task store: `KAgentTaskStore` (integrates with Kagent API)

### LangGraph Integration
- Uses compiled `CompiledStateGraph` from LangGraph
- Custom `BaseCheckpointSaver` implementation for remote persistence
- Streams graph execution events via LangGraph's event system
- Supports all LangGraph features: state persistence, branching, human-in-the-loop

### Tracing Integration
- Optional OpenTelemetry tracing via `kagent.core.configure_tracing()`
- LangGraph execution metadata includes:
  - App name, context_id, task_id, session_id
  - Project name and run name
  - Tags for filtering traces

## Deployment Modes

### Kubernetes Mode
- **Purpose**: Production deployment in Kagent clusters
- **Features**:
  - Full Kagent API integration
  - Remote checkpoint persistence
  - Multi-user support
  - Distributed execution
  - Health checks and monitoring
- **Configuration**: Via environment variables and Kagent CRDs

### Local Mode (Future)
Currently, the package focuses on Kubernetes deployment. Local development mode with in-memory checkpointing could be added similar to `kagent-adk`.

## Dependencies

### External
- **langgraph**: LangGraph framework for agent workflows
- **langchain-core**: LangChain core components
- **a2a.server**: A2A protocol server implementation
- **fastapi**: HTTP server framework
- **httpx**: Async HTTP client
- **pydantic**: Data validation
- **opentelemetry**: Distributed tracing (optional)

### Internal
- **kagent.core**: Core Kagent utilities (A2A helpers, task store, config, tracing)

## Usage Example

```python
from kagent.langgraph import KAgentApp, KAgentCheckpointer
from kagent.core import KAgentConfig
from langgraph.graph import StateGraph
from a2a.types import AgentCard
import httpx

# Define your LangGraph state and workflow
class State(TypedDict):
    messages: Annotated[Sequence[BaseMessage], "conversation"]

builder = StateGraph(State)
# Add nodes and edges...
builder.add_node("agent", agent_node)
builder.add_edge(START, "agent")
builder.add_edge("agent", END)

# Compile with KAgent checkpointer
http_client = httpx.AsyncClient(base_url="http://kagent-controller:8080")
checkpointer = KAgentCheckpointer(
    client=http_client,
    app_name="my-agent"
)
graph = builder.compile(checkpointer=checkpointer)

# Create agent card
agent_card = AgentCard(
    name="my-langgraph-agent",
    description="A LangGraph agent"
)

# Build Kagent app
config = KAgentConfig(
    url="http://kagent-controller:8080",
    app_name="my-agent"
)

kagent_app = KAgentApp(
    graph=graph,
    agent_card=agent_card,
    config=config,
    tracing=True
)

# Get FastAPI app for deployment
app = kagent_app.build()
```

## Key Features

1. **Distributed State Management**: Checkpoints stored remotely in Kagent
2. **Session Recovery**: Resume graph execution from any checkpoint
3. **A2A Protocol**: Full compatibility with Kagent's agent-to-agent communication
4. **Event Streaming**: Real-time streaming of graph execution
5. **Observability**: OpenTelemetry tracing with rich metadata
6. **Flexible Graphs**: Support for any LangGraph workflow
7. **Checkpoint History**: Navigate through checkpoint timelines
8. **Error Handling**: Graceful error propagation through A2A protocol

## Comparison with kagent-adk

| Feature | kagent-adk | kagent-langgraph |
|---------|------------|------------------|
| AI Framework | Google ADK | LangGraph |
| State Management | ADK Sessions | LangGraph Checkpoints |
| Primary Use Case | ADK agent apps | Graph-based workflows |
| State Store | Session API | Checkpoint API |
| Local Mode | Yes (in-memory sessions) | Not yet implemented |
| STS Integration | Yes | Via kagent.core |
| Event System | ADK events | LangGraph events |

## API Endpoints

### Checkpoint Management
- `GET /api/checkpoints?thread_id={id}` - List checkpoints for a thread
- `POST /api/checkpoints` - Create/store new checkpoint
- `POST /api/checkpoint_writes` - Store pending writes

### A2A Protocol (inherited from A2AStarletteApplication)
- Standard A2A endpoints for task execution
- Task streaming and status updates
- Agent card discovery

## Configuration

### KAgentConfig
- `url`: Kagent API endpoint
- `app_name`: Application name for identification

### LangGraphAgentExecutorConfig
- `execution_timeout`: Maximum execution time (default: 300s)
- `enable_streaming`: Enable event streaming (default: True)

## Related Components

- **Kagent Controller** (`go/internal/controller/`): Kubernetes controller managing agent lifecycle
- **Kagent Core** (`python/packages/kagent-core/`): Shared Python utilities
- **ADK Integration** (`python/packages/kagent-adk/`): Alternative using Google ADK
- **CrewAI Integration** (`python/packages/kagent-crewai/`): Multi-agent alternative

## Package Structure

```
kagent-langgraph/
├── src/kagent/langgraph/
│   ├── __init__.py           # Main exports (KAgentApp, KAgentCheckpointer, LangGraphAgentExecutor)
│   ├── _a2a.py               # FastAPI app builder (lines 42-119)
│   ├── _checkpointer.py      # Remote checkpointer (lines 75-100+)
│   ├── _executor.py          # LangGraph executor (lines 48-100+)
│   └── _converters.py        # LangGraph → A2A event conversion
└── README.md                 # Package documentation
```

## Best Practices

1. **Compile Graphs First**: Always compile your LangGraph with the KAgentCheckpointer before passing to KAgentApp
2. **Session IDs**: Ensure consistent session_id → thread_id mapping for proper state recovery
3. **Timeout Configuration**: Set appropriate `execution_timeout` for long-running graphs
4. **Error Handling**: Implement proper error nodes in your graph for graceful failures
5. **Checkpoint Frequency**: LangGraph automatically checkpoints after each node - balance between recoverability and performance
6. **Tracing**: Enable tracing in production for debugging distributed executions

## Future Enhancements

- **Local Development Mode**: In-memory checkpointer for local testing
- **Checkpoint Pruning**: Automatic cleanup of old checkpoints
- **Multi-Graph Support**: Run multiple graphs within single app
- **Advanced Streaming**: Fine-grained control over streaming behavior
- **Checkpoint Compression**: Reduce storage size for large states
