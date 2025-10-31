# KAgent OpenAI Agents SDK Integration

This package provides OpenAI Agents SDK integration for KAgent with A2A (Agent-to-Agent) server support. It implements session management, event streaming, and seamless integration with the KAgent platform.

## Features

- **A2A Server Integration**: Compatible with KAgent's Agent-to-Agent protocol
- **Session Management**: Persistent conversation history via KAgent REST API
- **Event Streaming**: Real-time streaming of agent execution events
- **FastAPI Integration**: Ready-to-deploy web server for agent execution
- **Skills Support**: Compatible with Anthropic's Agent Skills specification
- **File Tools**: Optional file operation tools for agents

## Quick Start

```python
from kagent.openai import KAgentApp
from agents.agent import Agent

# Create your OpenAI agent
agent = Agent(
    name="Assistant",
    instructions="You are a helpful assistant that answers questions concisely.",
)

# Create KAgent app
app = KAgentApp(
    agent=agent,
    agent_card={
        "name": "my-openai-agent",
        "description": "An OpenAI agent with KAgent integration",
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

# Run with uvicorn
if __name__ == "__main__":
    import uvicorn
    uvicorn.run(fastapi_app, host="0.0.0.0", port=8000)
```

## Architecture

The package mirrors the structure of `kagent-adk`, `kagent-langgraph`, and `kagent-crewai`:

- **KAgentSession**: Implements OpenAI SDK's `SessionABC` protocol, storing session data in KAgent backend
- **OpenAIAgentExecutor**: Executes OpenAI agents within A2A protocol with streaming support
- **KAgentApp**: FastAPI application builder with A2A integration
- **Event Converters**: Translates OpenAI streaming events into A2A events

## Session Management

The integration uses OpenAI's session interface with KAgent backend persistence:

```python
from agents.agent import Agent
from agents.run import Runner
from kagent.openai.agent._session_service import KAgentSession
import httpx

# Create HTTP client
client = httpx.AsyncClient(base_url="http://localhost:8080")

# Create a session
session = KAgentSession(
    session_id="conversation_123",
    client=client,
    app_name="my-agent",
    user_id="user@example.com"
)

# Use with OpenAI Runner
agent = Agent(name="Assistant", instructions="Be helpful")
result = await Runner.run(agent, "Hello!", session=session)
```

Sessions automatically:
- Store conversation history in KAgent backend
- Maintain context across multiple turns
- Support session lifecycle management (create, get, delete)

## Agent Configuration

### Basic Agent

```python
from agents.agent import Agent

agent = Agent(
    name="Assistant",
    instructions="You are a helpful assistant.",
)
```

### Agent with Tools

```python
from agents.agent import Agent
from agents.tool import function_tool

@function_tool
def get_weather(location: str) -> str:
    """Get the weather for a location."""
    return f"The weather in {location} is sunny."

agent = Agent(
    name="WeatherBot",
    instructions="Help users with weather information.",
    tools=[get_weather]
)
```

### Agent with Skills

```python
from agents.agent import Agent
from kagent.openai.agent.skills import SkillRegistry, get_skill_tool

# Create skills registry
registry = SkillRegistry()
registry.register_skill_directory("./my-skills")

# Get skill tool
skill_tool = get_skill_tool(registry)

agent = Agent(
    name="SkillfulAgent",
    instructions="Use skills when appropriate.",
    tools=[skill_tool]
)
```

## Using Agent Factories

For dynamic agent configuration, use factory functions:

```python
from agents.agent import Agent

def create_agent():
    """Factory function that creates a new agent instance."""
    return Agent(
        name="DynamicAgent",
        instructions="Dynamically configured agent",
    )

app = KAgentApp(
    agent=create_agent,  # Pass factory function
    agent_card=agent_card,
    kagent_url="http://localhost:8080",
    app_name="dynamic-agent"
)
```

## Local Development

For local testing without KAgent backend:

```python
app = KAgentApp(
    agent=my_agent,
    agent_card=agent_card,
    kagent_url="http://localhost:8080",
    app_name="test-agent"
)

# Use build_local() for in-memory operation
fastapi_app = app.build_local()
```

This mode:
- Uses in-memory task store (no backend required)
- Doesn't persist sessions
- Useful for development and testing

## Testing Your Agent

```python
import asyncio
from kagent.openai import KAgentApp
from agents.agent import Agent

agent = Agent(name="Assistant", instructions="Be helpful")

app = KAgentApp(
    agent=agent,
    agent_card={...},
    kagent_url="http://localhost:8080",
    app_name="test-agent"
)

# Test the agent directly
asyncio.run(app.test("What is 2+2?"))
```

