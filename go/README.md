# Kagent Go Workspace

This directory is a [Go workspace](https://go.dev/doc/tutorial/workspaces) (`go.work`) containing three modules that make up the Go components of Kagent.

## Modules

| Module | Path | Description |
|--------|------|-------------|
| **api** | `go/api/` | Shared types: CRD definitions, ADK model types, database models, HTTP client SDK |
| **core** | `go/core/` | Infrastructure: Kubernetes controllers, HTTP server, CLI, database implementation |
| **adk** | `go/adk/` | Go Agent Development Kit for building and running agents |

### Dependency graph

```
go/api  (shared types — no internal kagent deps)
  ^       ^
  |       |
go/core  go/adk
```

## Directory Structure

```
go/
├── go.work              # Go workspace file
├── Makefile              # Unified build targets
├── Dockerfile            # Shared multi-stage Docker build
│
├── api/                  # Shared types module
│   ├── v1alpha1/         # Legacy CRD types
│   ├── v1alpha2/         # Current CRD types
│   ├── adk/              # ADK config & model types
│   ├── database/         # GORM model structs & Client interface
│   ├── httpapi/          # HTTP API request/response types
│   ├── client/           # REST HTTP client SDK
│   ├── utils/            # Shared utility functions
│   └── config/           # Generated CRD & RBAC manifests
│
├── core/                 # Infrastructure module
│   ├── cmd/              # Controller binary entry point
│   ├── cli/              # kagent CLI application
│   ├── internal/         # Controllers, HTTP server, DB impl, A2A, MCP
│   ├── pkg/              # Auth, env vars, translator plugins
│   ├── hack/             # Development utilities (mock LLM, config gen)
│   └── test/e2e/         # End-to-end tests
│
└── adk/                  # Go Agent Development Kit module
    ├── cmd/              # ADK server entry point
    ├── pkg/              # Agent runtime, models, MCP, sessions, skills
    └── examples/         # Example tools (oneshot runner, BYO agent)
```

## Building

All commands are run from the `go/` directory via the unified Makefile.

```bash
# Generate CRD manifests and DeepCopy methods (after changing api/ types)
make generate
make manifests

# Build CLI binaries for all platforms
make build

# Build CLI for local development
make core/bin/kagent-local

# Run the controller locally
make run
```

## Testing

```bash
# Run all unit tests across the workspace
make test

# Run end-to-end tests (requires Kind cluster)
make e2e
```

## Code Quality

```bash
# Lint all modules
make lint

# Auto-fix lint issues
make lint-fix

# Format all modules
make fmt

# Vet all modules
make vet
```

## Docker

The workspace uses a single `Dockerfile` parameterized with `BUILD_PACKAGE`:

```bash
# Build controller image (default)
docker build --build-arg BUILD_PACKAGE=core/cmd/controller/main.go -t controller .

# Build Go ADK image
docker build --build-arg BUILD_PACKAGE=adk/cmd/main.go -t golang-adk .
```

In practice, use the root Makefile targets (`make build-controller`, `make build-golang-adk`).

## Quick Testing with Oneshot

The `adk/examples/oneshot` tool lets you test agent configs locally:

```bash
# Extract config from a running agent
kubectl get secret -n kagent k8s-agent -ojson | jq -r '.data."config.json"' | base64 -d > /tmp/config.json

# Run a single prompt
cd go/adk && go run ./examples/oneshot -config /tmp/config.json -task "Hello"
```
