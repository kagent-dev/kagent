# Kagent ADK Integration

## Overview

The `kagent-adk` package provides integration between Google's Agent Development Kit (ADK) and the Kagent framework. It enables ADK-based agents to run within Kagent's Kubernetes-native environment with A2A (Agent-to-Agent) protocol support, session management, and multi-provider LLM capabilities.

## Architecture

This package bridges ADK's agent runtime with Kagent's infrastructure by:
- Converting A2A protocol requests/events to ADK format
- Managing sessions through Kagent's API
- Providing FastAPI-based HTTP servers for agent endpoints
- Supporting multiple LLM providers (OpenAI, Anthropic, Google Vertex AI, Azure)
- Integrating MCP (Model Context Protocol) tools
- Supporting STS (Stateless Transaction Service) token propagation

## Key Components

### Core Classes

#### KAgentApp (`_a2a.py`)
- **Purpose**: Main application builder for Kagent ADK agents
- **Key Methods**:
  - `build()`: Creates a FastAPI app for Kubernetes deployment (lines 73-119)
  - `build_local()`: Creates a FastAPI app for local development (lines 121-156)
- **Features**:
  - Integrates with Kagent API for session management
  - Supports STS token propagation when `STS_WELL_KNOWN_URI` is set
  - Health check endpoint at `/health`
  - Thread dump endpoint at `/thread_dump` for debugging
  - A2A protocol server setup
- **Environment Variables**:
  - `KAGENT_URL`: Override for Kagent API endpoint
  - `STS_WELL_KNOWN_URI`: Enable STS integration
  - `LOG_LEVEL`: Logging level (default: INFO)

#### A2aAgentExecutor (`_agent_executor.py`)
- **Purpose**: Executes ADK agents in response to A2A requests
- **Key Features**:
  - Converts A2A requests to ADK run arguments
  - Streams ADK events as A2A events
  - Supports runner cleanup after execution
  - Handles both sync and async runner factories
- **Flow** (lines 93-97):
  1. Receives A2A request with context and event queue
  2. Converts request to ADK format
  3. Runs ADK agent
  4. Streams events back through A2A event queue
  5. Aggregates final result
- **OpenTelemetry**: Integrated tracing support

#### KAgentSessionService (`_session_service.py`)
- **Purpose**: Session persistence through Kagent API
- **Interface**: Implements ADK's `BaseSessionService`
- **API Methods**:
  - `create_session()`: POST `/api/sessions` (lines 28-61)
  - `get_session()`: GET `/api/sessions/{id}` (lines 64-100+)
  - `delete_session()`: DELETE `/api/sessions/{id}`
  - `list_sessions()`: GET `/api/sessions`
  - `update_session()`: PATCH `/api/sessions/{id}`
  - `append_events()`: POST session events
- **Features**:
  - User-based session isolation via `X-User-ID` header
  - Event history retrieval with pagination
  - Session name metadata support

#### KAgentTokenService (`_token.py`)
- **Purpose**: Manages authentication tokens for Kagent API calls
- **Features**:
  - Agent identity token injection
  - FastAPI lifespan management
  - HTTP client event hooks for automatic token injection

### Protocol Converters

Located in `converters/`:

#### EventConverter (`event_converter.py`)
- **Purpose**: Converts ADK events to A2A events
- **Function**: `convert_event_to_a2a_events()`
- **Supported Events**:
  - Task state changes
  - Artifact updates
  - Agent messages
  - Tool calls and results
  - Custom events

#### RequestConverter (`request_converter.py`)
- **Purpose**: Converts A2A requests to ADK run arguments
- **Function**: `convert_a2a_request_to_adk_run_args()`
- **Converts**:
  - Message history
  - User input
  - Configuration
  - Context metadata

#### PartConverter (`part_converter.py`)
- **Purpose**: Converts between A2A and ADK content parts
- **Handles**:
  - Text parts
  - Tool calls
  - Tool results
  - Artifacts

#### ErrorMappings (`error_mappings.py`)
- **Purpose**: Maps ADK error types to A2A error codes
- **Supports**: Standard error categorization for protocol compatibility

### LLM Model Support

#### OpenAI Models (`models/_openai.py`)
- **Custom Models**: Extended OpenAI model registry
- **Supported Providers**:
  - OpenAI (GPT-4, GPT-3.5)
  - Azure OpenAI
  - Anthropic (Claude models via OpenAI-compatible API)
  - DeepSeek
  - Custom endpoints
- **Features**: Cost tracking, token counting, custom model registration

### CLI Tool (`cli.py`)
- **Purpose**: Command-line interface for testing ADK agents locally
- **Features**:
  - Interactive REPL for agent conversations
  - Session management
  - Local development mode

### Types (`types.py`)
- **AgentConfig**: Configuration schema for ADK agent initialization
- **Pydantic Models**: Type-safe configuration handling

## Integration Points

