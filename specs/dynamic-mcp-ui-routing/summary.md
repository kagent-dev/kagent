# Summary: Dynamic MCP UI Routing for Plugins

## Artifacts

| File | Description |
|------|-------------|
| `specs/dynamic-mcp-ui-routing/rough-idea.md` | Original idea with context |
| `specs/dynamic-mcp-ui-routing/requirements.md` | 9 Q&A decisions covering proxy, CRD, sidebar, iframe, migration |
| `specs/dynamic-mcp-ui-routing/research/r1-current-architecture.md` | Analysis of current plugin/nginx/sidebar patterns |
| `specs/dynamic-mcp-ui-routing/research/r2-mcp-ext-apps.md` | MCP Apps extension spec analysis and alignment |
| `specs/dynamic-mcp-ui-routing/design.md` | Full design: CRD, DB, controller, proxy, iframe, postMessage bridge |
| `specs/dynamic-mcp-ui-routing/plan.md` | 11-step incremental implementation plan |

## Overview

This spec replaces hardcoded nginx proxy rules and static Next.js routes for MCP plugin UIs with a fully dynamic system:

1. **CRD** — `RemoteMCPServer.spec.ui` declares UI metadata (pathPrefix, displayName, icon, section)
2. **Controller** — Reconciles UI metadata into a `Plugin` database table
3. **Go reverse proxy** — `/plugins/{name}/` routes dynamically based on DB lookup
4. **Nginx** — Single `location /plugins/` block proxies to Go backend (one-time change)
5. **Next.js** — Catch-all `/plugins/[name]/` renders iframe with postMessage bridge
6. **Sidebar** — Auto-discovers plugins via `/api/plugins`, renders nav items dynamically
7. **postMessage bridge** — Theme sync, resize, navigation, namespace, auth, badges

Kanban-mcp migrates to the new system as proof-of-concept. Deploying a new MCP server with `ui.enabled: true` automatically makes its UI accessible — no core codebase changes needed.

## Next Steps

- Implement using the 11-step plan in `plan.md`
- Steps 1-6 (backend) can be developed and tested independently from steps 7-9 (frontend)
- Step 10 (kanban migration) validates the full pipeline end-to-end
