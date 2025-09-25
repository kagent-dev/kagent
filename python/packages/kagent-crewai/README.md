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
# This is the crew or flow you defined
from research_crew.crew import ResearchCrew

app = KAgentApp(crew=ResearchCrew().crew(), agent_card={
    "name": "my-crewai-agent",
    "description": "A CrewAI agent with KAgent integration",
    "version": "0.1.0",
    "capabilities": {"streaming": True},
    "defaultInputModes": ["text"],
    "defaultOutputModes": ["text"]
})

fastapi_app = app.build()
uvicorn.run(fastapi_app, host="0.0.0.0", port=8080)
```

## Architecture

The package mirrors the structure of `kagent-adk` and `kagent-langgraph` but uses CrewAI for multi-agent orchestration:

- **CrewAIAgentExecutor**: Executes CrewAI workflows within A2A protocol
- **KAgentApp**: FastAPI application builder with A2A integration
- **Event Converters**: Translates CrewAI events into A2A events for streaming.

## Deployment

The uses the same deployment approach as other KAgent A2A applications (ADK / LangGraph). You can refer to `samples/crewai/` for examples.

## Note

Due to the current design of the package, your tasks in CrewAI should expect a `input` parameter which contains the input text if available. We will support JSON input for more native CrewAI integration in the future. You can check out an example in `samples/crewai/research-crew/src/research_crew/config/tasks.yaml`.
