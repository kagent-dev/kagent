# A2A Agent Tools

Kagent runtimes can expose another A2A agent as a tool. Each invocation sends an
A2A message with a context ID and returns the child result together with that
context ID. A binding may reuse one context for consecutive calls or isolate each
call in a fresh context.

If the child task enters `input_required`, the tool records the child task and
context IDs in the parent's approval state. Continuing the parent forwards the
answer to that same child task. Authentication, user identity, and lineage headers
are forwarded by A2A client interceptors.

The Go and Python implementations are:

- `go/adk/pkg/tools/remote_a2a_tool.go`
- `python/packages/kagent-adk/src/kagent/adk/_remote_a2a_tool.py`

Public cross-AgentInstance delegation remains tracked in the API v2 execution
plan; this runtime tool does not replace gateway-level delegation policy.
