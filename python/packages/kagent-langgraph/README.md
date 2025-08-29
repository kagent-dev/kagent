# KAgent LangGraph Integration

This package provides LangGraph integration for KAgent with A2A (Agent-to-Agent) server support. It implements a custom checkpointer that persists LangGraph state to the KAgent REST API, enabling distributed agent execution with session persistence.

## Features

- **Custom Checkpointer**: Persists LangGraph checkpoints to KAgent via REST API
- **A2A Server Integration**: Compatible with KAgent's Agent-to-Agent protocol
- **Session Management**: Automatic session creation and state persistence
- **Event Streaming**: Real-time streaming of graph execution events
- **FastAPI Integration**: Ready-to-deploy web server for agent execution

## Quick Start

```python
from kagent_langgraph import KAgentApp
from langgraph.graph import StateGraph
from langchain_core.messages import BaseMessage
from typing import TypedDict, Annotated, Sequence

class State(TypedDict):
    messages: Annotated[Sequence[BaseMessage], "The conversation history"]

# Define your graph
builder = StateGraph(State)
# Add nodes and edges...

# Create KAgent app
app = KAgentApp(
    graph_builder=builder,
    agent_card={
        "name": "my-langgraph-agent",
        "description": "A LangGraph agent with KAgent integration",
        "version": "0.1.0",
        "capabilities": {"streaming": True},
        "defaultInputModes": ["text"],
        "defaultOutputModes": ["text"]
    },
    kagent_url="http://localhost:8080",
    app_name="my-agent"
)

# Build FastAPI application
fastapi_app = app.build()
```

## Architecture

The package mirrors the structure of `kagent-adk` but uses LangGraph instead of Google's ADK:

- **KAgentCheckpointer**: Custom checkpointer that stores graph state in KAgent sessions
- **LangGraphAgentExecutor**: Executes LangGraph workflows within A2A protocol
- **KAgentApp**: FastAPI application builder with A2A integration
- **Session Management**: Automatic session lifecycle management via KAgent REST API

## Configuration

The system uses the same REST API endpoints as the ADK integration:

- `POST /api/sessions` - Create new sessions
- `GET /api/sessions/{id}` - Retrieve session and events
- `POST /api/sessions/{id}/events` - Append checkpoint events
- `POST /api/tasks` - Task management

## Deployment

Use the same deployment pattern as kagent-adk samples with Docker and Kubernetes.
