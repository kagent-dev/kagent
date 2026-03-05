# Implementation Plan: Dynamic MCP UI Routing for Plugins

## Checklist

- [ ] Step 1: CRD extension — add PluginUISpec to RemoteMCPServer
- [ ] Step 2: Database model and client — Plugin table
- [ ] Step 3: Controller — reconcile UI metadata into Plugin records
- [ ] Step 4: HTTP handler — /api/plugins discovery endpoint
- [ ] Step 5: HTTP handler — /plugins/{name}/ reverse proxy
- [ ] Step 6: Nginx — add /plugins/ location block, remove /kanban-mcp/
- [ ] Step 7: Next.js — plugin iframe page with postMessage bridge
- [ ] Step 8: Sidebar — dynamic plugin navigation
- [ ] Step 9: Plugin bridge SDK snippet
- [ ] Step 10: Kanban migration — CRD, Helm, remove hardcoded routes
- [ ] Step 11: E2E test

---

## Step 1: CRD Extension — PluginUISpec

**Objective:** Add optional `ui` field to RemoteMCPServerSpec so MCP servers can declare UI metadata.

**Implementation guidance:**
- Edit `go/api/v1alpha2/remotemcpserver_types.go`
- Add `PluginUISpec` struct with fields: `Enabled`, `PathPrefix`, `DisplayName`, `Icon`, `Section`
- Add kubebuilder validation markers (pattern for pathPrefix, enum for section)
- Add `UI *PluginUISpec` field to `RemoteMCPServerSpec` with `+optional` and `json:"ui,omitempty"`
- Run `make -C go generate` to regenerate CRD manifests and deepcopy

**Test requirements:**
- Verify CRD YAML includes `ui` field with validation schema
- Verify `omitempty` — existing CRDs without `ui` field still work
- Unit test: `PluginUISpec` serialization/deserialization round-trip

**Integration notes:**
- No breaking changes — `ui` is optional, defaults to nil
- Existing RemoteMCPServer CRDs are unaffected

**Demo:** `kubectl apply` a RemoteMCPServer with `ui` section, `kubectl get rmcps -o yaml` shows it back.

---

## Step 2: Database Model and Client — Plugin Table

**Objective:** Add `Plugin` model and database methods so the controller can persist UI metadata.

**Implementation guidance:**
- Add `Plugin` struct to `go/api/database/models.go` with fields: `Name` (PK), `PathPrefix` (unique index), `DisplayName`, `Icon`, `Section`, `UpstreamURL`, timestamps, soft delete
- Add `TableName()` returning `"plugin"`
- Add methods to `Client` interface in `go/api/database/client.go`: `StorePlugin`, `DeletePlugin`, `GetPluginByPathPrefix`, `ListPlugins`
- Implement methods in `go/core/internal/database/` (the concrete client implementation)
- `StorePlugin` uses GORM `Clauses(clause.OnConflict{...})` for upsert on Name PK
- Add `Plugin` to AutoMigrate call

**Test requirements:**
- Unit test: CRUD operations on Plugin model (create, read by pathPrefix, list, delete)
- Unit test: upsert behavior — second StorePlugin updates existing record
- Unit test: unique index on pathPrefix rejects duplicates with different names

**Integration notes:**
- AutoMigrate handles schema creation for both SQLite and Postgres

**Demo:** Run controller, verify `plugin` table exists in database with correct schema.

---

## Step 3: Controller — Reconcile Plugin UI Metadata

**Objective:** When reconciling a RemoteMCPServer, persist or delete Plugin records based on `spec.ui`.

**Implementation guidance:**
- Edit `go/core/internal/controller/reconciler/reconciler.go`
- Add `reconcilePluginUI(ctx, server)` method
- Add `deriveBaseURL(rawURL string) (string, error)` helper using `net/url`
- Call `reconcilePluginUI` in `ReconcileKagentRemoteMCPServer` after existing tool server upsert
- On UI enabled: derive defaults (pathPrefix defaults to CR name, icon defaults to "puzzle", section defaults to "PLUGINS"), derive upstream URL from `spec.url`, upsert Plugin record
- On UI disabled or nil: delete Plugin record (ignore not-found)
- On CR deletion: add `DeletePlugin(serverRef)` alongside existing tool/toolserver deletion
- Plugin UI reconciliation failure is non-fatal — log error, do not block tool discovery

