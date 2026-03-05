# Research: MCP Apps Extension Specification

Source: https://apps.extensions.modelcontextprotocol.io/api/

## Overview

MCP Apps is an official MCP extension (spec version 2026-01-26) that enables MCP tool servers to declare interactive UIs alongside their tools. The UI is rendered inline in the host (Claude, ChatGPT, etc.) inside a sandboxed iframe.

## Key Pattern: `ui://` Resources

Tools declare UI via `_meta.ui.resourceUri` pointing to a `ui://` resource:

```typescript
registerAppTool(server, "get-time", {
  title: "Get Time",
  description: "Returns the current server time.",
  inputSchema: {},
  _meta: { ui: { resourceUri: "ui://get-time/mcp-app.html" } },
}, handler);
```

The server also registers the resource itself (serves bundled HTML):

```typescript
registerAppResource(server, resourceUri, resourceUri,
  { mimeType: RESOURCE_MIME_TYPE },
  async () => ({ contents: [{ uri: resourceUri, mimeType: RESOURCE_MIME_TYPE, text: html }] })
);
```

## Discovery Flow

1. **Tool Definition** — Server declares tools with `_meta.ui.resourceUri`
2. **Tool Invocation** — LLM calls the tool
3. **Resource Fetch** — Host fetches the `ui://` resource from the server
4. **Sandbox Rendering** — Host displays HTML in an isolated iframe

## Bidirectional Communication

- Host → UI: Passes tool results via `PostMessageTransport` notifications
- UI → Host: Can call other tools via `app.callServerTool()`
- SDK: `@modelcontextprotocol/ext-apps` (client), `ext-apps/server` (server), `ext-apps/app-bridge` (host)

## Relevance to kagent

The MCP Apps pattern is designed for **inline tool UIs in chat clients** (rendered per-tool-invocation in an iframe). kagent's need is different but related:

- **MCP Apps**: Tool-scoped UI, rendered inline in chat, iframe-sandboxed, per-invocation
- **kagent plugins**: Full-page application UIs (dashboards, boards), rendered in sidebar shell, persistent navigation

**Design consideration**: kagent could support BOTH patterns:
1. **Full-page plugin UIs** (kanban, git repos) — via Go reverse proxy at `/plugins/{name}/`
2. **MCP App inline UIs** (future) — via `ui://` resource fetching in chat views

For now, the full-page plugin pattern is the priority. But the CRD metadata design should not conflict with future MCP Apps support. The `_meta.ui.resourceUri` pattern from MCP Apps could inform how we discover which MCP servers have UIs.

## Key Takeaway

The MCP Apps spec validates the concept of MCP servers declaring UI capabilities. kagent's approach should:
- Use a similar metadata declaration pattern (tool server declares "I have a UI")
- Not duplicate the MCP Apps inline rendering (that's for chat clients)
- Focus on the full-page dashboard/app use case that MCP Apps doesn't cover
