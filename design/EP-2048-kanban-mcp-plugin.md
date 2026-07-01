# EP-2048: Kanban MCP server shipped as a kagent UI plugin

* Issue: [#2048](https://github.com/kagent-dev/kagent/issues/2048)

## Background

Agents frequently need a place to track work — tasks, boards, subtasks, progress —
both for their own multi-step plans and for human collaborators to observe what an
agent is doing. Today there is no first-class task/board primitive in kagent.

This EP introduces `kanban-mcp` (`go/plugins/kanban-mcp`): a self-contained MCP
server that

1. exposes **MCP tools** for task/board/subtask/attachment management that agents
   call as tools, and
2. ships its **own embedded web UI** (a board view + a real-time task-progress
   widget), surfaced inside the kagent console as a UI plugin via
   `RemoteMCPServer.spec.ui` (see EP-2047).

It is the first reference implementation of the "MCP server + embedded UI as a
kagent plugin" pattern, exercising both the plugin registration mechanism
(EP-2047) and the in-chat MCP UI widget mechanism (EP-2046).

## Motivation

- Give agents a durable, structured task/board store usable as tools.
- Give users a live board UI and per-task progress, embedded in the kagent console
  with the same theme/namespace chrome as the rest of the app.
- Validate the plugin and MCP-app extension points end-to-end with a real plugin.

### Goals

- A standalone Go MCP server (`go/plugins/kanban-mcp`) with:
  - SQLite **and** Postgres support (via a shared query layer).
  - Schema migrations and optional board seeding.
  - MCP tools for tasks, boards, subtasks, and attachments.
  - A REST API + SSE stream for the embedded UI's live updates.
  - An embedded SPA board UI and a `task-progress` MCP-app HTML resource.
- A Helm chart (`contrib/plugins/kanban-mcp`) that deploys the server and registers
  it as a kagent UI plugin via a `RemoteMCPServer` with `spec.ui.enabled`.

### Non-Goals

- Modifying the kagent controller, core HTTP server, or CRDs (handled by EP-2047).
- A general-purpose project-management product; scope is task/board primitives.
- Authn/authz beyond what kagent's plugin proxy and `RemoteMCPServer` provide.

## Implementation Details

### Layout (`go/plugins/kanban-mcp`)

```
main.go, server.go              entrypoint + HTTP/MCP server wiring
internal/config/                flags/env config (addr, transport, db-url, readonly, …)
internal/db/                    connect + sqlc-generated queries (gen/) + queries/*.sql
internal/migrations/            embedded SQL migrations (000001…000007)
internal/seed/                  optional board seeding from config
internal/service/               task/board/attachment/progress domain services
internal/mcp/tools.go           MCP tool definitions (tasks, boards, subtasks, attachments)
internal/api/handlers.go        REST handlers for the embedded UI
internal/sse/hub.go             SSE hub for live UI updates
internal/ui/                    embedded SPA (index.html) + task_progress.html (MCP app)
docs/mcp-app-task-progress.md   task-progress MCP app contract
```

- **Dual database support:** queries are authored once (`internal/db/queries/*.sql`),
  generated with sqlc (`internal/db/gen`), and run against SQLite or Postgres
  selected at runtime from the configured DB URL.
- **Migrations** are embedded and applied on startup; `internal/seed` can
  pre-populate boards from configuration.
- **Transports:** the MCP endpoint is served over Streamable HTTP (`/mcp`) and
  optionally stdio; the web UI, REST API, and SSE share the same listener.
- **Real-time:** `internal/sse/hub.go` fans out task/board changes so the board UI
  and the `task-progress` widget update live.

### Embedded UI and the task-progress MCP app

- `internal/ui/index.html` is the board SPA served at the server's web root `/`,
  surfaced in the kagent sidebar through `RemoteMCPServer.spec.ui` (EP-2047).
- `internal/ui/task_progress.html` is an **MCP app** resource rendered inline in the
  kagent chat (EP-2046): when an agent calls a kanban tool, the chat can render a
  live progress widget for the affected board/task.

### Deployment (`contrib/plugins/kanban-mcp`)

Helm chart deploying the server `Deployment`/`Service`, optional board `ConfigMap`
and `Secret`, an optional `Agent`, and a `RemoteMCPServer` that both points agents
at the `/mcp` endpoint (`spec.url`) and registers the web UI as a plugin:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: RemoteMCPServer
metadata:
  name: kanban-mcp
spec:
  protocol: STREAMABLE_HTTP
  url: http://kanban-mcp.kagent:8080/mcp
  ui:
    enabled: true
    pathPrefix: kanban
    displayName: Kanban
    icon: kanban
    section: PLUGINS
```

### Dependencies

- **EP-2047** (UI Plugins via `RemoteMCPServer.spec.ui`) — required to surface the
  board UI in the sidebar and reverse-proxy `/_p/kanban/`.
- **EP-2046** (Chat MCP UI widgets) — required to render the `task-progress` MCP
  app inline in chat. The server is usable without it (tools + standalone UI), but
  the in-chat widget needs it.

These are separate PRs; this PR is self-contained (all files live under
`go/plugins/kanban-mcp` and `contrib/plugins/kanban-mcp`) and builds
independently — it does not modify the kagent module's `go.mod` or any shared
file.

## Test Plan

- **Unit:** services (task/board/attachment/progress), config, migrations, seed,
  SSE hub, MCP tools, and REST handlers — all shipped with `*_test.go` coverage.
- **Integration:** `internal/integration/integration_test.go` exercises the server
  end-to-end against the embedded DB.
- **Build:** `go build ./plugins/kanban-mcp/...` passes within the `go/` module
  with no new module dependencies.
- **Manual / e2e:** `helm install` the chart, confirm the `RemoteMCPServer` is
  discovered, the board appears under "Plugins" in the sidebar, tools are callable
  by an agent, and the task-progress widget updates live in chat.

## Alternatives

- **External standalone service (not an MCP plugin):** loses agent-as-tools access
  and the embedded console integration.
- **New dedicated CRD instead of `RemoteMCPServer.spec.ui`:** rejected in EP-2047 in
  favor of reusing the existing CRD.
- **Postgres-only:** SQLite support keeps single-binary/local and lightweight
  deployments trivial.

## Open Questions

- Should board/task retention and archival be configurable?
- Should the task-progress widget support multiple concurrent boards in one chat?
