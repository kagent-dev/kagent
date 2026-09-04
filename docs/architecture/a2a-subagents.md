# A2A Agent Tools

Kagent has two distinct subagent mechanisms. They should not be confused.

```mermaid
flowchart TB
    PARENT[Parent agent]
    PARENT -->|Shared binding compiled into one runtime| LOCAL[Native in-process subagent]
    PARENT -->|remote A2A tool call| REMOTE[Addressable A2A agent]
    REMOTE -->|task + context IDs retained| CONTINUE[input-required continuation]
    DEDICATED[Dedicated binding] -. deferred .-> SEPARATE[separate AgentInstance]
    PUBLIC[Public cross-instance delegation] -. deferred .-> POLICY[credential and lineage policy]
```

## Shared agent tools

An `AgentTemplate` can bind another template as a `Shared` agent tool. The
translator resolves the referenced template in the same compilation tree and
the selected harness compiler emits its native, in-process representation.
Kagent, Codex, and Claude support Shared bindings according to their runtime
capabilities.

Tree resolution detects missing references and cycles before compilation.
`Dedicated` bindings are represented in the API but are currently rejected;
they do not create a separate AgentInstance today.

## Runtime remote A2A tools

The Go and Python ADKs also contain a remote A2A tool. Each call sends an A2A
message to an already-addressable remote agent and preserves the child task and
context IDs. If the child enters `input-required`, the parent can retain those
identifiers and continue the same child task after receiving human input.

Implementations:

- [`go/adk/pkg/tools/remote_a2a_tool.go`](../../go/adk/pkg/tools/remote_a2a_tool.go)
- [`python/packages/kagent-adk/src/kagent/adk/_remote_a2a_tool.py`](../../python/packages/kagent-adk/src/kagent/adk/_remote_a2a_tool.py)

This runtime helper is not public cross-AgentInstance delegation. Gateway-level
delegation still requires scoped credentials, lineage/depth/cycle enforcement,
and streamed child execution; that work is deferred in the execution plan.
