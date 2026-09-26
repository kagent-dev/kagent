# kagent

## Prerequisites
- [uv package manager](https://docs.astral.sh/uv/getting-started/installation/)
- OpenAI API key

## Python

The workspace uses `uv` to manage its Python version, dependencies, and local
`.venv`. From this directory, install the configured Python version and sync the
workspace:

```bash
uv python install
uv sync --all-extras
```

## Running the engine

The python code in this project uses the UV workspaces to manage the dependencies. You can read about them [here](https://docs.astral.sh/uv/concepts/projects/workspaces/).

The package directory contains various sub-packages which comprise the kagent engine. Each framework which kagent supports has its own package.

In addition there is a top-level kagent package which contains the main entry point for the engine. In the future we may want to have separate entrypoints for each framework to reduce the number of dependencies we have to install.

### Runtime entrypoint

The Python ADK image defaults to `kagent-adk run`, which expects a named
agent directory. To load controller-generated configuration with a `kagent`
Harness instead, select `kagent-adk static` as shown in this Harness excerpt:

```yaml
spec:
  kagent: {}
  workload:
    command: ["/.kagent/.venv/bin/kagent-adk"]
    args: ["static", "--host", "0.0.0.0", "--port", "8080"]
```

In this example, the HTTP listener uses port `8080`. Substrate readiness uses a
separate listener on port `8081`. A2A gRPC uses the address configured by
`KAGENT_A2A_GRPC_ADDRESS`.
## API v2 Inventory

The Python workspace contains these packages:

| Package | Responsibility |
| --- | --- |
| `agentsts-adk` | AgentSTS integration points for ADK |
| `agentsts-core` | OAuth 2.0 token exchange client |
| `kagent-adk` | ADK A2A runtime integration |
| `kagent-core` | Shared Python runtime support |
| `kagent-crewai` | CrewAI A2A runtime integration |
| `kagent-langgraph` | LangGraph A2A runtime integration |
| `kagent-openai` | OpenAI Agents SDK A2A runtime integration |
| `kagent-proto` | Generated protobuf and gRPC contracts |
| `kagent-skills` | Skills discovery and loading |

The retained samples and their installed entry points are:

| Sample | Command |
| --- | --- |
| `adk/basic` | `kagent-adk run basic --working-dir /app --host 0.0.0.0` |
| `crewai/poem_flow` | `poem-flow` |
| `crewai/research-crew` | `research-crew` |
| `langgraph/currency` | `currency` |
| `langgraph/hitl-tools` | `hitl-tools` |
| `langgraph/kebab` | `kebab` |
| `openai/basic_agent` | `basic-openai-agent` |

`make test` verifies package tests plus ASGI construction and `GET /health` for
every listed sample. These checks do not establish container startup, live A2A
requests, deployment, external-model execution, or durable restart behavior.

## Legacy Session References

The test suite rejects the removed Kagent-owned REST session APIs. Remaining uses
of "session" are framework-local: OpenAI SDK session factories, ADK's in-memory
or SQLite `DatabaseSessionService`, ADK remote-agent isolation, and temporary
skills working directories. They do not identify or call a Kagent-owned REST
session API.
