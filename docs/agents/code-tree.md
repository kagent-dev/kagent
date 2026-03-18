# Kagent Code-Tree Knowledge Graph

Pre-built knowledge graph artifacts, query commands, codebase statistics, entry points, hub files, module dependencies, and workflows for development and PR reviews.

**See also:** [architecture.md](architecture.md) (system architecture), [go-guide.md](go-guide.md) (Go files), [python-guide.md](python-guide.md) (Python files), [ui-guide.md](ui-guide.md) (UI files)

---

## Development workflow

Follow this workflow for any code change. Regenerate the graph first, use queries to scope your change, then verify impact after.

### Before writing code

1. **Regenerate graph**: `python3 tools/code-tree/code_tree.py --repo-root . --incremental -q`
2. **Locate**: `python3 tools/code-tree/query_graph.py --symbol <name>` or `--search <keyword>`
3. **Scope**: `--rdeps <file>` to understand blast radius before changing anything
4. **Read**: Load the relevant guide from [AGENTS.md](../../AGENTS.md), then read the target files

### While writing code

5. **Guard hub files**: Extra care when touching high-fanout files (see hub files below). Run `--impact <file> --depth 5` first.
6. **Match patterns**: Follow existing code style. Go: error wrapping with `%w`, descriptive names. Python: type hints, ruff formatting. TypeScript: strict mode, TailwindCSS.
7. **Cross-boundary changes**: Verify dependency direction rules (see [architecture.md](architecture.md#critical-dependency-directions)).

### After writing code

8. **Run relevant tests**: See essential commands in [AGENTS.md](../../AGENTS.md)
9. **Run impact check**: `python3 tools/code-tree/query_graph.py --test-impact <changed-file>`
10. **Lint**: Run the appropriate formatter/linter for the language you changed

---

## Available artifacts

| File | Contents | Use case |
|------|----------|----------|
| `docs/code-tree/summary.md` | Architecture overview, hub files, key types, inheritance, module deps | **Start here** for any exploration |
| `docs/code-tree/graph.json` | Full knowledge graph (nodes + edges) | Programmatic queries with `jq` |
| `docs/code-tree/tags.json` | Flat symbol index with file:line locations | Quick symbol lookup |
| `docs/code-tree/modules.json` | Directory-level dependency map | Module impact analysis |

## Querying the graph

Use the query tool at `tools/code-tree/query_graph.py`:

```bash
# Find where a symbol is defined
python3 tools/code-tree/query_graph.py --symbol AgentExecutor

# Trace what a file depends on
python3 tools/code-tree/query_graph.py --deps go/core/internal/controller/agent_controller.go

# Find what imports a file (reverse deps / blast radius)
python3 tools/code-tree/query_graph.py --rdeps go/api/v1alpha2/agent_types.go

# Show inheritance hierarchy for a class
python3 tools/code-tree/query_graph.py --hierarchy AgentExecutor

# Search symbols by keyword
python3 tools/code-tree/query_graph.py --search "reconcil"

# Get module overview (files, deps, symbols)
python3 tools/code-tree/query_graph.py --module go/core/internal/controller

# Find entry points (main functions, unimported files)
python3 tools/code-tree/query_graph.py --entry-points

# Extract code chunks from a file for context
python3 tools/code-tree/query_graph.py --chunks go/api/v1alpha2/agent_types.go

# Who calls this function/method?
python3 tools/code-tree/query_graph.py --callers Reconcile

# What does this function call?
python3 tools/code-tree/query_graph.py --callees CreateAgent

# Find call paths between two symbols
python3 tools/code-tree/query_graph.py --call-chain Reconcile CreateAgent

# Change impact analysis (transitive)
python3 tools/code-tree/query_graph.py --impact go/api/v1alpha2/agent_types.go
python3 tools/code-tree/query_graph.py --impact AgentSpec --depth 8

# Which tests are affected by a change?
python3 tools/code-tree/query_graph.py --test-impact go/core/internal/controller/agent_controller.go

# What tests cover a file?
python3 tools/code-tree/query_graph.py --test-coverage go/api/v1alpha2/agent_types.go

# Graph statistics
python3 tools/code-tree/query_graph.py --stats
```

Add `--json` to any query for machine-readable output.
Use `--depth N` to control impact/call-chain traversal depth (default: 5).

## Entry points

| Entry point | Purpose |
|-------------|---------|
| `go/core/cmd/controller/main.go` | Controller + HTTP server |
| `go/adk/cmd/main.go` | Go ADK server |
| `go/core/cli/cmd/kagent/main.go` | kagent CLI |
| `python/packages/kagent-adk/src/kagent/adk/cli.py` | Python ADK CLI |
| `python/packages/kagent-adk/src/kagent/adk/_a2a.py` | Python A2A server |
| `ui/src/app/layout.tsx` | UI entry (Next.js root layout) |

## Hub files (most connected)

These files are imported by the most other files. Changes to them have the widest blast radius:

| File | Role |
|------|------|
| `go/api/v1alpha2/agent_types.go` | Agent CRD type definitions (20KB) |
| `go/api/v1alpha2/modelconfig_types.go` | ModelConfig CRD types (14KB) |
| `go/api/v1alpha2/common_types.go` | Shared types (ValueRef, etc.) |
| `go/api/database/models.go` | Database GORM models |
| `go/api/adk/types.go` | Go ADK configuration types |
| `python/packages/kagent-adk/src/kagent/adk/types.py` | Python ADK types (23KB, mirrors Go) |
| `python/packages/kagent-adk/src/kagent/adk/_agent_executor.py` | Core executor (28KB) |
| `ui/src/lib/messageHandlers.ts` | A2A message parsing (26KB) |
| `ui/src/components/chat/ChatInterface.tsx` | Main chat UI (28KB) |

## Key module dependencies

```
go/core/internal/controller/  -> go/api/v1alpha2/           (CRD types)
go/core/internal/controller/  -> go/core/internal/database/  (DB operations)
go/core/internal/controller/  -> go/core/internal/mcp/       (tool discovery)
go/core/internal/httpserver/  -> go/core/internal/database/  (DB queries)
go/core/internal/httpserver/  -> go/core/internal/a2a/       (A2A proxy)
go/core/internal/a2a/         -> go/api/client/              (REST client)
go/adk/pkg/                   -> go/api/adk/                 (ADK types)
go/adk/pkg/models/            -> go/api/v1alpha2/            (ModelConfig types)
kagent-adk/                   -> kagent-core/                (core utilities)
kagent-adk/                   -> kagent-skills/              (skills framework)
ui/src/lib/                   -> ui/src/types/               (type definitions)
ui/src/components/            -> ui/src/lib/                 (utilities)
```

## Key type hierarchies

```
Agent CRD (go/api/v1alpha2/agent_types.go)
  |- AgentSpec
  |    |- DeclarativeAgentConfig (type: "Declarative")
  |    |- BYOAgentConfig (type: "BYO")
  |- AgentStatus

ModelConfig CRD (go/api/v1alpha2/modelconfig_types.go)
  |- ModelConfigSpec
  |    |- OpenAIConfig
  |    |- AnthropicConfig
  |    |- AzureOpenAIConfig
  |    |- OllamaConfig
  |    |- GeminiConfig
  |    |- CustomProviderConfig

LLM Providers (python/packages/kagent-adk/src/kagent/adk/models/)
  |- OpenAI native
  |- LiteLLM (multi-provider)
```

## Regenerating the graph

**Requires Python 3.9+** (uses `list[str]` type syntax). If your default `python3` is older, use `python3.12` or similar explicitly.

Always regenerate before first use in a session:

```bash
# Incremental (fast — only reparses changed files)
python3 tools/code-tree/code_tree.py --repo-root . --incremental -q

# Full rebuild (after major changes)
python3 tools/code-tree/code_tree.py --repo-root .
```

For a structured PR review workflow using these queries, see [copilot-instructions.md](../../.github/copilot-instructions.md#code-tree-impact-analysis).
