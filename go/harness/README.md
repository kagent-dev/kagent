# Native Harness runtimes

This directory contains the Actor-side runtimes for native agent harnesses.
Each runtime receives compiler-owned configuration, runs its native agent
process, and translates native events into the shared runtime event model. The
shared A2A executor then exposes those events through Kagent's private A2A
endpoint.

Controller-side compilation does not live here. It is under
[`core/internal/translator`](../core/internal/translator), where Kubernetes resources are
resolved into an immutable runtime revision and a versioned harness config.

## Request flow

```text
Harness + AgentTemplate
        |
        v
core/internal/translator/{claude,codex}   controller-side compilation
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
| [`runtime/runtime.go`](runtime/runtime.go) | Public runtime-neutral turn, event, sink, and outcome model |
| [`runtime/a2a`](runtime/a2a) | Serializes Actor execution and maps runtime events to upstream A2A events |
| [`runtime/continuation`](runtime/continuation) | Atomically persists the one native continuation ID owned by an Actor |
| [`internal/forwarder`](internal/forwarder) | Loopback routes that add the caller's credential to MCP and model requests |
| [`internal/utils`](internal/utils) | Bounded diagnostic capture, private-file materialization, and process-group signaling |

The native runtimes deliberately share only mechanisms. Claude- or
Codex-specific configuration and protocol behavior stays in the owning
harness.

## Runtime state

`DurableDir` contains private state that must survive Actor replacement and
snapshot restore, including the native continuation ID and native runtime home.
Ephemeral credentials and generated files that should not enter snapshots must
remain outside it. The utilities under `internal/utils` enforce restrictive
permissions and atomic replacement for private files.

Upstream A2A owns public tasks, contexts, history, and streaming semantics. A
native continuation ID is private Actor state; it is not a second public session
model.

## Human-in-the-loop runtime contract

A native driver returns ordinary completion or failure through
`runtime.Outcome`. When the native process instead pauses for approval or user
input, the outcome contains a `runtime.PendingTurn`:

```go
type PendingTurn interface {
        Request() InputRequest
        Resume(context.Context, InputResponse, EventSink) (Outcome, error)
        Cancel(context.Context, ModelCallSink) error
}
```

The handle owns the live native process or session. The shared A2A executor
keeps it while the task is `input-required`, then calls either `Resume` with the
validated response or `Cancel`. A resumed process can return another pending
handle for sequential questions.

The executor also serializes access to the Actor's one native conversation. Its
state is either active, parked on a `PendingTurn`, canceling that handle, or
idle. Drivers therefore implement native protocol and process cleanup without
maintaining a second global parked-task state.

## Caller credentials

The Go ADK sends the caller's bearer token on MCP calls under
`runtimeDefaults.tokenPropagation.enabled`, to a RemoteMCPServer with
`tokenPassthrough`, and to a model with ModelConfig `tokenPassthrough`. Native
harnesses get the same behavior from a loopback forwarder in the harness process.

- The adapter starts the forwarder when a route is needed and rewrites the native
  configuration to point at loopback routes: `/mcp/<server>` for each fronted MCP
  server and `/model` for the model. The native process authenticates with a
  per-process token, as a bearer token or, for an Anthropic client, as `x-api-key`.
- The A2A executor binds the `authorization` of each request around `Run` and
  `Resume`, for a runner that implements `CallerCredentialBinder`, and drops it before
  the outcome is handled. A parked or suspended Actor holds no credential, and a
  resumed turn runs as the request that resumed it.
- An MCP route sends the caller's credential first and the server's compiled headers
  last, so a static `Authorization` wins. A passthrough server gets the caller's
  credential only, and a turn without one is refused with 401. Of the native
  request's headers, only those of the streamable HTTP transport and of trace
  propagation are forwarded.
- The model route sends the caller's bearer token in the provider's API key header,
  keeps the client's other headers, and refuses a turn without a bearer token.
- A path with a `.` or `..` segment, decoded or not, an encoded slash, backslash or
  `%`, a `;` or a control byte is refused with 400, since the path is joined onto
  the upstream URL.

The caller's credential never enters the native process's environment or files.

## Development

From `go/`, run the focused unit tests with:

```bash
go test ./harness/...
```

The root Makefile builds the runtime images with `make build-claude-harness`
and `make build-codex-harness`.
