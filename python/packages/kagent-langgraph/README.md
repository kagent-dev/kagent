# KAgent LangGraph Integration

This package provides LangGraph integration for KAgent with A2A (Agent-to-Agent) server support.

## Features

- **A2A Server Integration**: Compatible with KAgent's Agent-to-Agent protocol
- **Event Streaming**: Real-time streaming of graph execution events
- **FastAPI Integration**: Ready-to-deploy web server for agent execution

## Quick Start

```python
import json
from pathlib import Path

from kagent.core import KAgentConfig, materialize_from_bootstrap
from kagent.langgraph import KAgentApp
import os
import sqlite3
from langgraph.checkpoint.sqlite import SqliteSaver
from langgraph.graph import StateGraph
from langchain_core.messages import BaseMessage
from typing import TypedDict, Annotated, Sequence


class State(TypedDict):
    messages: Annotated[Sequence[BaseMessage], "The conversation history"]


materialize_from_bootstrap("/config")
config = KAgentConfig.from_file()

# Define and compile your graph
builder = StateGraph(State)
# Add nodes and edges...
checkpointer = SqliteSaver(
    sqlite3.connect(
        os.getenv("KAGENT_CHECKPOINT_DB", "/tmp/langgraph-checkpoints.sqlite"),
        check_same_thread=False,
    )
)
graph = builder.compile(checkpointer=checkpointer)

# Create KAgent app
app = KAgentApp(
    graph=graph,
    agent_card=json.loads(Path("/config/agent-card.json").read_text()),
    config=config,
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

Set `runtime.control_plane_url` and `runtime.gateway_url` in `/config/config.json` when running locally.

```bash
# Load process settings from /config/config.json (see Configuration).
```

## Deployment

Start Uvicorn on a separate HTTP port such as `8080`. During application startup, `build()` starts the A2A gRPC server on `runtime.port` (default `80`), then serves `GET /readyz` on port `8081` for Substrate.

Use the same deployment pattern as kagent-adk samples with Docker and Kubernetes.

### Runtime configuration file

Managed entry points call `materialize_from_bootstrap("/config")` from `kagent.core`
to fetch and materialize the controller bundle before `KAgentConfig.from_file()`.
The file loader does not bootstrap. For a standalone process, create the file and
load it directly:

```json
{"runtime":{"control_plane_url":"http://localhost:8083","gateway_url":"http://localhost:8083","name":"my-agent","namespace":"default","log_level":"info"}}
```

Provider credentials and standard `OTEL_*` overrides remain environment inputs.
