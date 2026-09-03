# Native Harness runtimes

This directory contains the Actor-side runtimes for native agent harnesses.
Each runtime receives compiler-owned configuration, runs its native agent
process, and translates native events into the shared runtime event model. The
shared A2A executor then exposes those events through Kagent's private A2A
endpoint.

Controller-side compilation does not live here. It is under
[`core/v2/translator`](../core/v2/translator), where Kubernetes resources are
resolved into an immutable runtime revision and a versioned harness config.

## Request flow

```text
Harness + AgentTemplate
        |
        v
core/v2/translator/{claude,codex}   controller-side compilation
        |
        v
KAGENT_CONFIG_JSON + environment   immutable Actor inputs
        |
        v
harness/{claude,codex}/cmd         process entrypoint
        |
        v
internal/adapter -> internal/driver
        |
        v
runtime events -> runtime/a2a      private A2A task stream
```

## Layout

| Path | Responsibility |
| --- | --- |
| [`claude`](claude/README.md) | Claude Code configuration, materialization, process protocol, and image |
| [`codex`](codex/README.md) | Codex App Server configuration, JSON-RPC driver, and image |
| [`runtime/runtime.go`](runtime/runtime.go) | Small runtime-neutral turn, event, sink, and outcome model |
| [`runtime/a2a`](runtime/a2a) | Serializes Actor execution and maps runtime events to upstream A2A events |
| [`runtime/continuation`](runtime/continuation) | Atomically persists the one native continuation ID owned by an Actor |
| [`runtime/utils`](runtime/utils) | Bounded diagnostic capture, private-file materialization, and process-group signaling |

The native runtimes deliberately share only mechanisms. Claude- or
Codex-specific configuration and protocol behavior stays in the owning
harness.

## Runtime state

`DurableDir` contains private state that must survive Actor replacement and
snapshot restore, including the native continuation ID and native runtime home.
Ephemeral credentials and generated files that should not enter snapshots must
remain outside it. The utilities under `runtime/utils` enforce restrictive
permissions and atomic replacement for private files.

Upstream A2A owns public tasks, contexts, history, and streaming semantics. A
native continuation ID is private Actor state; it is not a second public session
model.

## Development

From `go/`, run the focused unit tests with:

```bash
go test ./harness/...
```

The root Makefile builds the runtime images with `make build-claude-harness`
and `make build-codex-harness`.
