# KAgent CrewAI Integration

This package provides CrewAI integration for KAgent with A2A (Agent-to-Agent) server support.

## Features

- **A2A Server Integration**: Compatible with KAgent's Agent-to-Agent protocol
- **Event Streaming**: Real-time streaming of crew execution events
- **FastAPI Integration**: Ready-to-deploy web server for agent execution

## Quick Start

This package supports both CrewAI Crews and Flows. To get started, define your CrewAI crew or flow as you normally would, then replace the `kickoff` command with the `KAgentApp` which will handle A2A requests and execution.

```python
from kagent.crewai import KAgentApp
from kagent.core import KAgentConfig
# This is the crew or flow you defined
from research_crew.crew import ResearchCrew

app = KAgentApp(crew=ResearchCrew().crew(), agent_card={
    "name": "my-crewai-agent",
    "description": "A CrewAI agent with KAgent integration",
    "version": "0.1.0",
    "capabilities": {"streaming": True},
    "defaultInputModes": ["text"],
    "defaultOutputModes": ["text"]
}, config=KAgentConfig())

fastapi_app = app.build()
uvicorn.run(fastapi_app, host="0.0.0.0", port=8080)
```

## User Guide

### Creating Tasks

For this version, tasks should either accept a single `input` parameter (string) or no parameters at all. Future versions will allow JSON / structured input where you can replace multiple values in your task to make it more flexible.

For example, you can create a task like follow with yaml (see CrewAI docs) and when triggered from the A2A client, the `input` field will be populated with the input text if provided.

```yaml
research_task:
  description: >
    Research topics on {input} and provide a summary.
```

This is equivalent of `crew.kickoff(inputs={"input": "your input text"})` when triggering agents manually.

### Memory and Flow State

#### CrewAI Crews

`KAgentApp` does not configure or persist CrewAI memory. Configure CrewAI memory and its backing storage explicitly when your application needs it.

#### CrewAI Flows

`KAgentApp` creates a Flow instance for each A2A request. It does not persist Flow state or restore it for later requests. Configure persistence in your Flow application when needed.

### Tracing

To enable tracing, follow [this guide](https://kagent.dev/docs/kagent/getting-started/tracing#installing-kagent) on Kagent docs. Once you have Jaeger (or any OTLP-compatible backend) running and the kagent settings updated, your CrewAI agent will automatically send traces to the configured backend.

## Architecture

The package mirrors the structure of `kagent-adk` and `kagent-langgraph` but uses CrewAI for multi-agent orchestration:

- **CrewAIAgentExecutor**: Executes CrewAI workflows within A2A protocol
- **KAgentApp**: FastAPI application builder with A2A integration
- **Event Converters**: Translates CrewAI events into A2A events for streaming.
- **Task store**: Tracks A2A tasks in memory for the lifetime of the application process.

`KAgentConfig` requires its configuration values, but this wrapper does not use `KAGENT_API_URL` or `KAGENT_GATEWAY_URL` for outbound connections. For local development, configure the required values:

```bash
export KAGENT_API_URL=http://localhost:8083
export KAGENT_GATEWAY_URL=http://localhost:8083
export KAGENT_NAME=my-agent
export KAGENT_NAMESPACE=default
```

## Deployment

Use the `samples/crewai/` applications as deployment examples. This package does not provide a Kagent-backed memory or Flow-persistence service.