### Kagent API Integration
The package integrates with the Kagent controller via HTTP API:
- **Base URL**: Environment variable `KAGENT_URL` or constructor parameter
- **Authentication**: Agent tokens via `KAgentTokenService`
- **Session Management**: All session operations proxy to Kagent API
- **Task Management**: Uses `KAgentTaskStore` for A2A task lifecycle

### A2A Protocol Integration
- Uses `@a2a-js/sdk` for A2A protocol server implementation
- Implements `A2AFastAPIApplication` for HTTP endpoints
- Custom request context builder: `KAgentRequestContextBuilder`
- Task store: `KAgentTaskStore` (integrates with Kagent API)

### STS Integration
Optional Stateless Transaction Service support:
- Configured via `STS_WELL_KNOWN_URI` environment variable
- Uses `agentsts.adk` library for token propagation
- Enables secure cross-service communication

### ADK Integration
- Extends Google ADK's `BaseAgent` and `Runner` classes
- Implements ADK session service interface
- Uses ADK's event system for streaming
- Supports ADK plugins (STS token propagation)

## Deployment Modes

### Kubernetes Mode (`build()`)
- **Purpose**: Production deployment in Kagent clusters
- **Features**:
  - Full Kagent API integration
  - Remote session persistence
  - Multi-user support
  - STS token propagation
  - Health checks and monitoring
- **Configuration**: Via environment variables and Kagent CRDs

### Local Mode (`build_local()`)
- **Purpose**: Local development and testing
- **Features**:
  - In-memory session storage
  - No Kagent API dependency
  - Simple FastAPI server
  - Single-user mode
- **Use Case**: Development, testing, debugging

## Dependencies

### External
- **google.adk**: Google Agent Development Kit
- **a2a.server**: A2A protocol server implementation
- **fastapi**: HTTP server framework
- **httpx**: Async HTTP client
- **pydantic**: Data validation
- **opentelemetry**: Distributed tracing
- **agentsts**: STS token service integration

### Internal
- **kagent.core**: Core Kagent utilities (A2A helpers, task store)

## Testing

Located in `tests/`:
- **Unit Tests**: `unittests/` directory
  - Converter tests: A2A ↔ ADK conversion validation
  - Model tests: Custom OpenAI model registration
- **Test Framework**: pytest with async support

## Usage Example

```python
from kagent.adk import KAgentApp, AgentConfig
from a2a.types import AgentCard
from google.adk.agents import BaseAgent

# Define your ADK agent
agent = BaseAgent(...)

# Create agent card for A2A protocol
agent_card = AgentCard(
    name="my-agent",
    description="My ADK agent"
)

# Build Kagent app
kagent_app = KAgentApp(
    root_agent=agent,
    agent_card=agent_card,
    kagent_url="http://kagent-controller:8080",
    app_name="my-agent"
)

# For Kubernetes deployment
app = kagent_app.build()

# For local development
# app = kagent_app.build_local()
```

## Key Features

1. **Multi-Provider Support**: OpenAI, Anthropic, Google Vertex AI, Azure, custom endpoints
2. **Protocol Translation**: Seamless A2A ↔ ADK conversion
3. **Session Persistence**: Distributed session management via Kagent API
4. **Streaming**: Real-time event streaming for agent responses
5. **Observability**: OpenTelemetry tracing integration
6. **Security**: STS token propagation for service-to-service auth
7. **MCP Tools**: Model Context Protocol for standardized tool integration
8. **Flexible Deployment**: Kubernetes and local modes

## Configuration

### Environment Variables
- `KAGENT_URL`: Kagent API endpoint override
- `STS_WELL_KNOWN_URI`: Enable STS integration
- `LOG_LEVEL`: Logging verbosity (DEBUG, INFO, WARNING, ERROR)

### Agent Configuration
See `types.AgentConfig` for full configuration schema including:
- Agent metadata
- System messages
- Tool configurations
- Model selection
- A2A settings

## Related Components

- **Kagent Controller** (`go/internal/controller/`): Kubernetes controller managing agent lifecycle
- **Kagent Core** (`python/packages/kagent-core/`): Shared Python utilities
- **LangGraph Integration** (`python/packages/kagent-langgraph/`): Alternative framework
- **CrewAI Integration** (`python/packages/kagent-crewai/`): Multi-agent alternative

## Package Structure

```
kagent-adk/
├── src/kagent/adk/
│   ├── __init__.py           # Main exports (KAgentApp, AgentConfig)
│   ├── _a2a.py               # FastAPI app builder (lines 60-156)
│   ├── _agent_executor.py    # ADK agent execution (lines 49-97)
│   ├── _session_service.py   # Session API client (lines 17-100+)
│   ├── _token.py             # Authentication service
│   ├── cli.py                # CLI tool for local testing
│   ├── types.py              # Configuration types
│   ├── converters/           # A2A ↔ ADK conversion
│   │   ├── event_converter.py
│   │   ├── request_converter.py
│   │   ├── part_converter.py
│   │   └── error_mappings.py
│   └── models/               # Custom LLM models
│       └── _openai.py        # Extended OpenAI models
└── tests/
    └── unittests/            # Unit test suite
```