**Test requirements:**
- Unit test: `deriveBaseURL` with various URL formats (`http://host:port/mcp`, `http://host:port`, `http://host/path/to/mcp`)
- Unit test: `reconcilePluginUI` creates Plugin when `ui.enabled=true`
- Unit test: `reconcilePluginUI` deletes Plugin when `ui` is nil
- Unit test: `reconcilePluginUI` updates Plugin when ui fields change
- Unit test: defaults applied (pathPrefix from name, icon "puzzle", section "PLUGINS")

**Integration notes:**
- Controller already reconciles every 60s — Plugin records stay in sync with CRDs
- Non-fatal error handling means existing functionality is never degraded

**Demo:** Apply RemoteMCPServer with `ui` section → verify Plugin row in DB. Remove `ui` section → verify Plugin row deleted.

---

## Step 4: HTTP Handler — /api/plugins Discovery Endpoint

**Objective:** Expose plugin list so the UI sidebar can discover available plugins.

**Implementation guidance:**
- Create `go/core/internal/httpserver/handlers/plugins.go`
- Implement `PluginsHandler` struct embedding `*Base`
- Implement `HandleListPlugins(w, r)` — queries `ListPlugins()` from DB, maps to `PluginResponse` DTOs, returns via `api.NewResponse`
- Add `PluginResponse` struct to `go/api/httpapi/types.go` with fields: `Name`, `PathPrefix`, `DisplayName`, `Icon`, `Section`
- Register handler in `go/core/internal/httpserver/server.go`: add `APIPathPlugins = "/api/plugins"` constant, register GET route
- Add `Plugins *PluginsHandler` to the handlers struct, initialize in handler factory

**Test requirements:**
- Unit test: `HandleListPlugins` returns correct JSON shape from mocked DB
- Unit test: empty plugin list returns `[]` not null

**Integration notes:**
- Uses same auth middleware as other `/api/` routes
- Response format follows existing `StandardResponse[T]` pattern

**Demo:** `curl /api/plugins` returns JSON array of plugin metadata.

---

## Step 5: HTTP Handler — /plugins/{name}/ Reverse Proxy

**Objective:** Go backend reverse-proxies requests to the plugin's upstream service based on database lookup.

**Implementation guidance:**
- Create `go/core/internal/httpserver/handlers/pluginproxy.go`
- Implement `PluginProxyHandler` with `sync.Map` cache of `pathPrefix → *httputil.ReverseProxy`
- `HandleProxy(w, r)` extracts `{name}` from gorilla/mux vars, looks up Plugin by pathPrefix, creates/caches reverse proxy, strips `/plugins/{name}` prefix, forwards request
- Set `FlushInterval: -1` on ReverseProxy for SSE support (no buffering)
- Set `X-Forwarded-Host` and `X-Plugin-Name` headers on proxied requests
- Register as PathPrefix route in server.go: `s.router.PathPrefix("/plugins/{name}").HandlerFunc(...)` — must be registered after more specific routes
- Note: this handler uses raw `http.HandlerFunc` (not `adaptHandler`) since it proxies directly

**Test requirements:**
- Unit test: prefix stripping — `/plugins/kanban/api/board` → `/api/board`
- Unit test: 404 when pathPrefix not found in DB
- Unit test: proxy cache hit on second request for same plugin
- Integration test: round-trip through proxy to a test HTTP server, verify headers and body

**Integration notes:**
- `sync.Map` cache is sufficient — plugin count expected to be small (<100)
- Cache entries should be invalidated if plugin upstream URL changes (handled by reconciler)
- SSE streaming works because FlushInterval=-1 disables response buffering

**Demo:** With kanban-mcp running, `curl /plugins/kanban/api/board` returns the board JSON.

---

## Step 6: Nginx — Add /plugins/ Location, Remove /kanban-mcp/

**Objective:** Route `/plugins/` to Go backend, remove hardcoded kanban route.

