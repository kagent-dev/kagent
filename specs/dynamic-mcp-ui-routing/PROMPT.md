# PROMPT: Dynamic MCP UI Routing for Plugins

## Objective

Implement dynamic UI routing for MCP plugins in kagent. MCP tool servers declare UI metadata in their RemoteMCPServer CRD. The Go backend discovers these declarations, persists them, and reverse-proxies plugin UIs at `/_p/{name}/`. The Next.js UI renders plugins in sandboxed iframes at `/plugins/{name}` with a postMessage bridge. Migrate the existing kanban integration to this system.

## Architecture

```
Browser URL: /plugins/{name}  →  nginx location /  →  Next.js (sidebar + iframe shell)
Iframe src:  /_p/{name}/      →  nginx location /_p/  →  Go backend  →  upstream plugin service
API:         /api/plugins     →  nginx location /api/  →  Go backend  →  database
```

Key: browser URLs and internal proxy URLs use separate paths to avoid nginx routing conflicts.

## Implementation Steps

### Backend (Go)

1. **CRD** — Add `PluginUISpec` to `go/api/v1alpha2/remotemcpserver_types.go`:
   - Fields: `Enabled` bool, `PathPrefix` string, `DisplayName` string, `Icon` string, `Section` enum
   - Optional `UI *PluginUISpec` on `RemoteMCPServerSpec`
   - Run `make -C go generate`

2. **Database** — Add `Plugin` model to `go/api/database/models.go`:
   - PK: `Name` (namespace/name), unique index: `PathPrefix`
   - Fields: `DisplayName`, `Icon`, `Section`, `UpstreamURL`
   - Interface methods: `StorePlugin`, `DeletePlugin`, `GetPluginByPathPrefix`, `ListPlugins`

3. **Controller** — Extend `go/core/internal/controller/reconciler/reconciler.go`:
   - `reconcilePluginUI(server)` — upsert/delete Plugin records from CRD `spec.ui`
   - `deriveBaseURL(url)` — strip path from `spec.url` to get upstream base
   - Non-fatal: plugin UI failure must not block tool discovery

4. **API handler** — `go/core/internal/httpserver/handlers/plugins.go`:
   - `GET /api/plugins` — returns `[]PluginResponse{name, pathPrefix, displayName, icon, section}`

5. **Proxy handler** — `go/core/internal/httpserver/handlers/pluginproxy.go`:
   - `/_p/{name}/{path...}` — DB lookup by pathPrefix, reverse proxy to upstream
   - Strip `/_p/{name}` prefix before forwarding
   - `sync.Map` cache for proxy instances, `FlushInterval: -1` for SSE

6. **Routes** — `go/core/internal/httpserver/server.go`:
   - `GET /api/plugins` → `PluginsHandler.HandleListPlugins`
   - `PathPrefix("/_p/{name}")` → `PluginProxyHandler.HandleProxy`

### Nginx

7. **`ui/conf/nginx.conf`**:
   - Add `location /_p/` → `proxy_pass http://kagent_backend/_p/;` (buffering off, WebSocket headers)
   - Remove any hardcoded `/kanban-mcp/` block
   - Do NOT add `location /plugins/` — browser URLs must reach Next.js via `location /`

### Frontend (Next.js)

8. **Plugin page** — `ui/src/app/plugins/[name]/[[...path]]/page.tsx`:
   - iframe with `src=/_p/${name}/${subPath}` (NOT `/plugins/`)
   - `sandbox="allow-scripts allow-same-origin allow-forms allow-popups"`
   - postMessage bridge: handle `kagent:ready`, `kagent:navigate`, `kagent:resize`, `kagent:badge`, `kagent:title`
   - Send `kagent:context` (theme, namespace, authToken) on load and on changes
   - Loading skeleton while iframe loads (`onLoad` handler)
   - "Plugin unavailable" fallback with retry on `onError`

9. **Sidebar** — `ui/src/components/sidebars/AppSidebarNav.tsx`:
   - Fetch `/api/plugins` on mount, merge into nav sections by `section` field
   - Loading indicator while fetch in-flight
   - Error indicator with retry button on fetch failure (NOT silent `.catch(() => {})`)
   - Badge support via `kagent:plugin-badge` custom event listener
   - `getIconByName(kebab-case)` → lucide-react component, fallback to Puzzle

