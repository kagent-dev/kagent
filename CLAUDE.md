# CLAUDE.md - Kagent Development Guide

This document provides essential guidance for AI agents (Claude Code and others) working in the kagent repository. It consolidates architectural patterns, conventions, and best practices to help agents make informed decisions and contribute effectively.

## Table of Contents
- [Project Overview](#project-overview)
- [Repository Structure](#repository-structure)
- [Development Workflow](#development-workflow)
- [Language Guidelines](#language-guidelines)
- [Testing Requirements](#testing-requirements)
- [API Design and Versioning](#api-design-and-versioning)
- [Code Patterns and Conventions](#code-patterns-and-conventions)
- [Common Tasks](#common-tasks)

---

## Project Overview

**Kagent** is a Kubernetes-native framework for building, deploying, and managing AI agents. It provides:

- Kubernetes operator for declarative agent management (Go)
- Agent Development Kit (ADK) for building agents (Python)
- Web UI for agent interaction (TypeScript/Next.js)
- Multi-LLM provider support (OpenAI, Anthropic, Ollama, Vertex AI, Bedrock, etc.)
- Model Context Protocol (MCP) integration for tools
- Agent-to-Agent (A2A) communication
- Built-in observability with OpenTelemetry

**Architecture at a Glance:**
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

**Current Version:** v0.x.x (Alpha stage)

---

## Repository Structure

```
kagent/
├── go/                      # Kubernetes controller, CLI, API server
│   ├── api/                 # CRD definitions (v1alpha1, v1alpha2)
│   ├── cmd/                 # Binary entry points
│   ├── internal/            # Core implementation
│   │   ├── controller/      # K8s reconciliation logic
│   │   ├── httpserver/      # REST/gRPC API
│   │   ├── database/        # SQLite/Postgres persistence
│   │   ├── a2a/             # Agent-to-Agent communication
│   │   ├── mcp/             # MCP integration
│   │   └── adk/             # Go ADK types
│   ├── pkg/                 # Public Go packages
│   └── test/e2e/            # End-to-end tests
│
├── python/                  # Agent runtime and ADK
│   ├── packages/            # UV workspace packages
│   │   ├── kagent-adk/      # Agent Development Kit
│   │   ├── kagent-core/     # Core utilities
│   │   ├── kagent-skills/   # Skills framework
│   │   ├── kagent-openai/   # OpenAI integration
│   │   ├── kagent-crewai/   # CrewAI support
│   │   └── kagent-langgraph/# LangGraph support
│   └── samples/             # Example agents
│
├── ui/                      # Next.js web interface
│   └── src/
│       ├── components/      # React components
│       ├── lib/             # Utilities and API clients
│       └── types/           # TypeScript types
│
├── helm/                    # Kubernetes deployment
│   ├── kagent-crds/         # CRD chart (install first)
│   ├── kagent/              # Main application chart
│   ├── agents/              # Pre-built agent charts
│   └── tools/               # Tool server charts
│
├── docs/                    # Architecture documentation
├── .github/workflows/       # CI/CD pipelines
└── examples/                # Example YAML configurations
```

### Key Files to Know

- [Makefile](Makefile) - Root build orchestration
- [DEVELOPMENT.md](DEVELOPMENT.md) - Development setup guide
- [go/api/v1alpha2/agent_types.go](go/api/v1alpha2/agent_types.go) - Agent CRD definition
- [go/internal/controller/reconciler/reconciler.go](go/internal/controller/reconciler/reconciler.go) - Shared reconciler pattern
- [python/packages/kagent-adk/](python/packages/kagent-adk/) - Python ADK implementation
- [helm/kagent/values.yaml](helm/kagent/values.yaml) - Default configuration

---

## Development Workflow

### Initial Setup

```bash
# 1. Create local Kind cluster
make create-kind-cluster

# 2. Set LLM provider (openAI, anthropic, ollama, etc.)
export KAGENT_DEFAULT_MODEL_PROVIDER=openAI
export OPENAI_API_KEY=your-key-here

# 3. Deploy kagent to cluster
make helm-install

# 4. Access UI
kubectl port-forward -n kagent svc/kagent-ui 3000:80
```

### Making Changes

1. **Read existing code** - Never propose changes to code you haven't read
2. **Check patterns** - Look for similar implementations to maintain consistency
3. **Run tests** - All tests must pass before submitting
4. **Update docs** - User-facing changes require README updates

### Build Targets

```bash
# Build all components
make build

# Build specific components
make -C go build          # Controller + CLI
make -C python build      # Python packages
make -C ui build          # Next.js UI

# Run tests
make -C go test           # Go unit tests
make -C go test-e2e           # Go E2E tests
make -C python test       # Python tests

# Code quality
make lint                 # Lint all code

# Generate code (after CRD changes)
make -C go generate
```

---

## Language Guidelines

### When to Use Each Language

| Language | Use For | Don't Use For |
|----------|---------|---------------|
| **Go** | K8s controllers, CLI tools, core APIs, HTTP server, database layer | Agent runtime, LLM integrations, UI |
| **Python** | Agent runtime, ADK, LLM integrations, AI/ML logic | Kubernetes controllers, CLI, infrastructure |
| **TypeScript** | Web UI components and API clients only | Backend logic, controllers, agents |

**Rule of thumb:** Infrastructure in Go, AI/Agent logic in Python, User interface in TypeScript.

### Go Guidelines

**Code Organization:**
- `internal/` - Private implementation details
- `pkg/` - Public libraries (use sparingly)
- `api/` - CRD type definitions only
- `cmd/` - Binary entry points (thin, delegate to internal/)

**Error Handling:**
- Always wrap errors with context: `fmt.Errorf("context: %w", err)`
- Use `%w` verb to preserve error chain
- Return errors up the call stack, handle at boundaries
- Log errors before returning in handlers/controllers

**Naming Conventions:**
- Interfaces: `SomethingDoer` suffix (e.g., `Reconciler`, `Authenticator`)
- Constructors: `NewThing()` returns `*Thing`
- Getters: Omit "Get" prefix (e.g., `Name()` not `GetName()`)
- Boolean fields: `Is`, `Has`, `Can` prefixes

**Dependencies:**
- Use `go.mod` for dependency management (no vendoring)
- Minimize external dependencies, prefer standard library
- Major dependencies: controller-runtime, cobra, gorm, prometheus

### Python Guidelines

**Package Structure:**
- UV workspaces for monorepo management
- Each package in `python/packages/` is independently versioned
- Use `pyproject.toml` for package configuration

**Code Style:**
- Ruff for formatting and linting (enforced in CI)
- Type hints for function signatures
- Pydantic for data validation
- FastAPI for web services

**ADK Compatibility:**
- Breaking changes OK during alpha stage
- Always update sample agents when breaking ADK
- Document migration path in PR

### TypeScript Guidelines

**Framework:**
- Next.js 14+ with App Router
- React Server Components where appropriate
- TailwindCSS + Shadcn/UI for styling

**Code Style:**
- Strict TypeScript mode
- Functional components with hooks
- No `any` types (use `unknown` if needed)

---

## Testing Requirements

### Required for All PRs

✅ **Unit tests** for new functions/methods
✅ **E2E tests** for new CRD fields or API endpoints
✅ **Mock external services** (LLMs, K8s API) in unit tests
✅ **All tests passing** in CI pipeline

### Go Testing Patterns

**Table-driven tests (preferred):**
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

**Mock LLMs:**
Use `github.com/kagent-dev/mockllm` for testing LLM interactions.

**E2E tests location:** [go/test/e2e/](go/test/e2e/)

### Python Testing

- Use `pytest` with async support
- Mock external API calls
- Test coverage for critical paths

### Running Tests

```bash
# All tests
make test

# Specific component
make -C go test
make -C python test
make -C ui test

# E2E tests (requires Kind cluster)
make -C go test-e2e

# With race detection (Go)
go test -race ./...
```

---

## API Design and Versioning

### Current API Versions

- **v1alpha2** (current) - All new features go here
- **v1alpha1** (legacy/deprecated) - Minimal maintenance only

### Versioning Policy

- Focus all new work on v1alpha2
- Breaking changes are acceptable in alpha versions
- Plan for v1beta1 when API stabilizes
- Use K8s conversion webhooks if needed for migration

### Adding CRD Fields

1. **Define in types.go** ([go/api/v1alpha2/agent_types.go](go/api/v1alpha2/agent_types.go))
2. **Add JSON/YAML tags** with `omitempty` for optional fields
3. **Add validation markers** (`+kubebuilder:validation:...`)
4. **Run codegen**: `make -C go generate`
5. **Update translator** if field affects K8s resources
6. **Add E2E test** verifying the new field works

Example:
```go
type AgentSpec struct {
    // ExistingField is an existing field
    // +kubebuilder:validation:Required
    ExistingField string `json:"existingField"`

    // NewField is a new optional field
    // +optional
    NewField *string `json:"newField,omitempty"`
}
```

### REST API Conventions

- Base path: `/api/`
- Resources: plural nouns (`/api/agents`, `/api/models`)
- CRUD: GET (list/read), POST (create), PUT (update), DELETE (delete)
- Use gRPC for performance-critical paths

---

## Code Patterns and Conventions

### Kubernetes Controller Patterns

#### Shared Reconciler Pattern

**When to use:** Evaluate case-by-case based on controller complexity.

The shared reconciler pattern is used for controllers that need database persistence:

```
AgentController
RemoteMCPServerController  ──→  kagentReconciler (shared)  ──→  Database
MCPServerController
```

**Key characteristics:**
- Single `kagentReconciler` instance shared across controllers
- Database handles concurrency (atomic upserts, transactions)
- No application-level locking needed
- Controllers translate CRs to K8s manifests via translators

**Implementation reference:** [go/internal/controller/reconciler/reconciler.go](go/internal/controller/reconciler/reconciler.go)

**When NOT to use:**
- Simple controllers without database persistence
- Controllers with unique reconciliation logic
- Custom status management needs

#### Controller File Structure

```go
// agent_controller.go
package controller

// 1. Reconciler struct
type AgentReconciler struct {
    client.Client
    Scheme *runtime.Scheme
    // ... dependencies
}

// 2. SetupWithManager registers controller
func (r *AgentReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&v1alpha2.Agent{}).
        WithEventFilter(predicate.Funcs{ /* custom predicates */ }).
        Complete(r)
}

// 3. Reconcile implements reconciliation logic
func (r *AgentReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. Fetch resource
    // 2. Handle deletion (finalizers)
    // 3. Reconcile (create/update child resources)
    // 4. Update status
}
```

### Database Patterns

**ORM:** GORM with SQLite (default) or Postgres (production)

**Schema evolution:** GORM AutoMigrate is sufficient during alpha stage.

**Models location:** [go/internal/database/models.go](go/internal/database/models.go)

**Common operations:**
```go
// Upsert (create or update)
db.Clauses(clause.OnConflict{
    Columns:   []clause.Column{{Name: "name"}},
    DoUpdates: clause.AssignmentColumns([]string{"description", "updated_at"}),
}).Create(&agent)

// Transaction
db.Transaction(func(tx *gorm.DB) error {
    // ... operations
    return nil
})
```

### Error Handling Patterns

**Go error wrapping:**
```go
// Always add context when wrapping
if err != nil {
    return fmt.Errorf("failed to create agent %s: %w", name, err)
}

// Use %w to preserve error chain for errors.Is/As
if errors.Is(err, ErrNotFound) {
    // Handle specific error
}
```

**HTTP handlers:**
```go
// Log then return error response
if err != nil {
    log.Error(err, "failed to process request")
    http.Error(w, "Internal server error", http.StatusInternalServerError)
    return
}
```

**Controllers:**
```go
// Return error to requeue with backoff
if err != nil {
    return ctrl.Result{}, fmt.Errorf("reconciliation failed: %w", err)
}

// Requeue after delay
return ctrl.Result{RequeueAfter: 5 * time.Minute}, nil

// Success, no requeue
return ctrl.Result{}, nil
```

### MCP Integration

**Adding new MCP servers:**
- All new MCP servers should be in separate repos/charts
- KMCP repository for K8s-native tools (Prometheus, Helm, etc.)
- Each MCP server is independently deployable

**MCP protocol implementation:** MCP-related HTTP handlers live under [go/internal/httpserver/handlers/](go/internal/httpserver/handlers/).

**Tool discovery:**
- Tools are fetched from MCP servers at runtime
- Cached for performance
- Refreshed on demand

### Commit Message Convention

Use **Conventional Commits** format:

```
<type>: <description>

[optional body]

[optional footer]
```

**Types:**
- `feat:` - New feature
- `fix:` - Bug fix
- `docs:` - Documentation only
- `refactor:` - Code change that neither fixes a bug nor adds a feature
- `test:` - Adding or updating tests
- `chore:` - Maintenance tasks, dependencies
- `perf:` - Performance improvement
- `ci:` - CI/CD changes

**Examples:**
```
feat: add support for custom service account in agent CRD

fix: enable usage metadata in streaming OpenAI responses

docs: update CLAUDE.md with testing requirements

chore: update trivy action and exit on failures
```

---

## Common Tasks

### Adding a New CRD Field

1. Edit type definition in `go/api/v1alpha2/*_types.go`
2. Add validation markers and JSON tags
3. Run `make -C go generate` to update generated code
4. Update translator in `go/internal/controller/translator/` if needed
5. Add E2E test in `go/test/e2e/`
6. Update Helm chart values/templates if exposed to users

### Creating a New Controller

1. Create controller file in `go/internal/controller/`
2. Implement `Reconcile()` method
3. Decide: use shared reconciler or custom logic
4. Add predicates for event filtering if needed
5. Register in `cmd/controller/main.go`
6. Add RBAC markers
7. Run `make -C go generate` to update RBAC manifests
8. Add unit and E2E tests

### Adding a New API Endpoint

1. Add handler in `go/internal/httpserver/handlers/`
2. Register route in `go/internal/httpserver/server.go`
3. Add auth middleware if needed
4. Update database models if storing data
5. Add unit tests for handler logic
6. Add E2E test for the full API flow
7. Update UI API client if user-facing

### Adding a New LLM Provider

1. Check if LiteLLM supports it (Python ADK uses LiteLLM)
2. Add provider enum to `go/api/v1alpha2/common_types.go`
3. Update ModelConfig validation
4. Add provider config struct to CRD types
5. Update Helm chart values
6. Add example ModelConfig YAML
7. Test with sample agent

### Adding Documentation

**When to document:**
- User-facing behavior changes → Update [README.md](README.md)
- Significant architectural decisions → Add to `docs/architecture/`
- New Helm values → Update `helm/kagent/README.md`

**When NOT to document:**
- Self-explanatory code (let code speak)
- Internal implementation details (use inline comments sparingly)
- Temporary/experimental features

### Troubleshooting Build Issues

**CRD generation fails:**
```bash
# Ensure controller-gen is installed
make -C go generate
```

**Go module issues:**
```bash
go mod tidy
go mod verify
```

**Python dependency issues:**
```bash
cd python
uv sync --all-packages
```

**Helm chart issues:**
```bash
helm lint helm/kagent
helm template test helm/kagent
```

**Kind cluster issues:**
```bash
make delete-kind-cluster
make create-kind-cluster
```

---

## Best Practices Summary

### Do's ✅

- Read existing code before making changes
- Follow the language guidelines (Go for infra, Python for agents, TS for UI)
- Write table-driven tests in Go
- Wrap errors with context using `%w`
- Use conventional commit messages
- Mock external services in unit tests
- Update documentation for user-facing changes
- Run `make lint` before submitting
- Test with a local Kind cluster

### Don'ts ❌

- Don't add features beyond what's requested (avoid over-engineering)
- Don't modify v1alpha1 unless fixing critical bugs (focus on v1alpha2)
- Don't vendor dependencies (use go.mod)
- Don't commit without testing locally first
- Don't use `any` type in TypeScript
- Don't add inline comments for self-explanatory code
- Don't skip E2E tests for API/CRD changes
- Don't create new MCP servers in the main kagent repo

---

## Getting Help

- **Development setup:** See [DEVELOPMENT.md](DEVELOPMENT.md)
- **Contributing:** See [CONTRIBUTING.md](CONTRIBUTING.md)
- **Architecture deep-dive:** See [docs/architecture/](docs/architecture/)
- **Examples:** Check `examples/` and `python/samples/`
- **Recent changes:** Check `git log` for patterns

---

## Quick Reference

| Task | Command |
|------|---------|
| Create Kind cluster | `make create-kind-cluster` |
| Deploy kagent | `make helm-install` |
| Build all | `make build` |
| Run all tests | `make test` |
| Run E2E tests | `make -C go e2e` |
| Lint code | `make lint` |
| Generate CRD code | `make -C go generate` |
| Run controller locally | `make -C go run` |
| Access UI | `kubectl port-forward -n kagent svc/kagent-ui 3000:80` |

---

**Last Updated:** 2026-02-03
**Project Version:** v0.x.x (Alpha)
**Maintained by:** Kagent Development Team

For questions or suggestions about this guide, please open an issue or PR.