## Configuration

The system uses these REST API endpoints:

- `POST /api/sessions` - Create new sessions
- `GET /api/sessions/{id}` - Retrieve session and events  
- `POST /api/sessions/{id}/events` - Append session events
- `DELETE /api/sessions/{id}` - Delete session
- `POST /api/tasks` - Task management

## Environment Variables

- `LOG_LEVEL`: Logging level (default: INFO)
- `KAGENT_URL`: Override KAgent backend URL
- `STS_WELL_KNOWN_URI`: STS well-known URI for token exchange

## Skills and Tools

The package includes two subpackages for agent capabilities:

### Skills (`kagent.openai.agent.skills`)

Implements Anthropic's Agent Skills specification:

```python
from kagent.openai.agent.skills import SkillRegistry, get_skill_tool

registry = SkillRegistry()
registry.register_skill_directory("./skills")
skill_tool = get_skill_tool(registry)

agent = Agent(name="Agent", tools=[skill_tool])
```

See the [skills tests](tests/unittests/skills/test_skills.py) for examples.

### Tools (`kagent.openai.agent.tools`)

File operation tools modeled after Claude Code:

```python
from kagent.openai.agent.tools import (
    READ_FILE_TOOL,
    WRITE_FILE_TOOL,
    EDIT_FILE_TOOL,
    SRT_SHELL_TOOL,
)

agent = Agent(
    name="CodeAgent",
    tools=[READ_FILE_TOOL, WRITE_FILE_TOOL, EDIT_FILE_TOOL]
)
```

**Note**: Tools are NOT automatically included. Add them explicitly if needed.

## Deployment

Use the same deployment pattern as other KAgent samples:

1. **Docker**: Build container with your agent
2. **Kubernetes**: Deploy with Helm chart
3. **Configure**: Set `KAGENT_URL` environment variable

Example Dockerfile:

```dockerfile
FROM python:3.13-slim

WORKDIR /app

# Install dependencies
COPY requirements.txt .
RUN pip install -r requirements.txt

# Copy agent code
COPY my_agent.py .

# Run
CMD ["uvicorn", "my_agent:app", "--host", "0.0.0.0", "--port", "8000"]
```

## Comparison with Other Integrations

| Feature | kagent-openai | kagent-adk | kagent-langgraph | kagent-crewai |
|---------|--------------|------------|------------------|---------------|
| Framework | OpenAI Agents SDK | Google ADK | LangGraph | CrewAI |
| Sessions | KAgentSession | KAgentSessionService | KAgentCheckpointer | Memory Storage |
| Streaming | ✅ Built-in | ✅ Built-in | ✅ astream | ✅ Listeners |
| Skills | ✅ Optional | ✅ Optional | ❌ | ❌ |
| HITL | ❌ | ✅ | ✅ | ❌ |

## API Reference

### KAgentApp

Main application builder class.

**Methods:**
- `build()`: Build production FastAPI app with KAgent backend
- `build_local()`: Build local FastAPI app for testing
- `test(task: str)`: Test agent with a simple query

### KAgentSession

Session implementation for OpenAI Agents SDK.

**Methods:**
- `get_items(limit)`: Retrieve conversation history
- `add_items(items)`: Store new conversation items
- `pop_item()`: Remove and return most recent item
- `clear_session()`: Delete all session data

### OpenAIAgentExecutor

Agent executor for A2A protocol.

**Methods:**
- `execute(context, event_queue)`: Execute agent and stream events
- `cancel(context, event_queue)`: Cancel execution (not implemented)

## Troubleshooting

### Session not persisting

Ensure KAgent backend is accessible:
```python
# Check backend connectivity
import httpx
client = httpx.AsyncClient(base_url="http://localhost:8080")
response = await client.get("/health")
print(response.status_code)  # Should be 200
```

### Streaming not working

Verify streaming is enabled:
```python
from kagent.openai._agent_executor import OpenAIAgentExecutorConfig

config = OpenAIAgentExecutorConfig(enable_streaming=True)
app = KAgentApp(agent=agent, config=config, ...)
```

### Import errors

Make sure all dependencies are installed:
```bash
pip install kagent-openai
```

## Examples

See the `samples/openai/` directory for complete examples:
- Basic agent
- Agent with tools
- Agent with skills
- Multi-agent workflows

## Development

Run tests:
```bash
cd python/packages/kagent-openai
pytest tests/
```

## License

See the root repository LICENSE file.

## Contributing

Contributions welcome! Please follow the existing code style and include tests for new features.

