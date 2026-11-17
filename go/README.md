# Kagent Go

This directory is a single Go module (`github.com/kagent-dev/kagent/go`) containing the top-level package trees that make up the Go components of Kagent.

## Packages

| Package | Path | Description |
|---------|------|-------------|
| **api** | `go/api/` | Public API: CRD types, generated protobuf code, gRPC and A2A clients, Kubernetes clientset |
| **core** | `go/core/` | Control plane: Kubernetes controllers, gRPC and HTTP servers, PostgreSQL store, CLI |
| **adk** | `go/adk/` | Go Agent Development Kit for building and running agents |
| **harness** | `go/harness/` | Native Claude and Codex Actor runtimes plus their shared A2A execution support |
| **pkg** | `go/pkg/` | Logging, OpenTelemetry setup, and the shared tracing contract |
| **sandbox** | `go/sandbox/` | Sandbox guest image and entry point |

### Dependencies between trees

`go/api` does not import any other tree in this module, so importing its packages
does not compile the controller, ADK, or harness code. The other trees import each
other at the package level:

- `core` uses `adk` and `harness` configuration packages to compile runtime inputs.
- `adk` and `harness` use `core` packages such as `core/pkg/env` and `core/pkg/config`.
- `pkg` uses `core/pkg/env`; `sandbox` uses `pkg`.

Run `go list -deps ./<tree>/...` to check the current graph.

## Directory Structure

```
go/
├── go.mod               # Single Go module file
├── Makefile              # Unified build targets
├── Dockerfile            # Shared multi-stage Docker build
│
├── api/                  # Public API types and clients
│   ├── v1alpha3/         # Current CRD types
│   ├── gen/kagent/       # Generated protobuf code (api, config, runtime)
│   ├── client/           # gRPC and A2A client SDK
│   ├── clientset/        # Generated Kubernetes clientset
│   ├── a2a/              # A2A metadata and runtime identity helpers
│   ├── agentplugin/      # Skill and Agent Plugin package configuration
│   ├── authorization/    # Authorization scope types
│   ├── bootstrap/        # Runtime configuration bundles
│   ├── sandbox/          # Sandbox routing metadata
│   ├── scheduledrun/     # ScheduledRun scheduling semantics
│   ├── structuredobject/ # StructuredObject conversion helpers
│   └── hack/             # Code generation boilerplate
│
├── core/                 # Control plane
│   ├── cmd/              # Controller binary entry point
│   ├── cli/              # kagent CLI application
│   ├── internal/         # Controllers, gRPC and HTTP servers, store, A2A gateway, MCP, Substrate
│   ├── pkg/              # App wiring, auth, config, env vars, install, migrations
│   ├── hack/             # Development utilities (mock LLM, agent config generator)
│   └── test/e2e/         # End-to-end tests
│
├── adk/                  # Go Agent Development Kit
│   ├── cmd/              # ADK server entry point
│   ├── pkg/              # Agent runtime, models, MCP, sessions, skills
│   └── examples/         # Example tools (oneshot runner, BYO agent)
│
├── harness/              # Native Harness Actor runtimes
│   ├── claude/           # Claude Code adapter and image
│   ├── codex/            # Codex App Server adapter and image
│   ├── runtime/          # Public event, A2A executor, and continuation APIs
│   └── internal/utils/   # Private OS utilities
│
├── pkg/                  # Shared observability libraries
│   ├── logging/          # JSON logger
│   ├── telemetry/        # OpenTelemetry SDK setup
│   └── tracing/          # Invocation span contract shared by runtimes
│
└── sandbox/
    └── guest/            # Sandbox guest image and entry point
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

`make run` reads `/etc/kagent/controller.yaml`, the path the Helm chart mounts. To
run with another file, use `go run ./core/cmd/controller --config <file>`; see
[controller configuration](../docs/architecture/process-configuration.md#controller).

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
docker build --build-arg BUILD_PACKAGE=./core/cmd/controller -t controller .

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

The chart supplies typed `harnessImages` in the mounted `controller.yaml` file.
For built-in Harnesses, `workload: {}` selects the default image. An explicit
`workload.image` overrides it and must include its `@sha256:` digest. BYO requires
an explicit image and command. This works for inline and referenced Harnesses.
`Agent.status.workloadImage` reports the image selected for its desired revision.

Local installations can set `config.harnessImages.kagent`, `.claude`, and
`.codex` to full digest-pinned references. Missing defaults only prevent Agents
that need those entries from compiling. `global.imageRegistry` rewrites the
registry of these defaults; it does not rewrite explicit Harness overrides.
A supplied `config.harnessImages.release` must match the controller version;
omit it for independently built local images.

The native images use `make build-claude-harness` and `make build-codex-harness`.
`kagent-adk` remains available for Python runtime overrides.

## Quick Testing with Oneshot

The `adk/examples/oneshot` tool lets you test agent configs locally:

```bash
# Run a single prompt with a sample config (requires OPENAI_API_KEY)
cd go/adk && go run ./examples/oneshot -config examples/oneshot/testdata/openai.json -task "Hello"
```

[`adk/examples/oneshot/testdata`](adk/examples/oneshot/testdata) has a config
for each supported model provider and lists the environment variables each needs.
