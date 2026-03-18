# Agent Guide: Kagent Monorepo

Entry point for AI agents and developers. Load only the reference file relevant to your current task.

- Scope: kagent main branch, controller (Go), ADK (Python/Go), UI (Next.js), Helm charts
- Current version: v0.x.x (Alpha stage)

## Development Workflow Skill

**For detailed development workflows, use the `kagent-dev` skill.** The skill provides comprehensive guidance on:

- Adding CRD fields (step-by-step with examples)
- Running and debugging E2E tests
- PR review workflows
- Local development setup
- CI failure troubleshooting
- Common development patterns

The skill includes detailed reference materials on CRD workflows, translator patterns, E2E debugging, and CI failures.

---

## Language guidelines

| Language | Use For | Don't Use For |
|----------|---------|---------------|
| **Go** | K8s controllers, CLI tools, core APIs, HTTP server, database layer | Agent runtime, LLM integrations, UI |
| **Python** | Agent runtime, ADK, LLM integrations, AI/ML logic | Kubernetes controllers, CLI, infrastructure |
| **TypeScript** | Web UI components and API clients only | Backend logic, controllers, agents |

**Rule of thumb:** Infrastructure in Go, AI/Agent logic in Python, User interface in TypeScript.

## API versioning

- **v1alpha2** (current) — All new features go here
- **v1alpha1** (legacy/deprecated) — Minimal maintenance only

Breaking changes are acceptable in alpha versions.

---

### Code reuse policy (agents and contributors)

- **Never duplicate code.** Extract shared helpers, use common abstractions, and avoid copy-paste. If you find yourself writing similar logic in more than one place, refactor to a shared location.
- Before creating a new helper or utility, search the codebase for existing implementations. Use the code-tree query tool if available: `python3 tools/code-tree/query_graph.py --search "<keyword>"`.

### Testing policy (agents and contributors)

- Every new non-trivial function, method, or exported API must have accompanying unit tests before merging.
- All existing tests must pass locally before pushing changes. Run the relevant test suites listed in the essential commands section.
- When modifying existing functions, verify that existing tests still pass and add new test cases if the behavior changes.
- Do not submit changes that break existing tests. If a test failure is pre-existing and unrelated to your changes, note it explicitly in the PR description.

### Commit policy (agents and contributors)

- Use **Conventional Commits** format: `<type>: <description>` (types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `perf`, `ci`)
- Always sign off on commits with `git commit -s` (adds a `Signed-off-by:` trailer).
- Never include AI agents (e.g. Claude Code, Copilot, or similar tools) as co-authors on commits. The human author is responsible for the work.

---

## Best practices

### Do's

- Read existing code before making changes
- Follow the language guidelines (Go for infra, Python for agents, TS for UI)
- Write table-driven tests in Go
- Wrap errors with context using `%w`
- Use conventional commit messages
- Mock external services in unit tests
- Update documentation for user-facing changes
- Run `make lint` before submitting

### Don'ts

- Don't add features beyond what's requested (avoid over-engineering)
- Don't modify v1alpha1 unless fixing critical bugs (focus on v1alpha2)
- Don't vendor dependencies (use go.mod)
- Don't commit without testing locally first
- Don't use `any` type in TypeScript
- Don't skip E2E tests for API/CRD changes
- Don't create new MCP servers in the main kagent repo

---

## Essential commands

| Task | Command |
|------|---------|
| Create Kind cluster | `make create-kind-cluster` |
| Deploy kagent | `make helm-install` |
| Build all | `make build` |
| Run all tests | `make test` |
| Go unit tests | `make -C go test` |
| Go E2E tests | `make -C go e2e` |
| Go lint | `make -C go lint` |
| Go CRD generation | `make -C go manifests generate` |
| Python tests | `make -C python test` |
| Python format | `make -C python format` |
| Python lint | `make -C python lint` |
| UI tests | `cd ui && npx jest` |
| UI lint | `cd ui && npx next lint` |
| Access UI | `kubectl port-forward -n kagent svc/kagent-ui 3000:8080` |

---

## Reference file index

Load only the guide you need for the current task:

| Guide | Contents | When to load |
|-------|----------|-------------|
| [docs/agents/architecture.md](docs/agents/architecture.md) | System architecture, subsystem boundaries, dependency rules, CRDs, protocols | Understanding how components interact |
| [docs/agents/go-guide.md](docs/agents/go-guide.md) | Go development: controllers, API, ADK, CLI, linting, testing patterns | Working on Go code |
| [docs/agents/python-guide.md](docs/agents/python-guide.md) | Python ADK, agent runtime, LLM integrations, framework support | Working on Python code |
| [docs/agents/ui-guide.md](docs/agents/ui-guide.md) | Next.js UI, components, routing, state management | Working on frontend code |
| [docs/agents/testing-ci.md](docs/agents/testing-ci.md) | Test suites, CI workflows, coverage requirements, local CI reproduction | Running tests or debugging CI |
| [docs/agents/code-tree.md](docs/agents/code-tree.md) | Knowledge graph queries, hub files, module dependencies, entry points | Scoping changes and impact analysis |

---

## Task routing

| Task | Start with |
|------|-----------|
| Add CRD field | [go-guide.md](docs/agents/go-guide.md#adding-crd-fields) → [architecture.md](docs/agents/architecture.md#crd-types-v1alpha2) |
| Add controller | [go-guide.md](docs/agents/go-guide.md#controller-development) → [architecture.md](docs/agents/architecture.md#controller-patterns) |
| Add LLM provider | [python-guide.md](docs/agents/python-guide.md#adding-llm-provider-support) |
| Add UI page | [ui-guide.md](docs/agents/ui-guide.md#adding-a-new-page) |
| Debug CI failure | [testing-ci.md](docs/agents/testing-ci.md#common-ci-failure-patterns) |
| Scope blast radius | [code-tree.md](docs/agents/code-tree.md#querying-the-graph) |
| Review a PR | [.github/copilot-instructions.md](.github/copilot-instructions.md) |

---

## Validation checklist

Before pushing any change:

1. **Lint**: `make -C go lint` / `make -C python lint` / `cd ui && npx next lint`
2. **Test**: `make -C go test` / `make -C python test` / `cd ui && npx jest`
3. **Generated files**: If CRD types changed, `make -C go manifests generate` and commit
4. **Type alignment**: If Go ADK types changed, verify Python mirror is updated
5. **Commit message**: Conventional format (`feat:`, `fix:`, `docs:`, etc.)
6. **Sign-off**: `git commit -s`

---

## Code Knowledge Graph

Two options for code-aware PR reviews and blast-radius analysis:

### Option 1: code-tree tools (zero-dependency, in-repo)

```bash
# Build the knowledge graph
python3 tools/code-tree/code_tree.py --repo-root . --incremental -q

# Query symbols, dependencies, impact
python3 tools/code-tree/query_graph.py --symbol <name>
python3 tools/code-tree/query_graph.py --impact <file> --depth 5
python3 tools/code-tree/query_graph.py --test-impact <file>
```

See [docs/agents/code-tree.md](docs/agents/code-tree.md) for full usage.

### Option 2: code-review-graph MCP server (optional)

```bash
pip install code-review-graph
code-review-graph install   # registers MCP server + git hooks
code-review-graph build     # initial index (~15s)
# restart Claude Code to pick up the new MCP server
```

| Command | Description |
|---------|-------------|
| `/code-review-graph:review-pr` | Review the current PR with graph-aware context |
| `/code-review-graph:review-delta` | Review only staged/unstaged changes |
| `code-review-graph update` | Manually refresh the index (auto-updates via hooks) |
| `code-review-graph status` | Show indexed node counts by language |

The `.code-review-graphignore` file controls which files are excluded from indexing. The local `.code-review-graph/` directory is git-ignored.

---

## Additional resources

- **Development setup:** See [DEVELOPMENT.md](DEVELOPMENT.md)
- **Contributing:** See [CONTRIBUTING.md](CONTRIBUTING.md)
- **Architecture (detailed):** See [docs/architecture/](docs/architecture/)
- **Examples:** Check `examples/` and `python/samples/`
