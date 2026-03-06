# Implementation Plan: Dynamic MCP UI Routing for Plugins

## Checklist

- [x] Step 1: CRD extension — add PluginUISpec to RemoteMCPServer
- [x] Step 2: Database model and client — Plugin table
- [x] Step 3: Controller — reconcile UI metadata into Plugin records
- [x] Step 4: HTTP handler — /api/plugins discovery endpoint
- [x] Step 5: HTTP handler — /plugins/{name}/ reverse proxy
- [x] Step 6: Nginx — add /plugins/ location block, remove /kanban-mcp/
- [x] Step 7: Next.js — plugin iframe page with postMessage bridge
- [x] Step 8: Sidebar — dynamic plugin navigation
- [x] Step 9: Plugin bridge SDK snippet
- [x] Step 10: Kanban migration — CRD, Helm, remove hardcoded routes
- [x] Step 11: E2E test (API-only)
- [x] Step 12: **FIX** — Rename proxy path from `/plugins/` to `/_p/` (routing conflict)
- [x] Step 13: **FIX** — Add loading/error states to sidebar and plugin page
- [x] Step 14: Mock plugin service for browser E2E tests
- [x] Step 15: Cypress browser E2E tests (adapted from Playwright to match existing test infra)
- [x] Step 16: CI integration — API verification script + Cypress

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

---

## Step 12: FIX — Rename Proxy Path from `/plugins/` to `/_p/`

**Objective:** Fix the nginx routing conflict where browser URL `/plugins/{name}` and iframe proxy URL collide, causing hard refresh to bypass Next.js layout.

**Root cause:** `location /plugins/` in nginx catches ALL `/plugins/*` requests — including browser navigation. On hard refresh or direct URL, nginx sends to Go backend instead of Next.js. User sees raw plugin HTML without sidebar.

**Implementation guidance:**

1. **Go server** — `go/core/internal/httpserver/server.go`:
   - Change `PluginsProxyPath` constant from `"/plugins/{name}"` to `"/_p/{name}"`
   - Update `PathPrefix` registration: `s.router.PathPrefix("/_p/{name}").HandlerFunc(...)`

2. **Go proxy handler** — `go/core/internal/httpserver/handlers/pluginproxy.go`:
   - Update `HandleProxy` prefix stripping: `prefix := "/_p/" + pathPrefix`
   - Update comments referencing `/plugins/`

3. **Nginx** — `ui/conf/nginx.conf`:
   - Rename `location /plugins/` to `location /_p/`
   - Change `proxy_pass` to `http://kagent_backend/_p/`
   - Browser URL `/plugins/{name}` now falls through to `location /` → Next.js

4. **Next.js plugin page** — `ui/src/app/plugins/[name]/[[...path]]/page.tsx`:
   - Change iframe src from `/plugins/${name}${subPath}` to `/_p/${name}${subPath}`

5. **Update unit tests** — `go/core/internal/httpserver/handlers/pluginproxy_test.go`:
   - Update all URL paths in tests from `/plugins/` to `/_p/`

6. **Update E2E test** — `go/core/test/e2e/plugin_routing_test.go`:
   - Update proxy URL from `/plugins/test-plugin/` to `/_p/test-plugin/`

7. **Update API verification script** — `scripts/check-plugins-api.sh`:
   - If checking proxy endpoint, use `/_p/` path

**Test requirements:**
- All existing unit tests pass with updated paths
- E2E test verifies `/_p/{name}/` proxy works
- Manual test: hard refresh on `/plugins/kanban` preserves sidebar layout
- Nginx config validates: `nginx -t`

