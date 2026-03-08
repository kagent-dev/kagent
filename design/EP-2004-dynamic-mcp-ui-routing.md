# EP-2004: Dynamic MCP UI Plugin Routing

* Status: **Implemented**
* Spec: [specs/dynamic-mcp-ui-routing](../specs/dynamic-mcp-ui-routing/)

## Background

Replaces hardcoded nginx proxy rules and static Next.js routes for MCP plugin UIs with a fully dynamic, CRD-driven system. Plugin UIs are discovered from RemoteMCPServer CRD metadata, stored in a Plugin database table, and served via Go reverse proxy.

## Motivation

Adding a new plugin UI previously required modifying nginx config, adding Next.js routes, and redeploying. The dynamic system allows plugins to self-register their UI via CRD annotations.

### Goals

- CRD declares UI metadata: `pathPrefix`, `displayName`, `icon`, `section`
- Controller reconciles UI metadata into `Plugin` DB table
- Go reverse proxy at `/_p/{name}/` routes dynamically based on DB lookup
- Next.js catch-all `/plugins/[name]/` renders iframe with postMessage bridge
- Sidebar auto-discovers plugins via `GET /api/plugins`
- Iframe bridge: theme sync, resize, navigation, auth token, badges

### Non-Goals

- Server-side rendering of plugin content
- Plugin marketplace or versioning

## Implementation Details

- **Proxy:** `go/core/internal/httpserver/handlers/pluginproxy.go` — `/_p/{name}/` reverse proxy
- **DB:** `Plugin` model in `go/api/database/models.go`
- **Reconciler:** `reconcilePluginUI()` in shared reconciler
- **UI:** `ui/src/app/plugins/[name]/[[...path]]/page.tsx` — iframe host with postMessage bridge
- **Discovery:** `GET /api/plugins` returns registered plugins for sidebar

### Test Plan

- Cypress browser E2E tests with 8 scenarios
- Plugin loading, error, and retry state testing
