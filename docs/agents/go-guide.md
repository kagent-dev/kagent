# Kagent Go Development Guide

Go backend development: controllers, HTTP server, database, CLI, and Go ADK.

**See also:** [architecture.md](architecture.md) (system overview), [testing-ci.md](testing-ci.md) (test commands), [code-tree.md](code-tree.md) (code navigation)

---

## Local development

### Prerequisites

- Go 1.26.1+
- Docker with Buildx v0.23.0+
- Kind v0.27.0+
- kubectl v1.33.4+
- Helm 3

### Essential commands

| Task | Command |
|------|---------|
| Build all Go | `make -C go build` |
| Run unit tests | `make -C go test` |
| Run E2E tests | `make -C go e2e` |
| Lint | `make -C go lint` |
| Fix lint | `make -C go lint-fix` |
| Format | `make -C go fmt` |
| Vet | `make -C go vet` |
| Generate CRDs | `make -C go manifests` |
| Generate DeepCopy | `make -C go generate` |
| Vulnerability check | `make -C go govulncheck` |
| Build CLI (local) | `make build-cli-local` |

### Module structure

Single Go module: `github.com/kagent-dev/kagent/go` (Go 1.26.1)

```
go/
├── api/                 # Shared types (no internal business logic)
│   ├── v1alpha2/        # Current CRD definitions
│   ├── v1alpha1/        # Legacy CRDs (deprecated)
│   ├── adk/             # ADK configuration types
│   ├── client/          # REST HTTP client SDK
│   ├── database/        # GORM database models
│   ├── httpapi/         # HTTP request/response types
│   ├── utils/           # Shared utility functions
│   └── config/          # Generated CRD & RBAC manifests
├── core/                # Infrastructure
│   ├── cmd/             # Binary entry points (controller, CLI)
│   ├── cli/             # kagent CLI application
│   ├── internal/        # Core services
│   │   ├── controller/  # K8s controllers & reconciliation
│   │   ├── database/    # Database implementation (SQLite/Postgres)
│   │   ├── httpserver/  # HTTP API server
│   │   ├── a2a/         # Agent-to-Agent protocol
│   │   ├── mcp/         # MCP tool server integration
│   │   ├── metrics/     # Prometheus metrics
│   │   └── telemetry/   # OpenTelemetry tracing
│   └── pkg/             # Public packages (auth, translators)
└── adk/                 # Go Agent Development Kit
    ├── cmd/             # ADK server entry point
    ├── pkg/             # Agent runtime, sessions, skills, MCP client
    └── examples/        # Example tools (oneshot, BYO agents)
```

## Go coding standards

### Error handling

```go
// Always wrap errors with context using %w
if err != nil {
    return fmt.Errorf("failed to create agent %s: %w", name, err)
}

// Controllers: return error to requeue with backoff
if err != nil {
    return ctrl.Result{}, fmt.Errorf("reconciliation failed: %w", err)
}
```

### Naming conventions

- Exported identifiers: `PascalCase` (e.g., `AgentSpec`, `CreateAgent`)
- Unexported identifiers: `camelCase` (e.g., `agentName`, `parseConfig`)
- Descriptive variable names: `fingerPrint` not `fp`, `cacheKey` not `ck`
- Context as first parameter: `func DoSomething(ctx context.Context, ...) error`

### Testing pattern (table-driven)

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

### Concurrency rules

- No nested goroutines (`go func() { go func() { ... } }()`)
- Reuse K8s clients from struct fields, not `kubernetes.NewForConfig()` per call
- Fire-and-forget goroutines: require `context.WithTimeout` + error logging
- Database-level concurrency via atomic upserts, no application-level locks

## Controller development

### Adding a new controller

1. Define CRD types in `go/api/v1alpha2/`
2. Run `make -C go manifests generate` to generate CRD YAML and DeepCopy methods
3. Create controller in `go/core/internal/controller/`
4. Register with the controller manager in `go/core/cmd/controller/main.go`
5. Create translator in `go/core/internal/controller/translator/`
6. Add E2E tests in `go/core/test/e2e/`

### Reconciliation pattern

```go
func (r *MyController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    // 1. Fetch the resource
    // 2. Handle deletion (finalizers)
    // 3. Translate CRD spec to K8s resources
    // 4. Apply resources (create/update)
    // 5. Update status
    // 6. Upsert to database
    return ctrl.Result{}, nil
}
```

## Adding CRD fields

1. Add field to type struct in `go/api/v1alpha2/*_types.go`
2. Run `make -C go manifests generate`
3. Update translator in `go/core/internal/controller/translator/`
4. Update ADK types in `go/api/adk/types.go`
5. Mirror in Python types: `python/packages/kagent-adk/src/kagent/adk/types.py`
6. Update Helm CRD templates if needed
7. Add unit tests for new field handling
8. Add E2E test if user-facing

## HTTP API development

- Server: `go/core/internal/httpserver/`
- Routes registered with gorilla/mux
- Request/response types in `go/api/httpapi/`
- Client SDK in `go/api/client/`

## CLI development

- CLI: `go/core/cli/` using Cobra + Viper
- Build: `make build-cli-local`
- Install: `make kagent-cli-install`

## Linting

golangci-lint v2.11.3+ with configuration in `go/.golangci.yaml`:

```bash
make -C go lint          # Check
make -C go lint-fix      # Auto-fix
make -C go lint-config   # Validate config
```

Always run lint before pushing. CI will fail on lint errors including `ineffassign`, `staticcheck`, and `gofmt` violations.
