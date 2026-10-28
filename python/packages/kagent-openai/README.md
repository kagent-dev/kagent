# KAgent OpenAI Agents SDK Integration

OpenAI Agents SDK integration for KAgent with A2A (Agent-to-Agent) protocol support, session management, and optional file and bash tools.

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
        "defaultOutputModes": ["text"],
    },
    kagent_url="http://localhost:8083",
    app_name="my-agent",
)

# Run
fastapi_app = app.build()
# uvicorn run_me:fastapi_app
```

---

## Optional tools

BYO harnesses keep agent behavior in user code. Import the basic filesystem and
bash tools you need from `kagent.openai.tools`:

```python
from agents.agent import Agent
from kagent.openai.tools import bash, edit_file, read_file, write_file

agent = Agent(
    name="Assistant",
    instructions="Use tools when appropriate.",
    tools=[read_file, write_file, edit_file, bash],
)
```

Skill discovery and loading are user-implemented in BYO harnesses.

File and bash tools share `/data/workspace` within an Actor. Substrate snapshots
preserve that directory; applications create any upload or output directories
when needed. See [runtime helpers](../kagent-tools/README.md).

---

## Session Management

Sessions persist conversation history in KAgent backend:

```python
from agents.agent import Agent
from agents.run import Runner
from kagent.openai._session_service import KAgentSession
import httpx

client = httpx.AsyncClient(base_url="http://localhost:8083")
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
app = KAgentApp(agent=agent, agent_card=agent_card, kagent_url="http://localhost:8083", app_name="test-agent")

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

Load `/config/config.json` with `KAgentConfig.from_file()` to connect to kagent.

---

## Architecture

| Component               | Purpose                                      |
| ----------------------- | -------------------------------------------- |
| **KAgentApp**           | FastAPI application builder with A2A support |
| **KAgentSession**       | Session persistence via KAgent REST API      |
| **OpenAIAgentExecutor** | Executes agents with event streaming         |

---

## Environment Variables


---

## Examples

See `samples/openai/` for complete examples:

- `basic_agent/` - Simple agent with custom tools
- More examples coming soon

---

## See Also

- [OpenAI Agents SDK Docs](https://github.com/openai/agents)
- [Python runtime helpers](../kagent-tools/README.md)
- [A2A Protocol](https://docs.kagent.ai/a2a)

---

## License

See repository LICENSE file.

### Runtime configuration file

Managed runtimes receive `/config/config.json` from the controller. For a standalone
process, create that file and load it with `KAgentConfig.from_file()`:

```json
{"runtime":{"control_plane_url":"http://localhost:8083","gateway_url":"http://localhost:8083","name":"my-agent","namespace":"default","log_level":"info"}}
```

Provider credentials and standard `OTEL_*` overrides remain environment inputs.