**Integration notes:**
- This is a non-breaking change for the API (`/api/plugins` is unchanged)
- The `pathPrefix` field in Plugin model and CRD is unchanged (it's the logical name, not the URL path)
- `/_p/` is an internal-only path — not exposed to users or documented externally

**Demo:** Hard refresh on `/plugins/kanban` → sidebar stays visible, iframe loads from `/_p/kanban/`.

---

## Step 13: FIX — Add Loading/Error States to Sidebar and Plugin Page

**Objective:** Eliminate silent empty UI by adding loading indicators, error states, and retry behavior.

**Implementation guidance:**

1. **Sidebar plugin fetch** — `ui/src/components/sidebars/AppSidebarNav.tsx`:
   - Add `loading` and `error` state variables
   - Set `loading=true` before fetch, `false` after
   - Replace `.catch(() => {})` with `.catch((err) => { setError(true); console.error(err); })`
   - Render loading indicator (spinner or skeleton) while `loading=true`
   - Render error indicator with "Retry" button when `error=true`
   - Retry button calls `fetchPlugins()` again

2. **Plugin page loading** — `ui/src/app/plugins/[name]/[[...path]]/page.tsx`:
   - Add `loading` state (default `true`)
   - Add `error` state (default `false`)
   - iframe `onLoad` → `setLoading(false)`
   - iframe `onError` → `setLoading(false); setError(true)`
   - Render loading skeleton (Loader2 spinner + "Loading plugin...") while `loading=true`
   - Render error fallback (AlertCircle icon + "Plugin unavailable" + Retry button) when `error=true`
   - Hide iframe with `className="hidden"` while loading or error
   - Retry button resets state and reloads iframe (toggle `key` prop or re-set src)

3. **Update sidebar tests** — `ui/src/components/sidebars/__tests__/AppSidebarNav.test.tsx`:
   - Test: loading state visible during pending fetch
   - Test: error state visible on fetch rejection
   - Test: retry re-fetches `/api/plugins`

**Test requirements:**
- Unit test: sidebar shows loading indicator while fetch is pending
- Unit test: sidebar shows error indicator when `/api/plugins` returns 500
- Unit test: clicking retry re-fetches and clears error state
- Unit test: plugin page shows loading skeleton, then content after iframe loads
- Unit test: plugin page shows error fallback on iframe error

**Integration notes:**
- Uses existing Shadcn/UI components (Loader2 icon from lucide-react)
- No new dependencies needed

**Demo:** Stop the Go backend → sidebar shows "Failed to load plugins (Retry)" instead of empty. Start backend → click Retry → plugins appear.

---

## Step 14: Mock Plugin Service for Browser E2E Tests

**Objective:** Create a minimal HTTP server that serves as a mock plugin for Playwright browser tests, including `kagent-plugin-bridge.js` integration.

**Implementation guidance:**

1. **Create mock plugin** — `ui/e2e/fixtures/mock-plugin-server.ts`:
   - Simple Express or Node HTTP server
   - Serves on configurable port (default 9999)
   - Endpoints:
     - `GET /` → returns test HTML with bridge integration (see below)
     - `GET /api/health` → returns 200
     - `GET /events` → SSE stream emitting test events

2. **Mock plugin HTML**:
   ```html
   <!DOCTYPE html>
   <html>
   <body>
     <div id="plugin-content">Mock Plugin Loaded</div>
     <div id="theme" data-testid="theme-value">unknown</div>
     <div id="namespace" data-testid="namespace-value">unknown</div>
     <script>
       // Inline bridge (no external file dependency for test simplicity)
       window.addEventListener("message", (event) => {
         if (event.data?.type === "kagent:context") {
           document.getElementById("theme").textContent = event.data.payload.theme;
           document.getElementById("namespace").textContent = event.data.payload.namespace;
         }
       });
       // Signal ready
       window.parent.postMessage({ type: "kagent:ready", payload: {} }, "*");
       // Send badge after load
       setTimeout(() => {
         window.parent.postMessage({ type: "kagent:badge", payload: { count: 3 } }, "*");
       }, 100);
     </script>
   </body>
   </html>
   ```

3. **Deploy mock plugin to Kind** — `ui/e2e/fixtures/mock-plugin.yaml`:
   - Deployment + Service for mock plugin
   - RemoteMCPServer CRD with `ui` section pointing to mock service

4. **Playwright fixture** — `ui/e2e/fixtures/plugin-fixture.ts`:
   - Before all: apply mock plugin K8s manifests, wait for plugin to appear in `/api/plugins`
   - After all: delete mock plugin manifests

**Test requirements:**
- Mock plugin responds within 100ms for test reliability
- Mock plugin HTML includes data-testid attributes for Playwright selectors
- Mock plugin emits `kagent:ready`, processes `kagent:context`, sends `kagent:badge`

**Integration notes:**
- Mock plugin deploys to same Kind cluster as kagent
- Uses existing `kubectl apply` patterns from E2E setup

**Demo:** `kubectl apply -f ui/e2e/fixtures/mock-plugin.yaml` → `/api/plugins` shows mock plugin → `/_p/mock-plugin/` returns test HTML.

---

## Step 15: Playwright Browser E2E Tests

**Objective:** Automated browser tests verifying the full UI pipeline — sidebar discovery, plugin navigation, iframe rendering, postMessage bridge, and error states.

**Implementation guidance:**

1. **Setup Playwright** — `ui/playwright.config.ts`:
   - Base URL from env `KAGENT_UI_URL` (default `http://localhost:8080`)
   - Browser: chromium
   - Timeout: 30s per test
   - Global setup: deploy mock plugin, wait for it in `/api/plugins`
   - Global teardown: remove mock plugin

2. **Test file** — `ui/e2e/plugin-routing.spec.ts`:

   ```typescript
   test.describe("Plugin UI Routing", () => {

     test("sidebar shows plugin nav item from /api/plugins", async ({ page }) => {
       await page.goto("/");
       // Wait for plugin to appear in sidebar (fetched from /api/plugins)
       const pluginLink = page.getByRole("link", { name: "Mock Plugin" });
       await expect(pluginLink).toBeVisible({ timeout: 10000 });
     });

     test("clicking plugin navigates to /plugins/{name} with sidebar", async ({ page }) => {
       await page.goto("/");
       await page.getByRole("link", { name: "Mock Plugin" }).click();
       await expect(page).toHaveURL(/\/plugins\/mock-plugin/);
       // Sidebar still visible
       await expect(page.getByRole("link", { name: "Dashboard" })).toBeVisible();
       // iframe present with correct src
       const iframe = page.frameLocator('iframe[title="Plugin: mock-plugin"]');
       await expect(iframe.locator("#plugin-content")).toHaveText("Mock Plugin Loaded");
     });

     test("hard refresh preserves sidebar and iframe", async ({ page }) => {
       await page.goto("/plugins/mock-plugin");
       // Sidebar visible on direct navigation
       await expect(page.getByRole("link", { name: "Dashboard" })).toBeVisible();
       // iframe loads
       const iframe = page.frameLocator('iframe[title="Plugin: mock-plugin"]');
       await expect(iframe.locator("#plugin-content")).toHaveText("Mock Plugin Loaded");
     });

     test("theme sync via postMessage", async ({ page }) => {
       await page.goto("/plugins/mock-plugin");
       const iframe = page.frameLocator('iframe[title="Plugin: mock-plugin"]');
       // Wait for plugin to receive initial context
       await expect(iframe.locator('[data-testid="theme-value"]')).not.toHaveText("unknown", { timeout: 5000 });
       // Theme should be light or dark (from host)
       const themeText = await iframe.locator('[data-testid="theme-value"]').textContent();
       expect(["light", "dark"]).toContain(themeText);
     });

     test("badge update appears in sidebar", async ({ page }) => {
       await page.goto("/plugins/mock-plugin");
       // Mock plugin sends badge count=3 after 100ms
       const badge = page.getByTestId("sidebar-menu-badge");
       await expect(badge).toHaveText("3", { timeout: 5000 });
     });

     test("loading state shown while iframe loads", async ({ page }) => {
       await page.goto("/plugins/mock-plugin");
       // Loading indicator should be visible briefly
       // (may be too fast to catch; test validates no error state)
       const iframe = page.frameLocator('iframe[title="Plugin: mock-plugin"]');
       await expect(iframe.locator("#plugin-content")).toHaveText("Mock Plugin Loaded");
       // No error message visible
       await expect(page.getByText("Plugin unavailable")).not.toBeVisible();
     });

     test("error state shown when plugin unreachable", async ({ page }) => {
       // Navigate to a non-existent plugin
       await page.goto("/plugins/nonexistent-plugin-xyz");
       // Should show error fallback (upstream returns 404 or iframe fails)
       await expect(page.getByText("Plugin unavailable")).toBeVisible({ timeout: 10000 });
       // Retry button present
       await expect(page.getByRole("button", { name: /retry/i })).toBeVisible();
     });
   });
   ```

3. **Add npm scripts** — `ui/package.json`:
   ```json
   "test:e2e": "playwright test",
   "test:e2e:ui": "playwright test --ui"
   ```

**Test requirements:**
- All 7 tests pass against Kind cluster with mock plugin deployed
- Tests are idempotent (can run multiple times without side effects)
- Test timeout: 30s per test (allows for K8s reconciliation latency)
- Tests use Playwright built-in assertions (auto-retry + timeout)

**Integration notes:**
- Playwright runs against the nginx-fronted kagent UI (port 8080)
- Tests use `page.frameLocator()` for iframe content assertions
- Mock plugin must be deployed before tests run (handled by global setup)

**Demo:** `cd ui && npx playwright test` → 7 tests pass, HTML report generated.

---

## Step 16: CI Integration — API Verification + Playwright

**Objective:** Integrate API verification script and Playwright browser tests into CI pipeline.

**Implementation guidance:**

1. **Enhance `scripts/check-plugins-api.sh`**:
   - Add `--wait` flag: poll `/api/plugins` every 2s up to 60s until expected plugin appears
   - Add `--proxy` flag: verify `/_p/{name}/` returns non-404
   - Add `--all` flag: run both checks
   - Use in CI after `helm install` to wait for plugin to be ready before running browser tests

2. **Add Makefile targets**:
   ```makefile
   # Run API verification
   test-plugins-api:
   	scripts/check-plugins-api.sh --wait --proxy

   # Run Playwright browser E2E tests
   test-e2e-browser:
   	cd ui && npx playwright install --with-deps chromium
   	cd ui && npx playwright test

   # Run all E2E tests (API + browser)
   test-e2e-all: test-e2e test-plugins-api test-e2e-browser
   ```

3. **GitHub Actions workflow** — `.github/workflows/e2e-browser.yml` (or extend existing):
   - Trigger: PR, push to main
   - Steps:
     1. Create Kind cluster
     2. Build and deploy kagent
     3. Deploy mock plugin (`kubectl apply -f ui/e2e/fixtures/mock-plugin.yaml`)
     4. Wait for plugin ready (`scripts/check-plugins-api.sh --wait --plugin mock-plugin`)
     5. Run Playwright tests (`make test-e2e-browser`)
     6. Upload Playwright report as artifact

**Test requirements:**
- CI completes within 10 minutes (including cluster creation)
- Playwright report uploaded as artifact on failure
- API verification script exits non-zero on any failure

**Integration notes:**
- Kind cluster reused from existing E2E setup
- Playwright installed via `npx playwright install --with-deps` (includes browser binaries)
- Mock plugin cleanup handled by CI teardown (Kind cluster deleted)

**Demo:** PR triggers CI → Kind cluster created → kagent deployed → mock plugin deployed → API verified → Playwright tests pass → green check on PR.