10. **Plugin bridge SDK** — `go/plugins/kagent-plugin-bridge.js`:
    - `connect()`, `onContext(fn)`, `navigate(href)`, `setBadge(count, label)`, `setTitle(title)`, `reportHeight(height)`

### Migration

11. **Kanban** — Add `ui` section to kanban-mcp Helm RemoteMCPServer template:
    - `enabled: true, pathPrefix: "kanban", displayName: "Kanban Board", icon: "kanban", section: "AGENTS"`
    - Delete `ui/src/app/kanban/page.tsx`, remove static sidebar entry
    - Integrate `kagent-plugin-bridge.js` in kanban-mcp embedded UI

### Testing

12. **Go unit tests**:
    - `deriveBaseURL()` with various URL formats
    - `PluginsHandler` returns correct JSON shape
    - `PluginProxyHandler` strips `/_p/` prefix, 404 on unknown, proxy cache reuse
    - Controller `reconcilePluginUI` create/update/delete/defaults

13. **Go E2E test** — `go/core/test/e2e/plugin_routing_test.go`:
    - Create RemoteMCPServer with `ui` → poll `/api/plugins` → verify metadata
    - Verify `/_p/{name}/` returns proxied response (non-404)
    - Delete CRD → verify removed from `/api/plugins` and `/_p/` returns 404

14. **Frontend unit tests** — `ui/src/components/sidebars/__tests__/AppSidebarNav.test.tsx`:
    - Plugin items merged into correct sections
    - Badge renders on `kagent:plugin-badge` event
    - Loading state during fetch, error state on failure, retry re-fetches

15. **Mock plugin service** — `ui/e2e/fixtures/`:
    - K8s Deployment + Service + RemoteMCPServer CRD with `ui` section
    - HTML with inline bridge: receives `kagent:context`, sends `kagent:badge {count: 3}`
    - `data-testid` attributes for Playwright selectors

16. **Playwright browser E2E** — `ui/e2e/plugin-routing.spec.ts`:
    1. Sidebar shows plugin nav item from `/api/plugins`
    2. Click navigates to `/plugins/{name}` with sidebar + iframe
    3. Hard refresh on `/plugins/{name}` preserves sidebar layout
    4. Theme sync via postMessage (iframe receives `kagent:context`)
    5. Badge update appears in sidebar
    6. Loading state shown (no error on success)
    7. Error state + retry button for unreachable plugin

17. **CI integration**:
    - `scripts/check-plugins-api.sh` — add `--wait` polling mode, `--proxy` check for `/_p/`
    - Makefile: `test-e2e-browser` (Playwright), `test-e2e-all` (Go E2E + API check + Playwright)

## Acceptance Criteria

```gherkin
# Routing
Given nginx has location /_p/ (Go backend) and location / (Next.js)
When a user navigates to /plugins/kanban (browser URL)
Then Next.js renders sidebar + iframe with src=/_p/kanban/
And hard refresh preserves the same layout

# API Pipeline
Given a RemoteMCPServer CRD with ui.enabled=true and ui.pathPrefix="kanban"
When the controller reconciles
Then GET /api/plugins returns the plugin metadata
And GET /_p/kanban/ reverse-proxies to the plugin service

# UI
Given /api/plugins returns plugin metadata
Then the sidebar shows the plugin in the correct section with icon and badge
And the plugin page renders iframe content with postMessage bridge

# Error Handling
Given /api/plugins fails or upstream is unreachable
Then the UI shows loading/error states with retry (not silent empty)

# Testing
Given Playwright tests run against Kind cluster with mock plugin
Then all 7 browser E2E scenarios pass
```

## Reference

- Design: `specs/dynamic-mcp-ui-routing/design.md`
- Plan (17 steps): `specs/dynamic-mcp-ui-routing/plan.md`
- Requirements (Q1-Q13): `specs/dynamic-mcp-ui-routing/requirements.md`
