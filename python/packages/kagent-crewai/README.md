# KAgent CrewAI Integration

This package provides CrewAI integration for KAgent with A2A (Agent-to-Agent) server support.

## Features

- **A2A Server Integration**: Compatible with KAgent's Agent-to-Agent protocol
- **Event Streaming**: Real-time streaming of crew execution events
- **FastAPI Integration**: Ready-to-deploy web server for agent execution

## Architecture

The package mirrors the structure of `kagent-adk` and `kagent-langgraph` but uses CrewAI for multi-agent orchestration:

- **CrewAIAgentExecutor**: Executes CrewAI workflows within A2A protocol
- **KAgentApp**: FastAPI application builder with A2A integration
- **Event Converters**: Translates CrewAI events into A2A events for streaming.