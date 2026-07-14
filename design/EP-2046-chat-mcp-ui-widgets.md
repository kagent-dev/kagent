# EP-2046: Chat UI support for MCP UI widgets (MCP Apps)

* Issue: [#2046](https://github.com/kagent-dev/kagent/issues/2046)

## Background

The Model Context Protocol is gaining an "Apps"/UI extension
(`@modelcontextprotocol/ext-apps`, rendered via `@mcp-ui/client`) that lets an MCP
server attach an interactive HTML/UI **resource** to a tool. When an agent calls
such a tool, the client can render a live widget instead of (or in addition to) raw
tool-call JSON.

kagent's chat today renders tool calls as collapsible JSON. This EP makes the chat
MCP-App–aware: when a tool call maps to an MCP app resource, the chat renders the
app inline in a sandboxed frame and brokers messages between the app and the chat
(send a message on the user's behalf, surface "visible" tool calls, proxy resource
reads and tool calls back to the originating MCP server).

## Motivation

- Let MCP servers deliver rich, interactive results (forms, boards, charts, live
  progress) directly in the kagent chat.
- Provide the in-chat rendering half of the kagent plugin story (the sidebar/plugin
  half is EP-2047; the first consumer is the Kanban task-progress widget, EP-2048).

### Goals

- Discover MCP app resources per MCP server and associate them with tool calls.
- Render the app via a sandboxed renderer inside chat messages / tool-call display.
- Broker host↔app messaging: `sendMessage`, visible tool calls, and proxying of
  resource reads and tool calls to the backend MCP server.
- Backend endpoints to list an MCP server's tools, read its resources, and call its
  tools on behalf of the UI.

### Non-Goals

- The sidebar plugin/registration mechanism (EP-2047).
- Shipping a specific MCP app (the Kanban task-progress app is EP-2048).
- File-upload / artifact handling — note the chat files carry adjacent
  file-upload/minimap code (see "Adjacent code" below); that feature is tracked
  separately and is **not** part of this EP's scope.

## Implementation Details

### Backend

- **`go/adk/pkg/mcp/registry.go`** — `CreateToolsets` now also returns the set of
  **MCP-app–capable tool names** (tools whose MCP server advertises a UI resource),
  so the agent can treat their results specially.
- **`go/adk/pkg/agent/mcp_apps.go`** — `MakeMCPAppModelResultCallback`: for
  MCP-app tools, keep the rich tool payload in chat history for UI rendering while
  compacting what is sent back to the model (avoids redundant polling/tool churn).
  Wired in `agent.go` only when `len(mcpAppToolNames) > 0`.
- **`go/core/internal/httpserver/handlers/mcpapps.go`** — `MCPAppsHandler` with
  `HandleListTools`, `HandleCallTool`, `HandleReadResource`, exposed under
  `/api/mcp-apps/{namespace}/{name}/...`. (Only the MCP-apps hunks of the shared
  `server.go`/`handlers.go` are included here; the plugins hunks belong to EP-2047.)

### UI (`ui/src`)

- **`components/mcp-apps/McpAppRenderer.tsx`** — renders an MCP app resource via
  `@mcp-ui/client` in a sandbox, wiring its `onUIAction`/resource-read/tool-call
  callbacks to the backend; `McpAppsInspector.tsx` is a standalone inspector view
  (surfaced at `app/apps/[appName]/page.tsx`, reachable by clicking an MCP app
  listed alongside a server's tools in `components/mcp/McpServersView.tsx`).
- **`components/chat/ChatMcpAppsContext.tsx`** — context that maps a tool name to its
  MCP app (`getMcpAppForTool`) and brokers `sendMessage` / `McpAppVisibleToolCall`
  between an app and the chat.
- **`components/chat/ChatLayoutUI.tsx`** — mounts `ChatMcpAppsProvider` around the
  chat subtree so the MCP-app context is active for every chat session (without this
  mount, tool calls never resolve to apps and no widget renders).
- **`components/chat/ChatInterface.tsx`, `ChatMessage.tsx`, `ToolCallDisplay.tsx`,
  `components/ToolDisplay.tsx`** — render the app for MCP-app tool calls and forward
  app actions.
- **`app/actions/mcp-apps.ts`** + **`app/api/mcp-apps/.../{resources,tools/.../call}`**
  — server actions / BFF routes calling the backend MCP-apps endpoints.
- **`public/sandbox_proxy.html`** — sandbox proxy document for the app iframe.

### New dependencies (`ui/package.json`)

- `@mcp-ui/client` `^7.1.1`
- `@modelcontextprotocol/ext-apps` `^1.7.1`
- `@modelcontextprotocol/sdk` `^1.29.0`

The lockfile (`ui/package-lock.json`) and the generated `ui/public/mockServiceWorker.js`
(MSW worker, bumped `2.14.2` → `2.14.6`) are regenerated as a side effect of resolving
the new dependency tree.

### Adjacent code

Per the agreed split, the chat files (`ChatInterface.tsx`, `ChatMessage.tsx`,
`messageHandlers.ts`) are taken whole and therefore also carry the chat
**file-upload** (`lib/fileUpload.ts`, `chat/FileAttachment.tsx`) and **minimap**
(`chat/ChatMinimap.tsx`) UI that was developed alongside MCP apps. These are
included so the chat compiles, but are not the subject of this EP; the file-upload
backend (artifact extraction, `save_artifact`) is intentionally **excluded**.

## Test Plan

- **Unit (Go):** `registry_test.go` (MCP-app tool-name detection) and
  `mcp_apps_test.go` (model-result callback). `go build ./adk/... ./core/...` and
  test compilation pass.
- **Unit (UI):** `getMcpAppForTool` mapping (`ChatMcpAppsContext.test.tsx`); mcp-apps
  server actions (`actions/__tests__/mcp-apps.test.ts`); and a regression test
  (`chat/__tests__/ChatLayoutUI.test.tsx`) asserting `ChatLayoutUI` mounts
  `ChatMcpAppsProvider` around the chat so widgets can render.
- **Manual / e2e:** point the chat at an MCP server exposing a UI resource; confirm
  the widget renders inline, `sendMessage` posts to the chat, and resource/tool-call
  proxying reaches the server. The Kanban task-progress widget (EP-2048) is the
  reference end-to-end case.

## Alternatives

- **Render apps only in a side panel (not inline in chat):** loses the
  tool-call→widget association and the conversational flow.
- **Trust the model with full tool payloads:** causes token bloat and tool churn;
  hence the model-result compaction callback.

## Open Questions

- Should MCP-app rendering be opt-in per MCP server (a `spec` flag) rather than
  inferred from advertised UI resources?
- How should multiple apps in a single conversation share/scope state?
