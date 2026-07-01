# Kanban MCP App: Task Progress widget

This document specifies the MCP App (SEP-1865) support added to the `kanban-mcp`
server: an interactive **task progress** widget that renders inline in Kagent
Chat instead of plain text.

It covers the server contract (tools + `ui://` resource), the View ↔ host
protocol, and how this maps onto the generic in-chat App Bridge that Kagent's UI
implements.

## 1. Goals

- Let an agent surface the live progress of a *specific* card (a Feature or a
  Task) as a rich, self-updating widget in the chat, not a wall of JSON.
- Stay **backward compatible**: the same tools return a useful text fallback, so
  non-UI hosts, logs, and the model still work.
- Be **standards-compliant** with MCP Apps (`io.modelcontextprotocol/ui`,
  `text/html;profile=mcp-app`, `ui://` resources) so the widget renders in any
  compliant host, not just Kagent.

## 2. Architecture

```
[Kagent Chat UI]  ── host, generic MCP Apps App Bridge (@mcp-ui/client AppRenderer)
   │  proxies tools/call + resources/read over /api/mcp-apps/{ns}/{name}
   ▼
[kagent core HTTP server]  ── MCPAppsHandler: list tools / read ui:// resource / call tool
   │  MCP streamable-HTTP (JSON-RPC)
   ▼
[kanban-mcp server]   <-- this plugin
   ├─ resource  ui://kanban/task-progress   (single-file HTML, text/html;profile=mcp-app)
   ├─ tool      show_task_progress           (_meta.ui.resourceUri -> renders the View)
   └─ tool      refresh_task_progress        (_meta.ui.visibility: ["app"] -> in-iframe refresh)
        │
        ▼
[Sandboxed iframe View]  ── self-contained vanilla JS, speaks MCP Apps postMessage protocol
```

The View never talks to the kanban server directly. Every call flows
View → host → core proxy → kanban-mcp. The host is the security boundary.

## 3. Server contract

### 3.1 UI resource

| Field | Value |
| --- | --- |
| URI | `ui://kanban/task-progress` |
| MIME | `text/html;profile=mcp-app` |
| Body | A single self-contained HTML document (inline CSS + JS, no external fetches) |

Registered via `server.AddResource`. The host fetches it through
`resources/read` (proxied by the core `MCPAppsHandler.HandleReadResource`, which
validates the `ui://` scheme and the exact MIME type).

### 3.2 Tools

Both tools share one input and one output shape and one handler; only their
`_meta.ui` differs.

**`show_task_progress`** — model- and app-visible (default visibility).

```jsonc
{
  "name": "show_task_progress",
  "inputSchema": { "id": "integer (card id)" },
  "_meta": { "ui": { "resourceUri": "ui://kanban/task-progress" } }
}
```

The presence of `_meta.ui.resourceUri` is what tells the host "render this tool's
result as an App". When the agent calls it, the host renders the View and pushes
the result into the iframe.

**`refresh_task_progress`** — app-only (`_meta.ui.visibility: ["app"]`).

```jsonc
{
  "name": "refresh_task_progress",
  "inputSchema": { "id": "integer (card id)" },
  "_meta": {
    "ui": { "resourceUri": "ui://kanban/task-progress", "visibility": ["app"] }
  }
}
```

The model never sees this tool (the ADK toolset filters it out). It exists only
so the View can re-fetch fresh progress data from inside the iframe (the
"Refresh" button / poll) without spawning a new chat turn.

### 3.3 Result shape

Every call returns:

- `content[0].text` — a one-line human summary (the **required** text fallback),
  e.g. `Feature "Checkout v2" is 60% complete — 3 of 5 child tasks done (in "Done").`
- `structuredContent` — the `TaskProgress` object the View renders:

```jsonc
{
  "task_id": 12, "title": "...", "kind": "feature" | "task",
  "status": "Develop", "assignee": "...", "labels": ["..."],
  "user_input_needed": false,
  "percent": 60, "done_count": 3, "total_count": 5,
  "summary": "...",
  "board": { "key": "default", "name": "Default", "columns": ["Inbox", ...], "done_column": "Done" },
  "columns":  [ { "status": "Inbox", "count": 1 }, ... ],   // feature only: child counts per column
  "children": [ { "id": 3, "title": "...", "status": "Done", "percent": 100, "done": true }, ... ], // feature
  "subtasks": [ { "id": 7, "title": "...", "percent": 0, "done": false }, ... ],                     // task
  "updated_at": "2026-..."
}
```

### 3.4 Progress computation

`done_column` is the board's last column.

- **Task with checklist subtasks:** `percent = round(done/total * 100)`.
- **Task without subtasks:** `percent = column position` of its status in the
  board's ordered columns (`index / (len-1) * 100`).
- **Feature child (`children[].percent`):** each child Task's own completion —
  its checklist ratio when it has checklist subtasks, else its column position.
  The View renders one progress bar per child from this value.
- **Feature (`percent`):** `mean(children[].percent)`;
  `done_count = children whose status == done_column`. With no children, falls
  back to the Feature's own column position.

## 4. View ↔ host protocol (MCP Apps / SEP-1865)

The View is a single self-contained HTML file that implements the MCP Apps
postMessage contract directly (no bundler, no runtime imports — the iframe has no
network access). Messages are JSON-RPC 2.0 over `window.parent.postMessage`.

Handshake and data flow:

1. View → host **request** `ui/initialize` `{ appCapabilities, appInfo, protocolVersion: "2026-01-26" }`.
2. Host → View **result** `{ hostCapabilities, hostInfo, hostContext }` (theme, locale, ...).
3. View → host **notification** `ui/notifications/initialized`.
4. Host → View **notification** `ui/notifications/tool-input` `{ arguments: { id } }`.
5. Host → View **notification** `ui/notifications/tool-result` `{ ...CallToolResult }` —
   the View renders `structuredContent`.
6. Refresh: View → host **request** `tools/call` `{ name: "refresh_task_progress", arguments: { id } }`
   → host returns a fresh `CallToolResult`; the View re-renders.
7. View → host **notification** `ui/notifications/size-changed` `{ width, height }`
   so the host can size the iframe to content.

The View also responds to host `ping` requests and tolerates unknown
host→View requests (replies with an empty result) for forward compatibility.

## 5. Host-side genericness (App Bridge)

The in-chat host is **not** kanban-specific. It is implemented once in the Kagent
UI/core and works for any MCP server that follows the contract above:

- **Discovery:** the UI lists each configured `RemoteMCPServer`'s tools via
  `/api/mcp-apps/{ns}/{name}/tools` and detects App tools purely by
  `_meta.ui.resourceUri`. Visibility (`app` / `model`) is read from
  `_meta.ui.visibility` — no tool names or payload keys are hard-coded.
- **Rendering:** `@mcp-ui/client`'s `AppRenderer` mounts the `ui://` HTML in a
  sandboxed iframe via a local `sandbox_proxy.html`, and proxies
  `resources/read` / `tools/call` back through the core `MCPAppsHandler`.
- **Routing:** app-only tools (`visibility: ["app"]`) are proxied in-iframe;
  model-visible tools invoked from the iframe are promoted to a normal chat
  tool-call turn. This is protocol-based and identical for every server.
- **Model loop guard:** the ADK wraps App-tool results the *model* sees with a
  terminal "already rendered" notice so the agent doesn't re-invoke the render
  tool on every refresh.

This means adding the task-progress widget required **no host changes** — only
the kanban server-side tools + resource defined here. Any other MCP server can
add a widget the same way.
