# kagent CLI Overview

The kagent CLI is the primary interface for installing, managing, and interacting with kagent. For exact flags and options on any command, run `kagent <command> --help`.

## Installation

```bash
brew install kagent
# or
curl https://raw.githubusercontent.com/kagent-dev/kagent/refs/heads/main/scripts/get-kagent | bash
```

## Command Groups

### Cluster Management
- **`kagent install`** — Install kagent onto a Kubernetes cluster. Use `--profile demo` for preloaded agents and tools, or `--profile minimal` for a bare install. Auto-detects provider API keys from environment variables.
- **`kagent uninstall`** — Remove kagent from the cluster.
- **`kagent bug-report`** — Generate a diagnostic report. Review for sensitive data before sharing.

### Interacting with Agents
- **`kagent` (no args)** — Launch the interactive terminal UI (TUI) for chatting with agents.
- **`kagent dashboard`** — Open the web UI at http://localhost:8082.
- **`kagent invoke`** — Send a one-shot task to an agent from the command line. Supports streaming, file-based tasks, and session continuity.
- **`kagent get`** — List agents, sessions, or tools.

### BYO Agent Development
These commands support the full lifecycle of building custom agents with code (Google ADK, OpenAI Agents SDK, LangGraph, CrewAI):

- **`kagent init`** — Scaffold a new agent project. Generates agent code, config, and docker-compose.
- **`kagent build`** — Build a Docker image for the agent project.
- **`kagent run`** — Run the agent locally with docker-compose and an interactive chat UI.
- **`kagent deploy`** — Deploy the agent to Kubernetes. Supports `--env-file` for secrets and `--dry-run` for preview.
- **`kagent add-mcp`** — Add an MCP tool server to a local agent project.

### MCP Server Development
- **`kagent mcp`** — Subcommands for developing MCP tool servers (init, build, deploy, run, add-tool, secrets). This is for creating *new tool servers*, not for exposing agents as MCP tools (that's the controller's `/mcp` endpoint).

### Utilities
- **`kagent version`** — Print version info.
- **`kagent completion`** — Generate shell autocompletion (bash, zsh, fish).
- **`kagent help`** — Get help for any command.

## Tips
- Always use `kagent <command> --help` to discover available flags — the CLI is well-documented.
- The `install` command uses `KAGENT_DEFAULT_MODEL_PROVIDER` to select the provider (defaults to `openAI`). Set this along with the corresponding API key env var (`OPENAI_API_KEY`, `ANTHROPIC_API_KEY`, `GOOGLE_API_KEY`, `AZURE_OPENAI_API_KEY`).
- `kagent invoke --stream` is usually preferred for interactive use since it shows output as it's generated.
