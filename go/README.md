# Kagent Go

This directory is a single Go module (`github.com/kagent-dev/kagent/go`) containing four top-level package trees that make up the Go components of Kagent.

## Packages

| Package | Path | Description |
|---------|------|-------------|
| **api** | `go/api/` | Shared types: CRD definitions, ADK model types, database models, HTTP client SDK |
| **core** | `go/core/` | Infrastructure: Kubernetes controllers, HTTP server, CLI, database implementation |
| **adk** | `go/adk/` | Go Agent Development Kit for building and running agents |
| **harness** | `go/harness/` | Native Claude and Codex Actor runtimes plus their shared A2A execution support |

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
├── go.mod               # Single Go module file
├── Makefile              # Unified build targets
├── Dockerfile            # Shared multi-stage Docker build
│
├── api/                  # Shared types module
│   ├── v1alpha3/         # Current CRD types
│   ├── adk/              # ADK config & model types
│   ├── database/         # database model structs & Client interface
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
├── adk/                  # Go Agent Development Kit
│   ├── cmd/              # ADK server entry point
│   ├── pkg/              # Agent runtime, models, MCP, sessions, skills
│   └── examples/         # Example tools (oneshot runner, BYO agent)
│
└── harness/              # Native Harness Actor runtimes
    ├── claude/           # Claude Code adapter and image
    ├── codex/            # Codex App Server adapter and image
    ├── runtime/          # Public event, A2A executor, and continuation APIs
    └── internal/utils/  # Private OS utilities
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

### Agent runtime image digests

Published Helm charts contain the matching release's digest-pinned `golang-adk`,
`claude-harness`, and `codex-harness` images. Release packaging consumes each
image build's final Buildx digest; controller builds run independently and never
wait for runtime-image digests. OCI and GitHub releases share the same chart archive.

Package a release with:

```sh
make helm-release VERSION=1.0.0-alpha9 \
  RUNTIME_IMAGE_METADATA_DIR=/path/to/buildx-metadata
```

The directory must contain
`golang-adk.json`, `claude-harness.json`, and `codex-harness.json` from those builds.
The target validates the digests and packages the catalog with the charts in a
temporary directory, then writes the completed archives to `dist`. It does not
modify the chart sources. `make helm-publish` requires the same metadata input.
For local registries, set `RUNTIME_IMAGE_REPOSITORY=localhost:5001/kagent-dev/kagent`.

The chart supplies `KAGENT_BUILTIN_HARNESS_IMAGES` through the controller ConfigMap.
For built-in Harnesses, `workload: {}` selects the default image. An explicit
`workload.image` overrides it and must include its `@sha256:` digest. BYO requires
an explicit image and command. This works for inline and referenced Harnesses.
`Agent.status.workloadImage` reports the image selected for its desired revision.

Local installations can set `controller.harnessImages.kagent`, `.claude`, and
`.codex` to full digest-pinned references. Missing defaults only prevent Agents
that need those entries from compiling. `global.imageRegistry` rewrites the
registry of these defaults; it does not rewrite explicit Harness overrides.
A supplied `controller.harnessImages.release` must match the controller version;
omit it for independently built local images.

The native images use `make build-claude-harness` and `make build-codex-harness`.
`kagent-adk` remains available for Python runtime overrides.

## Quick Testing with Oneshot

The `adk/examples/oneshot` tool lets you test agent configs locally:

```bash
# Extract config from a running agent
kubectl get secret -n kagent k8s-agent -ojson | jq -r '.data."config.json"' | base64 -d > /tmp/config.json

# Run a single prompt
cd go/adk && go run ./examples/oneshot -config /tmp/config.json -task "Hello"
```
