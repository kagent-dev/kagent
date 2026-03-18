# Kagent Python Development Guide

Python ADK, agent runtime, LLM integrations, and framework support.

**See also:** [architecture.md](architecture.md) (system overview), [testing-ci.md](testing-ci.md) (test commands), [go-guide.md](go-guide.md) (Go types that mirror Python)

---

## Local development

### Prerequisites

- Python 3.13 (3.10+ supported in CI)
- UV package manager (v0.10.4+)

### Essential commands

| Task | Command |
|------|---------|
| Sync dependencies | `make -C python update` |
| Format code | `make -C python format` |
| Lint code | `make -C python lint` |
| Run all tests | `make -C python test` |
| Build packages | `make -C python build` |
| Security audit | `make -C python audit` |
| Generate test certs | `make -C python generate-test-certs` |

### Package structure (UV workspace)

```
python/packages/
├── kagent-adk/         # Main ADK - agent executor, A2A server, MCP toolset
│   └── src/kagent/adk/
│       ├── _a2a.py              # FastAPI A2A server
│       ├── _agent_executor.py   # Core request handler (hub file)
│       ├── _approval.py         # Human-in-the-loop approval
│       ├── types.py             # Config types (mirrors Go ADK types)
│       ├── _mcp_toolset.py      # MCP tool integration
│       ├── _mcp_capability_tools.py  # MCP capability tools
│       ├── _memory_service.py   # Vector memory service
│       ├── _session_service.py  # Session persistence
│       ├── _token.py            # K8s token refresh
│       ├── cli.py               # CLI interface
│       ├── converters/          # Event/part converters
│       ├── models/              # LLM provider implementations
│       └── tools/               # Built-in tools (AskUser, Skills, memory)
│
├── kagent-core/        # Core utilities
├── kagent-skills/      # Skills runtime/execution
├── kagent-openai/      # OpenAI native integration
├── kagent-langgraph/   # LangGraph framework support
├── kagent-crewai/      # CrewAI framework support
└── agentsts-*/         # AgentSTS variants
```

## Python coding standards

### Type hints

- Type hints on all function signatures
- Use `Optional[T]` for nullable parameters
- Use `list[T]`, `dict[K, V]` (lowercase) for Python 3.10+
- No bare `Any` types without justification

### Formatting and linting

- **Ruff** for formatting and linting
- Run `make -C python format` before committing
- Run `make -C python lint` to check formatting

### Error handling

```python
# Wrap errors with context
try:
    result = await mcp_client.call_tool(tool_name, args)
except Exception as e:
    raise RuntimeError(f"Failed to call tool {tool_name}: {e}") from e
```

### Testing

```python
# Use pytest with async support
import pytest

@pytest.mark.asyncio
async def test_agent_executor():
    executor = AgentExecutor(config)
    result = await executor.handle_request(request)
    assert result.status == "completed"
```

Test certs required for TLS tests: `make -C python generate-test-certs`

## ADK development

### Agent executor flow

1. A2A request received via FastAPI server (`_a2a.py`)
2. Request parsed and routed to `AgentExecutor._handle_request()` (`_agent_executor.py`)
3. ADK Runner manages LLM loop: system prompt + history + tool calls
4. Tool execution via MCP toolset (`_mcp_toolset.py`)
5. Events converted from ADK format to A2A format (`converters/`)
6. Response streamed back via SSE

### Adding LLM provider support

1. Create provider implementation in `kagent-adk/src/kagent/adk/models/`
2. Add provider config types to `types.py`
3. Mirror config types in Go: `go/api/v1alpha2/modelconfig_types.go`
4. Update translator: `go/core/internal/controller/translator/`
5. Add tests

### Type alignment (Python ↔ Go)

Python types in `kagent-adk/src/kagent/adk/types.py` must stay aligned with Go types in `go/api/adk/types.go`. Both are serialized as JSON in `config.json`.

When adding fields:
- Add to Go type first, then mirror in Python
- Use the same JSON field names
- Add cross-reference comments in both languages
- Flag changes to one side without corresponding changes to the other

## Sample agents

Located in `python/samples/`:

```
python/samples/
├── adk/          # ADK-based examples
├── langgraph/    # LangGraph examples
├── crewai/       # CrewAI examples
└── openai/       # OpenAI examples
```

## Common development patterns

### Adding a new built-in tool

1. Create tool class in `kagent-adk/src/kagent/adk/tools/`
2. Register in the agent executor's tool loading
3. Add tests
4. Update documentation

### Adding a new framework integration

1. Create new package in `python/packages/kagent-<framework>/`
2. Add to UV workspace in `python/pyproject.toml`
3. Implement the agent executor interface
4. Add sample in `python/samples/<framework>/`
5. Add CI test job in `.github/workflows/ci.yaml`
