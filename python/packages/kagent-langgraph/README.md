# KAgent LangGraph Integration

This package provides LangGraph integration for KAgent with A2A (Agent-to-Agent) server support. It implements a custom checkpointer that persists LangGraph state through the KAgent generated gRPC API, enabling distributed agent execution with session persistence.

## Features

- **Custom Checkpointer**: Persists LangGraph checkpoints through generated gRPC clients
- **A2A Server Integration**: Compatible with KAgent's Agent-to-Agent protocol
- **Session Management**: Automatic session creation and state persistence
- **Event Streaming**: Real-time streaming of graph execution events
- **FastAPI Integration**: Ready-to-deploy web server for agent execution

## Quick Start

```python
from kagent.core import AsyncControllerClient, AsyncFileTokenProvider, KAgentConfig
from kagent.langgraph import KAgentApp, KAgentCheckpointer
from langgraph.graph import StateGraph
from langchain_core.messages import BaseMessage
from typing import TypedDict, Annotated, Sequence

class State(TypedDict):
    messages: Annotated[Sequence[BaseMessage], "The conversation history"]

config = KAgentConfig()
controller_client = AsyncControllerClient(
    config.grpc_url,
    agent_name=config.app_name,
    token_provider=AsyncFileTokenProvider(),
)

# Define and compile your graph
builder = StateGraph(State)
# Add nodes and edges...
graph = builder.compile(
    checkpointer=KAgentCheckpointer(
        client=controller_client,
        app_name=config.app_name,
    )
)

# Create KAgent app
app = KAgentApp(
    graph=graph,
    agent_card={
        "name": "my-langgraph-agent",
        "description": "A LangGraph agent with KAgent integration",
        "version": "0.1.0",
        "capabilities": {"streaming": True},
        "defaultInputModes": ["text"],
        "defaultOutputModes": ["text"]
    },
    config=config,
    controller_client=controller_client,
)

# Build FastAPI application
fastapi_app = app.build()
```

## Architecture

The package mirrors the structure of `kagent-adk` but uses LangGraph instead of Google's ADK:

- **KAgentCheckpointer**: Custom checkpointer that stores graph state in KAgent sessions
- **LangGraphAgentExecutor**: Executes LangGraph workflows within A2A protocol
- **KAgentApp**: FastAPI application builder with A2A integration
- **Session Management**: Automatic task and checkpoint persistence through one shared authenticated gRPC channel

## Configuration

Set both controller endpoints when running locally. `KAGENT_URL` remains the HTTP base URL for protocol traffic, while application persistence uses `KAGENT_GRPC_URL` independently.

```bash
export KAGENT_URL=http://localhost:8083
export KAGENT_GRPC_URL=localhost:8084
export KAGENT_NAME=my-agent
export KAGENT_NAMESPACE=default
```

## Deployment

Use the same deployment pattern as kagent-adk samples with Docker and Kubernetes.
