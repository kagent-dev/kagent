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

Steps 1-11 are implemented. Steps 12-16 address bugs and testing gaps found during review:

| Step | Status | Description |
|------|--------|-------------|
| 1-11 | Done | Core implementation (CRD, DB, controller, proxy, UI, sidebar, migration, API E2E) |
| 12 | TODO | **FIX**: Rename proxy path `/plugins/` → `/_p/` (nginx routing conflict) |
| 13 | TODO | **FIX**: Add loading/error states to sidebar and plugin page |
| 14 | TODO | Mock plugin service for browser E2E tests |
| 15 | TODO | Playwright browser E2E tests (7 scenarios) |
| 16 | TODO | CI integration (API verification + Playwright) |

## Bugs Fixed (Q10-Q11)

- **Nginx routing conflict**: `location /plugins/` caught browser URLs, breaking hard refresh. Fix: separate `/_p/` for proxy, `/plugins/` for browser.
- **Silent empty UI**: Sidebar `.catch(() => {})` swallowed errors; iframe showed blank on upstream failure. Fix: loading/error states with retry.

## Next Steps

- Implement Steps 12-16 from `plan.md`
- Step 12 (routing fix) is critical — do first
- Step 13 (error states) prevents "UI is empty" confusion
- Steps 14-16 (Playwright) ensure UI works end-to-end in CI
