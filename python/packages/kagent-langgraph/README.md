# KAgent LangGraph Integration

This package provides LangGraph integration for KAgent with A2A (Agent-to-Agent) server support.

## Features

- **A2A Server Integration**: Serves LangGraph workflows over A2A
- **Event Streaming**: Streams graph execution events
- **FastAPI Integration**: Builds a deployable FastAPI application

## Quick Start

```python
import os
import sqlite3
from typing import Annotated, Sequence, TypedDict

from kagent.core import KAgentConfig
from kagent.langgraph import KAgentApp
from langchain_core.messages import BaseMessage
from langgraph.checkpoint.sqlite import SqliteSaver
from langgraph.graph import StateGraph


class State(TypedDict):
    messages: Annotated[Sequence[BaseMessage], "The conversation history"]


builder = StateGraph(State)
# Add nodes and edges...
checkpointer = SqliteSaver(
    sqlite3.connect(
        os.getenv("KAGENT_CHECKPOINT_DB", "/tmp/langgraph-checkpoints.sqlite"),
        check_same_thread=False,
    )
)
graph = builder.compile(checkpointer=checkpointer)

app = KAgentApp(
    graph=graph,
    agent_card={
        "name": "my-langgraph-agent",
        "description": "A LangGraph agent with KAgent integration",
        "version": "0.1.0",
        "supportedInterfaces": [{"url": "http://localhost:8080", "protocolBinding": "JSONRPC"}],
        "capabilities": {"streaming": True},
        "defaultInputModes": ["text/plain"],
        "defaultOutputModes": ["text/plain"],
    },
    config=KAgentConfig(),
)

fastapi_app = app.build()
```

## State and Task Storage

The LangGraph checkpointer owns graph conversation state. A SQLite checkpointer stores checkpoints in a local file; persistence across pod replacement requires placing that file on durable storage or selecting another durable LangGraph checkpointer.

`KAgentApp` persists public A2A task history through `KAgentTaskStore` and the
controller gRPC API. This task history is separate from LangGraph conversation
state; configuring one does not make the other durable.

## Architecture

- **LangGraphAgentExecutor**: Executes LangGraph workflows over A2A
- **KAgentApp**: Builds the FastAPI A2A application
- **LangGraph checkpointer**: Stores graph conversation state when configured
- **KAgentTaskStore**: Persists public A2A task history through controller gRPC

## Configuration

`KAgentConfig` requires both endpoint values and the agent identity.
`KAGENT_API_URL` selects the controller gRPC endpoint used for task persistence.
`KAGENT_GATEWAY_URL` is required by the shared configuration but is not used
directly by this wrapper:

```bash
export KAGENT_API_URL=http://localhost:8083
export KAGENT_GATEWAY_URL=http://localhost:8083
export KAGENT_NAME=my-agent
export KAGENT_NAMESPACE=default
```

## Deployment

This package has no documented end-to-end deployment path yet. Sample documentation identifies the validation available for each sample.
