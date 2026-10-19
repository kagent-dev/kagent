# KAgent OpenAI Agents SDK Integration

OpenAI Agents SDK integration for KAgent with A2A (Agent-to-Agent) protocol support and optional skills integration.

---

## Quick Start

Provide the runtime configuration at `/config/config.json`:

```json
{"runtime":{"control_plane_url":"http://localhost:8083","gateway_url":"http://localhost:8083","name":"my-openai-agent","namespace":"default","log_level":"info"}}
```

Then build the A2A application:

```python
from agents.agent import Agent
from kagent.core import KAgentConfig
from kagent.openai import KAgentApp

agent = Agent(
    name="Assistant",
    instructions="You are a helpful assistant.",
    tools=[],
)

agent_card = {
    "name": "my-openai-agent",
    "description": "My OpenAI agent",
    "version": "0.1.0",
    "supportedInterfaces": [{"url": "http://localhost:8080", "protocolBinding": "JSONRPC"}],
    "capabilities": {"streaming": True},
    "defaultInputModes": ["text/plain"],
    "defaultOutputModes": ["text/plain"],
}

app = KAgentApp(
    agent=agent,
    agent_card=agent_card,
    config=KAgentConfig.from_file(),
)

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

## Task and Conversation State

`build()` persists public A2A task history through `KAgentTaskStore` and the
controller gRPC API. It does not configure an OpenAI Agents SDK session, so
framework-level conversation state is not persisted automatically.

Applications that need durable framework-level conversation state must configure
an OpenAI Agents SDK session explicitly. There is no Kagent REST session client
in this package.

---

## Local Development

`build_local()` creates the same kind of in-memory A2A application without connecting to a KAgent backend. `KAgentConfig` still requires its runtime configuration:

```python
app = KAgentApp(
    agent=agent,
    agent_card=agent_card,
    config=KAgentConfig.from_file(),
)

fastapi_app = app.build_local()
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

| Component | Purpose |
| --- | --- |
| **KAgentApp** | FastAPI application builder with A2A support |
| **OpenAIAgentExecutor** | Executes agents with event streaming |
| **KAgentTaskStore** | Persists public A2A task history through controller gRPC |
| **InMemoryTaskStore** | Tracks local-development tasks for the lifetime of the process |

---

## Environment Variables

- `OPENAI_API_KEY` - OpenAI API key
- `OPENAI_API_BASE` - Optional OpenAI-compatible API base URL

---

## Examples

See `samples/openai/` for complete examples:

- `basic_agent/` - Simple agent with custom tools

---

## See Also

- [OpenAI Agents SDK Docs](https://github.com/openai/openai-agents-python)
- [KAgent Skills](../../kagent-skills/README.md)
- [A2A Protocol](https://a2a-protocol.org/)

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