**Implementation guidance:**
- Edit `ui/conf/nginx.conf`
- Remove the `location /kanban-mcp/` block (lines 64-79)
- Add new `location /plugins/` block before the catch-all `location /`:
  ```nginx
  location /plugins/ {
      proxy_pass http://kagent_backend/plugins/;
      proxy_http_version 1.1;
      proxy_set_header Upgrade $http_upgrade;
      proxy_set_header Connection $connection_upgrade;
      proxy_set_header Host $host;
      proxy_set_header X-Forwarded-Host $host;
      proxy_set_header X-Forwarded-Proto $scheme;
      proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
      proxy_cache_bypass $http_upgrade;
      proxy_read_timeout 300s;
      proxy_send_timeout 300s;
      proxy_buffering off;
  }
  ```
- Uses existing `kagent_backend` upstream (Go server on 127.0.0.1:8083)

**Test requirements:**
- Verify nginx config is valid: `nginx -t`
- Integration test: request to `/plugins/kanban/` reaches Go backend

**Integration notes:**
- One-time change — no further nginx edits needed for new plugins
- WebSocket upgrade headers included for future plugin needs
- `proxy_buffering off` ensures SSE works through the full chain (nginx → Go → plugin)

**Demo:** After nginx reload, `/plugins/kanban/` resolves through nginx → Go → kanban-mcp.

---

## Step 7: Next.js — Plugin iframe Page with postMessage Bridge

**Objective:** Create a catch-all route that renders plugin UI in a sandboxed iframe with bidirectional postMessage communication.

**Implementation guidance:**
- Create `ui/src/app/plugins/[name]/[[...path]]/page.tsx`
- Render full-height iframe with `src=/plugins/{name}/{path}`
- iframe sandbox: `allow-scripts allow-same-origin allow-forms allow-popups`
- On mount and on theme/namespace changes: send `kagent:context` message to iframe
- Listen for `kagent:ready`, `kagent:navigate`, `kagent:resize`, `kagent:badge`, `kagent:title` from iframe
- `kagent:badge` dispatches a `CustomEvent` on `window` for sidebar to pick up
- `kagent:navigate` uses `router.push()` for client-side navigation
- `kagent:resize` sets iframe height (for non-full-height plugins)
- `kagent:title` updates an optional header bar above the iframe

**Test requirements:**
- Unit test: postMessage handler dispatches correct events for each message type
- Unit test: iframe src constructed correctly from route params
- Visual test: iframe fills available space below optional title bar

**Integration notes:**
- Uses existing `useTheme()` from next-themes and `useNamespace()` from namespace context
- `allow-same-origin` in sandbox is needed because iframe loads from same origin (`/plugins/` via nginx)

**Demo:** Navigate to `/plugins/kanban` in browser → see kanban board inside iframe with sidebar visible.

---

## Step 8: Sidebar — Dynamic Plugin Navigation

**Objective:** Sidebar auto-discovers plugins from `/api/plugins` and renders nav items in configured sections with badge support.

**Implementation guidance:**
- Edit `ui/src/components/sidebars/AppSidebarNav.tsx`
- Add `useEffect` to fetch `/api/plugins` on mount, store in state
- Add `useEffect` to listen for `kagent:plugin-badge` custom events, update badge state
- Create `getIconByName(name: string): LucideIcon` helper — converts kebab-case icon name to PascalCase lucide-react component, falls back to `Puzzle`
- Merge plugin items into matching sections (by `section` field)
- If any plugins have `section="PLUGINS"`, render a new PLUGINS group
- Remove hardcoded Kanban entry from static `NAV_SECTIONS`
- Use `SidebarMenuBadge` for badge display
- Active state: match `pathname === href || pathname.startsWith(href + "/")`

**Test requirements:**
- Unit test: `getIconByName` maps "kanban" → Kanban, "git-fork" → GitFork, unknown → Puzzle
- Unit test: plugins merged into correct sections
- Unit test: PLUGINS section only rendered when plugins exist for it
- Unit test: badge renders when count is present

