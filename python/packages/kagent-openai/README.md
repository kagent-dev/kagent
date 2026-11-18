# KAgent OpenAI Agents SDK Integration

OpenAI Agents SDK integration for KAgent with A2A (Agent-to-Agent) protocol support, session management, and optional file and bash tools.

---

## Quick Start

```python
import json
from pathlib import Path

from kagent.core import KAgentConfig, materialize_from_bootstrap
from kagent.openai import KAgentApp
from agents.agent import Agent

# Create your OpenAI agent
agent = Agent(
    name="Assistant",
    instructions="You are a helpful assistant.",
)

# Load the runtime configuration supplied to the workload
materialize_from_bootstrap("/config")
app = KAgentApp(
    agent=agent,
    agent_card=json.loads(Path("/config/agent-card.json").read_text()),
    config=KAgentConfig.from_file(),
)

# Run
app.build().run()
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

The production app persists A2A tasks and their public history through the controller task store. The OpenAI runner receives the A2A context ID as its session context.

---

## Deployment

`app.build().run()` starts A2A gRPC on `runtime.port` (default `80`), then serves `GET /readyz` on port `8081` for Substrate.

Standard Docker deployment:

```dockerfile
FROM python:3.13-slim
WORKDIR /app
COPY requirements.txt .
RUN pip install -r requirements.txt
COPY agent.py .
CMD ["python", "agent.py"]
```

Load `/config/config.json` with `KAgentConfig.from_file()` to connect to kagent.

---

## Architecture

| Component               | Purpose                                      |
| ----------------------- | -------------------------------------------- |
| **KAgentApp**           | gRPC server builder with A2A support |
| **KAgentTaskStore**     | A2A task persistence via controller gRPC     |
| **OpenAIAgentExecutor** | Executes agents with event streaming         |

---

## Examples

See `samples/openai/` for complete examples:

- `basic_agent/` - Simple agent with custom tools
- More examples coming soon

---

## See Also

- [OpenAI Agents SDK Docs](https://openai.github.io/openai-agents-python/)
- [Python runtime helpers](../kagent-tools/README.md)
- [A2A Protocol](https://docs.kagent.ai/a2a)

---

## License

See repository LICENSE file.

### Substrate startup

The image materializes the controller's revision into `/config` before building
the app. Clients discover and invoke the Agent through the
[controller gateway](../../../docs/architecture/a2a-transports.md).
