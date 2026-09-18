# KAgent OpenAI Agents SDK Integration

OpenAI Agents SDK integration for KAgent with A2A (Agent-to-Agent) protocol support and optional skills integration.

---

## Quick Start

Set the required configuration first:

```bash
export KAGENT_API_URL=http://localhost:8083
export KAGENT_GATEWAY_URL=http://localhost:8083
export KAGENT_NAME=my-openai-agent
export KAGENT_NAMESPACE=default
export OPENAI_API_KEY=your-api-key
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
    "supportedInterfaces": [
        {"url": "http://localhost:8080", "protocolBinding": "JSONRPC"}
    ],
    "capabilities": {"streaming": True},
    "defaultInputModes": ["text/plain"],
    "defaultOutputModes": ["text/plain"],
}

app = KAgentApp(
    agent=agent,
    agent_card=agent_card,
    config=KAgentConfig(),
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

`KAgentApp` uses an in-memory A2A task store and does not configure an OpenAI Agents SDK session. Task state held by the agent process is lost when that process restarts. This package does not configure a gateway connection; a deployment may place a gateway in front of the application and have that gateway own durable task history.

Applications that need durable framework-level conversation state must configure it explicitly rather than relying on a KAgent REST session service.

---

## Local Development

`build_local()` creates the same kind of in-memory A2A application without connecting to a KAgent backend. `KAgentConfig` still requires its configuration values:

```python
app = KAgentApp(
    agent=agent,
    agent_card=agent_card,
    config=KAgentConfig(),
)

fastapi_app = app.build_local()
```

---

## Architecture

| Component | Purpose |
| --- | --- |
| **KAgentApp** | FastAPI application builder with A2A support |
| **OpenAIAgentExecutor** | Executes agents with event streaming |
| **InMemoryTaskStore** | Tracks A2A tasks for the lifetime of the process |

---

## Environment Variables

- `KAGENT_API_URL` - Required by `KAgentConfig`; not used for outbound control-plane calls by this wrapper
- `KAGENT_GATEWAY_URL` - Required by `KAgentConfig`; not used for outbound gateway calls by this wrapper
- `KAGENT_NAME` - Agent name
- `KAGENT_NAMESPACE` - Agent namespace
- `OPENAI_API_KEY` - OpenAI API key
- `OPENAI_API_BASE` - Optional OpenAI-compatible API base URL
- `LOG_LEVEL` - Logging level (default: INFO)

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
