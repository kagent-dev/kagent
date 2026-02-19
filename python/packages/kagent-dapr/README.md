# kagent-dapr

Dapr-Agents integration for KAgent. Wraps Dapr-Agents `DurableAgent` and exposes it
via the A2A protocol through FastAPI with durable workflow execution.

## Features

- **DurableAgent Support**: Durable workflow orchestration via Dapr workflow runtime
- **A2A Protocol Compatibility**: Compatible with KAgent's Agent-to-Agent protocol
- **State Persistence**: Durable workflow state via Dapr state stores (Redis, etc.)
- **OpenTelemetry Tracing**: Automatic span attribute injection via kagent-core
- **Debug Endpoints**: Health check and thread dump endpoints for observability

## Quick Start

```python
from dapr_agents import DurableAgent
from kagent.core import KAgentConfig
from kagent.dapr import KAgentApp

agent = DurableAgent(name="my-agent", role="assistant", instructions=["Be helpful."])

agent_card = {
    "name": "my-agent",
    "description": "A Dapr-Agents based agent",
    "url": "http://localhost:8080",
    "version": "0.1.0",
    "skills": [{"id": "chat", "name": "Chat", "description": "General chat"}],
    "capabilities": {},
}

config = KAgentConfig()
app = KAgentApp(agent=agent, agent_card=agent_card, config=config)
fastapi_app = app.build()
```

## Architecture

- **KAgentApp**: FastAPI application builder with A2A route integration
- **DaprDurableAgentExecutor**: Schedules `agent_workflow`, waits for completion, emits A2A events
- **Tracing**: Automatic span attribute injection via kagent-core's span processor

## Configuration

- `KAGENT_URL`: KAgent controller URL (default: `http://kagent-controller.kagent:8083`)
- `PORT`: Server port (default: `8080`)

## Deployment

See `samples/dapr/` for a complete Kubernetes deployment example with Dapr components.
