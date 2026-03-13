# AGENTS.md - Kagent Repository Guide for AI Agents

This document provides instructions and context for AI coding agents working in the kagent repository.

---

## Project Overview

**Kagent** is a Kubernetes-native framework for building, deploying, and managing AI agents. It is in **alpha stage (v0.x.x)**.

**Architecture:**

```
┌─────────────┐   ┌──────────────┐   ┌─────────────┐
│ Controller  │   │  HTTP Server │   │     UI      │
│    (Go)     │──▶│   (Go)       │──▶│ (Next.js)   │
└─────────────┘   └──────────────┘   └─────────────┘
       │                  │
       ▼                  ▼
┌─────────────┐   ┌──────────────┐
│  Database   │   │ Agent Runtime│
│ (SQLite/PG) │   │   (Python)   │
└─────────────┘   └──────────────┘
```

- **Go** — Kubernetes controllers, HTTP server, CLI, database layer, Go ADK
- **Python** — Agent runtime, AI/ML logic, LLM integrations, Python ADK
- **TypeScript** — Next.js web UI only

---

## Repository Structure

```
kagent/
├── go/                          # Go workspace (go.work)
│   ├── api/                     # Shared types: CRDs (v1alpha2), DB models, HTTP client SDK
│   ├── core/                    # Kubernetes controllers, HTTP server, CLI
│   │   └── test/e2e/            # End-to-end tests (SQLite + PostgreSQL)
│   └── adk/                     # Go Agent Development Kit
├── python/                      # Python workspace (UV)
│   ├── packages/                # kagent-adk, kagent-core, kagent-skills, etc.
│   └── samples/                 # Example agents
├── ui/                          # Next.js web interface
├── helm/                        # Kubernetes deployment charts
│   ├── kagent-crds/             # CRD chart (install first)
│   └── kagent/                  # Main application chart
├── docker/                      # Dockerfiles for all images
├── contrib/                     # Community addons, tools, integrations
├── design/                      # Architecture enhancement proposals (EP-*.md)
├── docs/                        # Documentation
├── examples/                    # Sample configurations
├── scripts/                     # Build and deployment scripts
├── test/                        # Integration tests
├── .github/workflows/           # CI/CD pipelines
├── Makefile                     # Top-level build orchestration
├── DEVELOPMENT.md               # Detailed development setup
└── CONTRIBUTING.md              # Contribution standards
```

---

## Language Guidelines

| Language       | Use For                                              | Do Not Use For                       |
|----------------|------------------------------------------------------|--------------------------------------|
| **Go**         | K8s controllers, CLI, core APIs, HTTP server, DB     | Agent runtime, LLM integrations, UI  |
| **Python**     | Agent runtime, ADK, LLM integrations, AI/ML logic    | K8s controllers, CLI, infrastructure |
| **TypeScript** | Web UI components and API clients only                | Backend logic, controllers, agents   |

---

## Build & Test Commands

| Task                     | Command                                |
|--------------------------|----------------------------------------|
| Build all images         | `make build`                           |
| Build CLI                | `make build-cli`                       |
| Run all tests            | `make test`                            |
| Run Go E2E tests         | `make -C go e2e`                       |
| Lint Go + Python         | `make lint`                            |
| Lint Go only             | `make -C go lint`                      |
| Lint UI                  | `npm -C ui run lint`                   |
| Generate CRD code        | `make -C go generate`                  |
| Create Kind cluster      | `make create-kind-cluster`             |
| Deploy kagent            | `make helm-install`                    |
| Helm template tests      | `make helm-test`                       |
| CVE scan                 | `make audit`                           |
| Access UI                | `kubectl port-forward -n kagent svc/kagent-ui 3000:8080` |

Before submitting changes, run `make lint` for Go/Python changes, and for UI changes run `npm -C ui run lint` and the relevant UI tests.

---

## Go Development

### Module Layout

The Go code lives under `go/` with three modules managed by `go.work`:

- **`go/api`** — Foundation layer. CRD types (`v1alpha2`), database models, HTTP client SDK, shared utilities.
- **`go/core`** — Infrastructure layer. Kubernetes controller (`cmd/controller`), HTTP server, MCP integration, metrics, E2E tests.
- **`go/adk`** — Agent Development Kit. Agent runner, session management, skills, models, memory, telemetry.

### Conventions

- Wrap errors with context: `fmt.Errorf("failed to create agent %s: %w", name, err)`
- Controllers should return errors to requeue with backoff.
- Run `gofmt -w` on changed files before committing.
- Run `golangci-lint run` before submitting (CI enforces this).
- Use table-driven tests:

```go
func TestSomething(t *testing.T) {
    tests := []struct {
        name    string
        input   string
        want    string
        wantErr bool
    }{
        {name: "valid input", input: "foo", want: "bar", wantErr: false},
        {name: "invalid input", input: "", want: "", wantErr: true},
    }
    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            got, err := Something(tt.input)
            if (err != nil) != tt.wantErr {
                t.Errorf("Something() error = %v, wantErr %v", err, tt.wantErr)
            }
            if got != tt.want {
                t.Errorf("Something() = %v, want %v", got, tt.want)
            }
        })
    }
}
```

