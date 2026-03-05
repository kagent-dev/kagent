# PROMPT: Dynamic MCP UI Routing for Plugins

## Objective

Implement dynamic UI routing for MCP plugins in kagent. MCP tool servers declare UI metadata in their RemoteMCPServer CRD. The Go backend discovers these declarations, persists them, and reverse-proxies plugin UIs at `/plugins/{name}/`. The Next.js UI renders plugins in sandboxed iframes with a postMessage bridge. Migrate the existing kanban integration to this system.

## Key Requirements

- Extend `RemoteMCPServer` CRD with optional `spec.ui` section (`enabled`, `pathPrefix`, `displayName`, `icon`, `section`)
- Add `Plugin` database model persisted by the controller from CRD UI metadata
- Go reverse proxy at `/plugins/{name}/` using `net/http/httputil.ReverseProxy` with DB-driven lookup
- New `/api/plugins` endpoint returning plugin UI metadata for sidebar discovery
- One-time nginx `location /plugins/` block proxying to Go backend; remove hardcoded `/kanban-mcp/` block
- Next.js catch-all `/plugins/[name]/[[...path]]/page.tsx` renders iframe with `sandbox="allow-scripts allow-same-origin allow-forms allow-popups"`
- postMessage bridge (`kagent:` prefix): theme sync, resize, navigation, namespace context, auth forwarding, badge updates
- Sidebar dynamically merges plugin nav items into configured sections via `/api/plugins` fetch
- Migrate kanban-mcp: add `ui` section to Helm RemoteMCPServer template, delete `ui/src/app/kanban/page.tsx`, remove static sidebar entry
- Plugin bridge SDK snippet (`kagent-plugin-bridge.js`) for plugin developers

## Acceptance Criteria

```gherkin
Given a RemoteMCPServer CRD with ui.enabled=true and ui.pathPrefix="kanban"
When the controller reconciles
Then GET /api/plugins returns the plugin metadata
And GET /plugins/kanban/ reverse-proxies to the plugin service
And the sidebar shows "Kanban Board" under the AGENTS section
And the plugin renders in a sandboxed iframe with postMessage bridge
And theme/namespace changes propagate to the iframe
And the plugin can update its sidebar badge via postMessage
And deleting the CRD removes the plugin from /api/plugins and sidebar
And no hardcoded /kanban-mcp/ nginx route or ui/src/app/kanban/page.tsx exists
```

## Reference

- Full design: `specs/dynamic-mcp-ui-routing/design.md`
- Implementation plan (11 steps): `specs/dynamic-mcp-ui-routing/plan.md`
- Requirements decisions: `specs/dynamic-mcp-ui-routing/requirements.md`
