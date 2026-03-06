# Summary: Dynamic MCP UI Routing for Plugins

## Artifacts

| File | Description |
|------|-------------|
| `specs/dynamic-mcp-ui-routing/rough-idea.md` | Original idea with context |
| `specs/dynamic-mcp-ui-routing/requirements.md` | 13 Q&A decisions (Q1-Q9 original + Q10-Q13 routing fix, testing) |
| `specs/dynamic-mcp-ui-routing/research/r1-current-architecture.md` | Analysis of current plugin/nginx/sidebar patterns |
| `specs/dynamic-mcp-ui-routing/research/r2-mcp-ext-apps.md` | MCP Apps extension spec analysis and alignment |
| `specs/dynamic-mcp-ui-routing/design.md` | Full design: CRD, DB, controller, proxy, iframe, postMessage bridge, browser E2E |
| `specs/dynamic-mcp-ui-routing/plan.md` | 16-step plan (11 original + 5 fixes/testing) |

## Overview

This spec replaces hardcoded nginx proxy rules and static Next.js routes for MCP plugin UIs with a fully dynamic system:

1. **CRD** — `RemoteMCPServer.spec.ui` declares UI metadata (pathPrefix, displayName, icon, section)
2. **Controller** — Reconciles UI metadata into a `Plugin` database table
3. **Go reverse proxy** — `/_p/{name}/` routes dynamically based on DB lookup
4. **Nginx** — `location /_p/` proxies to Go backend; browser URL `/plugins/{name}` goes to Next.js
5. **Next.js** — Catch-all `/plugins/[name]/` renders iframe (src=`/_p/{name}/`) with postMessage bridge
6. **Sidebar** — Auto-discovers plugins via `/api/plugins`, renders nav items with loading/error states
7. **postMessage bridge** — Theme sync, resize, navigation, namespace, auth, badges

## Implementation Status

All 16 steps are complete.

| Step | Status | Description |
|------|--------|-------------|
| 1-11 | Done | Core implementation (CRD, DB, controller, proxy, UI, sidebar, migration, API E2E) |
| 12 | Done | **FIX**: Rename proxy path `/plugins/` → `/_p/` (nginx routing conflict) |
| 13 | Done | **FIX**: Add loading/error states (SidebarStatusProvider, StatusIndicator, plugin page fallback) |
| 14 | Done | Mock plugin service (Cypress fixtures + cy.intercept) |
| 15 | Done | Cypress browser E2E tests (8 scenarios in plugin-routing.cy.ts) |
| 16 | Done | CI integration (check-plugins-api.sh with --wait/--proxy, Makefile targets) |

## Bugs Fixed (Q10-Q11)

- **Nginx routing conflict**: `location /plugins/` caught browser URLs, breaking hard refresh. Fix: separate `/_p/` for proxy, `/plugins/` for browser.
- **Silent empty UI**: Sidebar `.catch(() => {})` swallowed errors; iframe showed blank on upstream failure. Fix: loading/error states with retry via SidebarStatusProvider context.

## Key Implementation Details

- Browser E2E tests use **Cypress** (not Playwright as originally planned) to match existing test infrastructure
- Sidebar status (loading/error/retry) is managed via `SidebarStatusProvider` React context, not inline in AppSidebarNav
- Plugin status page at `/plugins` provides health checks via server action `checkPluginBackend()`
- Next.js API route `/api/plugins` proxies to Go backend for client-side sidebar fetch