- Go tests run with `-race` flag in CI.
- E2E tests run against both SQLite and PostgreSQL.

---

## Python Development

- Python 3.10+ required.
- Uses **UV** for dependency management across workspace packages.
- **Ruff** for linting and formatting (120-char line limit).
- **Pytest** for testing (async auto mode).
- CI tests across Python 3.10-3.13.
- Key packages: `kagent-adk`, `kagent-core`, `kagent-skills`, `kagent-crewai`, `kagent-langgraph`, `kagent-openai`.

---

## UI Development

- **Next.js** with React, TypeScript, Tailwind CSS, Radix UI.
- **Zod** for validation, **Zustand** for state management, **React Hook Form** for forms.
- Do not use `any` type in TypeScript.
- ESLint for linting, Jest for unit tests, Cypress for E2E tests.

---

## API Versioning

- **v1alpha2** — Current. All new features go here.
- **v1alpha1** — Legacy/deprecated. Do not modify unless fixing critical bugs.

Key CRDs: `Agent`, `ModelConfig`, `ModelProviderConfig`, `RemoteMCPServer`.

Breaking changes are acceptable in alpha versions.

---

## CI/CD Pipeline

The main CI workflow (`.github/workflows/ci.yaml`) runs on pushes/PRs to `main` and release branches:

1. **Go** — Unit tests with race detection, E2E tests (SQLite + PostgreSQL), golangci-lint
2. **Python** — Tests on 3.10-3.13, ruff linting, format validation
3. **UI** — ESLint, Jest unit tests
4. **Helm** — Chart unit tests, manifest verification
5. **Docker** — Multi-platform builds (amd64, arm64) for controller, ui, app, cli, golang-adk, skills-init

Additional workflows: image scanning, release tagging, stale issue management.

---

## Commit Messages

Use **Conventional Commits**:

```
<type>: <description>

[optional body]

Signed-off-by: Name <email>
```

Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `perf`, `ci`

All commits require `Signed-off-by` trailer (DCO). Use `git commit -s` or run `make init-git-hooks`.

---

## Testing Requirements

All PRs must include:

- Unit tests for new functions/methods
- E2E tests for new CRD fields or API endpoints
- Mocked external services (LLMs, K8s API) in unit tests
- All tests passing in CI

---

## Key Architectural Patterns

### MCP Integration
- Reference-based architecture (no embedded details in CRDs)
- Supports MCPServer CRD, Kubernetes Services, and RemoteMCPServer
- Protocols: SSE and STREAMABLE_HTTP
- Built-in tools: Kubernetes, Istio, Helm, Argo, Prometheus, Grafana, Cilium

### Memory System
- PostgreSQL + pgvector for production, Turso/libSQL for development
- 768-dimensional embeddings with cosine similarity search (threshold 0.3)
- Dual save: explicit (via tools) + implicit (auto-save every 5 turns)
- 15-day memory expiration with intelligent retention

### HTTP Client SDK
- Located in `go/api/client/`
- Covers Agent, Session, Tool, Memory, Model, ModelConfig, ModelProviderConfig operations

---

## Code Reuse

- Before writing new code, search the codebase for existing functions, utilities, and patterns that already solve the problem.
- Do not duplicate logic. If similar functionality exists, refactor it into a shared helper or reuse the existing implementation.
- Shared Go utilities belong in `go/api/` (types, models, client SDK) — not duplicated across `go/core/` and `go/adk/`.
- Shared Python utilities belong in `kagent-core` — not duplicated across other packages.
- If you find duplicated code while working on a change, consolidate it as part of your PR when the scope is reasonable.

---

## Naming Conventions

- Use **descriptive variable and function names** across all languages. Names should clearly convey purpose and intent.
- Avoid abbreviations and single-letter names (except loop counters like `i`, `j`). Use `agentConfig` not `ac`, `modelProvider` not `mp`, `sessionID` not `sID`.
- Function names should describe what they do: `createAgentFromSpec` not `makeAgent`, `validateModelConfig` not `checkCfg`.
- Go: Follow standard Go naming (camelCase for unexported, PascalCase for exported). Receiver names can be short (1-2 chars) per Go convention.
- Python: Use snake_case for functions/variables, PascalCase for classes.
- TypeScript: Use camelCase for functions/variables, PascalCase for components and types.

---

## What Not to Do

- Do not add features beyond what is requested (avoid over-engineering).
- Do not modify v1alpha1 unless fixing critical bugs.
- Do not vendor dependencies (use go.mod).
- Do not commit without running tests locally.
- Do not use `any` type in TypeScript.
- Do not skip E2E tests for API/CRD changes.
- Do not create new MCP servers in the main kagent repo.
- Do not duplicate existing functions or utilities — reuse what already exists.

---

## Additional Resources

- [DEVELOPMENT.md](DEVELOPMENT.md) — Detailed local development setup
- [CONTRIBUTING.md](CONTRIBUTING.md) — Contribution process and standards
- [docs/architecture/](docs/architecture/) — Architecture documentation
- [design/](design/) — Enhancement proposals (EP-*.md)
- [examples/](examples/) and [python/samples/](python/samples/) — Sample configurations
