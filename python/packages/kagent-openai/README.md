# KAgent OpenAI Agents SDK Integration

OpenAI Agents SDK integration for KAgent with A2A (Agent-to-Agent) protocol support, session management, and optional skills integration.

---

## Quick Start

```python
from kagent.openai import KAgentApp
from agents.agent import Agent

# Create your OpenAI agent
agent = Agent(
    name="Assistant",
    instructions="You are a helpful assistant.",
    tools=[my_tool],  # Optional
)

# Create KAgent app
app = KAgentApp(
    agent=agent,
    agent_card={
        "name": "my-openai-agent",
        "description": "My OpenAI agent",
        "version": "0.1.0",
        "capabilities": {"streaming": True},
        "defaultInputModes": ["text"],
        "defaultOutputModes": ["text"]
    },
    kagent_url="http://localhost:8080",
    app_name="my-agent"
)

# Run
fastapi_app = app.build()
# uvicorn run_me:fastapi_app
```

---

## Agent with Skills

Skills provide domain expertise through filesystem-based instruction files and helper tools (read/write/edit files, bash execution). We provide a function to load all skill-related tools. Otherwise, you can select the ones you need by importing from `kagent.openai.tools`.

```python
from agents.agent import Agent
from kagent.openai import get_skill_tools

tools = [my_custom_tool]
tools.extend(get_skill_tools("./skills"))

agent = Agent(
    name="SkillfulAgent",
    instructions="Use skills and tools when appropriate.",
    tools=tools,
)
```

See [skills README](../../kagent-skills/README.md) for skill format and structure.

---

## Session Management

Sessions persist conversation history in KAgent backend:

```python
from agents.agent import Agent
from agents.run import Runner
from kagent.openai.agent._session_service import KAgentSession
import httpx

client = httpx.AsyncClient(base_url="http://localhost:8080")
session = KAgentSession(
    session_id="conversation_123",
    client=client,
    app_name="my-agent",
)

agent = Agent(name="Assistant", instructions="Be helpful")
result = await Runner.run(agent, "Hello!", session=session)
```

---

## Local Development

Test without KAgent backend using in-memory mode:

```python
app = KAgentApp(
    agent=agent,
    agent_card=agent_card,
    kagent_url="http://localhost:8080",
    app_name="test-agent"
)

fastapi_app = app.build_local()  # In-memory, no persistence
```

---

## Deployment

Standard Docker deployment:

```dockerfile
FROM python:3.13-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install -r requirements.txt
COPY agent.py .
CMD ["uvicorn", "agent:fastapi_app", "--host", "0.0.0.0", "--port", "8000"]
```

Set `KAGENT_URL` environment variable to connect to KAgent backend.

---

## Architecture

| Component               | Purpose                                      |
| ----------------------- | -------------------------------------------- |
| **KAgentApp**           | FastAPI application builder with A2A support |
| **KAgentSession**       | Session persistence via KAgent REST API      |
| **OpenAIAgentExecutor** | Executes agents with event streaming         |

---

## Environment Variables

- `KAGENT_URL` - KAgent backend URL (default: http://localhost:8080)
- `LOG_LEVEL` - Logging level (default: INFO)

---

## Examples

See `samples/openai/` for complete examples:

- `basic_agent/` - Simple agent with custom tools
- More examples coming soon

---

## See Also

- [OpenAI Agents SDK Docs](https://github.com/openai/agents)
- [KAgent Skills](../../kagent-skills/README.md)
- [A2A Protocol](https://docs.kagent.ai/a2a)

---

## License

See repository LICENSE file.
