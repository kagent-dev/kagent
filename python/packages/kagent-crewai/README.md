# KAgent CrewAI Integration

This package provides CrewAI integration for KAgent with A2A (Agent-to-Agent) server support and session-aware memory storage.

## Features

- **A2A Server Integration**: Compatible with KAgent's Agent-to-Agent protocol
- **Event Streaming**: Real-time streaming of crew execution events
- **FastAPI Integration**: Ready-to-deploy web server for agent execution
- **Session-aware Memory**: Store and retrieve agent memories scoped by session ID
- **Flow State Persistence**: Save and restore CrewAI Flow states to KAgent backend

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

### Session-aware Memory

#### CrewAI Crews

Session scoped memory is implemented using the `LongTermMemory` interface in CrewAI. If you wish to share memories between agents, you must interact with them in the same session to share long term memory so they can search and access the previous conversation history (because agent ID is volatile, we must use session ID). You can enable this by setting `memory=True` when creating your CrewAI crew. Note that this memory is also scoped by user ID so different users will not see each other's memories.

Our KAgent backend is designed to handle long term memory saving and retrieval with the identical logic as `LTMSQLiteStorage` which is used by default for `LongTermMemory` in CrewAI, with the addition of session and user scoping. It will search the LTM items based on the task description and return the most relevant items (sorted and limited).

> Note that when you set `memory=True`, you are responsible to ensure that short term and entity memory are configured properly (e.g. with `OPENAI_API_KEY` or set your own providers). The KAgent CrewAI integration only handles long term memory.

#### CrewAI Flows

In flow mode, we implement memory similar to checkpointing in LangGraph so that the flow state is persisted to the KAgent backend after each method finishes execution. We consider each session to be a single flow execution, so you can reuse state within the same session by enabling `@persist()` for flow or methods. We do not manage `LongTermMemory` for crews inside a flow since flow is designed to be very customizable. You are responsible for implementing your own memory management for all the crew you use in the flow.

### Tracing

To enable tracing, follow [this guide](https://kagent.dev/docs/kagent/getting-started/tracing#installing-kagent) on Kagent docs. Once you have Jaeger (or any OTLP-compatible backend) running and the kagent settings updated, your CrewAI agent will automatically send traces to the configured backend.

## Architecture

The package mirrors the structure of `kagent-adk` and `kagent-langgraph` but uses CrewAI for multi-agent orchestration:

- **CrewAIAgentExecutor**: Executes CrewAI workflows within A2A protocol
- **KAgentApp**: FastAPI application builder with A2A integration
- **Event Converters**: Translates CrewAI events into A2A events for streaming.
- **Session-aware Memory**: Custom persistence backend scoped by session ID and user ID, works with Crew and Flow mode by leveraging memory and state persistence.

## Deployment

The uses the same deployment approach as other KAgent A2A applications (ADK / LangGraph). You can refer to `samples/crewai/` for examples.
