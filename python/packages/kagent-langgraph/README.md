# KAgent LangGraph Integration

This package provides LangGraph integration for KAgent with A2A (Agent-to-Agent) server support.

## Features

- **A2A Server Integration**: Compatible with KAgent's Agent-to-Agent protocol
- **Event Streaming**: Real-time streaming of graph execution events
- **FastAPI Integration**: Ready-to-deploy web server for agent execution

## Quick Start

```python
from kagent.core import AsyncControllerClient, AsyncFileTokenProvider, KAgentConfig
from kagent.langgraph import KAgentApp
import os
import sqlite3
from langgraph.checkpoint.sqlite import SqliteSaver
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
checkpointer = SqliteSaver(sqlite3.connect(
    os.getenv("KAGENT_CHECKPOINT_DB", "/tmp/langgraph-checkpoints.sqlite"),
    check_same_thread=False,
))
graph = builder.compile(checkpointer=checkpointer)

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

- **LangGraphAgentExecutor**: Executes LangGraph workflows within A2A protocol
- **KAgentApp**: FastAPI application builder with A2A integration
- **Task Management**: Automatic A2A task persistence through one shared authenticated gRPC channel

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