**Integration notes:**
- Fetch on mount is sufficient — plugin list changes rarely (only on CRD apply/delete)
- Badge state is ephemeral (per-session) — resets on page reload

**Demo:** Deploy kanban-mcp with `ui.section=AGENTS` → sidebar shows "Kanban Board" under AGENTS with kanban icon.

---

## Step 9: Plugin Bridge SDK Snippet

**Objective:** Provide a small JS snippet that plugin developers include in their UIs to communicate with the kagent host.

**Implementation guidance:**
- Create `go/plugins/kagent-plugin-bridge.js` (or embed in plugin documentation)
- Lightweight object with methods: `connect()`, `onContext(fn)`, `navigate(href)`, `setBadge(count, label)`, `setTitle(title)`, `reportHeight(height)`
- `connect()` posts `kagent:ready` and starts listening for `kagent:context`
- No build step, no dependencies — vanilla JS, copy-pasteable
- Document the protocol in a brief README section

**Test requirements:**
- Unit test (in-browser or jsdom): `connect()` sends ready message, `onContext` callback fires on context message
- Manual test: kanban-mcp includes bridge and receives theme updates

**Integration notes:**
- This is a convenience — plugins can implement the protocol directly
- Future: publish as npm package or Go embed for Go plugins

**Demo:** Plugin UI calls `kagent.connect()`, receives theme, applies dark mode.

---

## Step 10: Kanban Migration

**Objective:** Migrate kanban from hardcoded integration to the new plugin system, proving the pattern end-to-end.

**Implementation guidance:**
- Edit `helm/tools/kanban-mcp/templates/remotemcpserver.yaml` — add `ui` section with `enabled: true`, `pathPrefix: "kanban"`, `displayName: "Kanban Board"`, `icon: "kanban"`, `section: "AGENTS"`
- Delete `ui/src/app/kanban/page.tsx`
- Remove static Kanban nav item from `AppSidebarNav.tsx` (already done in Step 8)
- Nginx `/kanban-mcp/` removal already done in Step 6
- Edit kanban-mcp embedded UI (`go/plugins/kanban-mcp/internal/ui/index.html`):
  - Include the plugin bridge snippet
  - On load: call `kagent.connect()`
  - On context change: apply theme (light/dark class toggle)
  - On board update: call `kagent.setBadge(totalTasks)` to update sidebar badge
- Rebuild kanban-mcp binary (HTML is `//go:embed`)

**Test requirements:**
- E2E: deploy kanban-mcp chart → verify `/plugins/kanban/` serves the board
- E2E: verify old `/kanban-mcp/` path returns 404
- E2E: verify sidebar shows "Kanban Board" under AGENTS (dynamically, not static)
- E2E: verify SSE events stream through the full proxy chain
- E2E: verify theme changes propagate to kanban iframe

**Integration notes:**
- Kanban-mcp's internal routes (`/api/*`, `/events`, `/mcp`, `/`) remain unchanged
- The Go reverse proxy strips `/plugins/kanban` prefix, so plugin receives requests at root
- SSE works because both nginx and Go proxy have buffering disabled

**Demo:** Full end-to-end: deploy chart → open UI → see kanban in sidebar under AGENTS → click → board loads in iframe → add task → SSE updates live → toggle dark mode → kanban follows theme.

---

## Step 11: E2E Test

**Objective:** Automated test verifying the full plugin routing pipeline.

**Implementation guidance:**
- Add test in `go/core/test/e2e/`
- Test flow:
  1. Create RemoteMCPServer CRD with `ui` section
  2. Wait for controller to reconcile (poll `/api/plugins` until entry appears)
  3. Verify `/api/plugins` returns correct metadata
  4. Verify `/plugins/{name}/` returns 200 (proxied response)
  5. Delete CRD
  6. Verify `/api/plugins` no longer returns the entry
  7. Verify `/plugins/{name}/` returns 404

**Test requirements:**
- Uses existing E2E test framework and Kind cluster
- Requires a test MCP server (can use a simple httptest server or deploy kanban-mcp)

**Integration notes:**
- Follows existing E2E patterns in `go/core/test/e2e/`

**Demo:** `make -C go test-e2e` passes with new plugin routing tests.
