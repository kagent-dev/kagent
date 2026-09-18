# KAgent LangGraph Integration

This package provides LangGraph integration for KAgent with A2A (Agent-to-Agent) server support.

## Features

- **A2A Server Integration**: Serves LangGraph workflows over A2A
- **Event Streaming**: Streams graph execution events
- **FastAPI Integration**: Builds a deployable FastAPI application

## State and Task Storage

The LangGraph checkpointer owns graph conversation state. A SQLite checkpointer stores checkpoints in a local file; persistence across pod replacement requires placing that file on durable storage or selecting another durable LangGraph checkpointer.

`KAgentApp` uses an in-memory A2A task store inside the agent process. That task state does not survive a process restart. This package does not configure a gateway connection; a deployment may place a gateway in front of the application and have that gateway own durable task history.

## Architecture

- **LangGraphAgentExecutor**: Executes LangGraph workflows over A2A
- **KAgentApp**: Builds the FastAPI A2A application
- **LangGraph checkpointer**: Stores graph conversation state when configured
- **InMemoryTaskStore**: Tracks A2A tasks for the lifetime of the process

## Configuration

`KAgentConfig` currently requires both endpoint values and the agent identity. This wrapper uses the identity and tracing configuration, but does not use these URLs for outbound control-plane or gateway calls:

```bash
export KAGENT_API_URL=http://localhost:8083
export KAGENT_GATEWAY_URL=http://localhost:8083
export KAGENT_NAME=my-agent
export KAGENT_NAMESPACE=default
```

## Deployment

This package has no documented end-to-end deployment path yet. Sample documentation identifies the validation available for each sample.
