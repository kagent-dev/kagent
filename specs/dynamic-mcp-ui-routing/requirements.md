# Requirements — Dynamic MCP UI Routing for Plugins

This file documents the Q&A record from requirements clarification.

---

## Q1: Proxy approach — nginx dynamic map vs. Go reverse proxy?

Currently nginx has hardcoded `location /kanban-mcp/` blocks. Two options for dynamic routing:

**Option A: Nginx with generated config** — A sidecar or init container generates nginx location blocks from K8s API (RemoteMCPServer CRDs with UI metadata), then reloads nginx. Simple but requires config regeneration + reload on CRD changes.

**Option B: Go reverse proxy** — Add a `/plugins/{name}/` handler in the Go backend HTTP server that looks up the plugin's service URL from the database (populated by controller from CRD) and reverse-proxies requests. No nginx changes needed. More dynamic but adds load to Go server.

**User answer:** Option B — Go reverse proxy. The Go backend handles `/plugins/{name}/` routing dynamically, looking up plugin service URLs from the database. No nginx config changes needed for new plugins.

---

## Q2: CRD extension — where should UI metadata live?

To know which plugins have UIs and how to route to them, we need metadata. Options:

**Option A: Extend RemoteMCPServer CRD** — Add an optional `ui` section to RemoteMCPServerSpec:
```yaml
spec:
  ui:
    enabled: true
    pathPrefix: "kanban"        # → /plugins/kanban/
    displayName: "Kanban Board"
    icon: "kanban"              # lucide icon name
    section: "AGENTS"           # sidebar section
```

**Option B: Annotations on RemoteMCPServer** — Use well-known annotations like `kagent.dev/ui-enabled: "true"`, `kagent.dev/ui-path: "kanban"`, etc. No CRD schema change needed but less discoverable and no validation.

**Option C: Separate CRD** — A new `PluginUI` CRD that references a RemoteMCPServer. More complex, overkill for alpha.

**User answer:** Option A — Extend RemoteMCPServer CRD with an optional `ui` section. Validated by kubebuilder, discoverable, and keeps plugin metadata co-located with the MCP server definition.

---

## Q3: Plugin UI service URL — same service or separate?

Currently kanban-mcp serves both MCP tools (at `/mcp`) and its UI/REST API (at `/`, `/api/*`) on the same port/service. The RemoteMCPServer CRD's `spec.url` points to the MCP endpoint.

For the Go reverse proxy, it needs to know the HTTP base URL to forward UI requests to. Options:

**Option A: Derive from existing `spec.url`** — Strip the MCP path from the existing URL (e.g., `http://kanban-mcp.kagent.svc:8080/mcp` → `http://kanban-mcp.kagent.svc:8080`). Simple but assumes MCP and UI are on the same host:port.

**Option B: Explicit `ui.url` field** — The `ui` section includes its own URL, allowing the UI to be served from a different service/port than the MCP endpoint. More flexible.

**User answer:** Option A — derive from `spec.url`. Also consider the MCP Apps extension spec (`ui://` resources at https://apps.extensions.modelcontextprotocol.io/api/). MCP Apps defines a pattern where tools declare `_meta.ui.resourceUri` for inline iframe UIs in chat clients. kagent's full-page plugin UIs are a different use case (persistent dashboards vs. per-invocation inline UIs), but the CRD metadata design should not conflict with future MCP Apps support.

---

## Q4: Sidebar integration — auto-discover or explicit section assignment?

When a RemoteMCPServer has `ui.enabled: true`, it should appear in the sidebar. Options:

**Option A: Dedicated PLUGINS section** — All plugin UIs appear in a new "PLUGINS" sidebar section, auto-populated from CRDs with UI metadata. Simple, clear separation.

**Option B: Configurable section** — The CRD `ui.section` field lets each plugin specify which sidebar section it belongs to (OVERVIEW, AGENTS, RESOURCES, ADMIN, or PLUGINS). More flexible but plugins could clutter core sections.

**Option C: Always PLUGINS section + pinning** — Default to PLUGINS section, but users can drag/pin items to other sections via UI (stored client-side). Best of both but more UI work.

**User answer:** Option B — Configurable section via CRD `ui.section` field. Each plugin declares which sidebar section it belongs to. Default to "PLUGINS" if not specified.

---

## Q5: Plugin UI rendering — iframe isolation or direct proxy?

When the user navigates to `/plugins/kanban/`, how is the plugin UI rendered within the kagent shell (sidebar stays visible)?

**Option A: iframe** — The Next.js page at `/plugins/[name]/` renders an `<iframe src="/api/plugins/{name}/">` that loads the plugin UI from the Go reverse proxy. Full isolation (CSS/JS sandboxed), but cross-origin communication is harder, and iframe quirks (scroll, resize, SSE).

**Option B: Direct proxy, full page** — The Go reverse proxy serves the plugin HTML directly. The Next.js app has a catch-all `/plugins/[name]/[[...path]]/page.tsx` that renders the plugin content inside the sidebar layout shell. The plugin UI is fetched and rendered inline (not iframe). Tighter integration but plugin CSS/JS could conflict with kagent's.

**Option C: iframe with postMessage bridge** — Like Option A but with a lightweight postMessage API for theme sync, navigation events, and resize. Aligns with MCP Apps pattern (which also uses iframe + postMessage).

**User answer:** Option C — iframe with postMessage bridge. Provides full isolation (CSS/JS sandboxed) while enabling theme sync, resize, and navigation events via postMessage. Aligns with MCP Apps extension pattern.

---

## Q6: Migration of existing kanban integration

The current kanban UI has two integration points:
1. **nginx**: hardcoded `location /kanban-mcp/` proxy block
2. **Next.js**: full React page at `ui/src/app/kanban/page.tsx` with direct REST/SSE calls to `/kanban-mcp/`

With the new system, kanban would be:
1. RemoteMCPServer CRD gains `ui` metadata → controller persists to DB → Go proxy auto-routes `/plugins/kanban/`
2. Sidebar auto-discovers and renders nav item under AGENTS section
3. Next.js catch-all `/plugins/[name]/` renders iframe pointing to Go proxy

**Question:** Should we migrate the existing kanban integration to the new plugin system in this spec, or keep it as-is and only apply the new pattern to future plugins?

**Option A: Migrate kanban** — Remove hardcoded nginx block and `ui/src/app/kanban/page.tsx`. Kanban becomes the first plugin using the new system. Proves the pattern works end-to-end.

**Option B: Keep kanban as-is** — Only new plugins use the dynamic system. Less risk but two different patterns coexist.

**User answer:** Option A — Migrate kanban. It becomes the first plugin on the new dynamic system, proving the pattern end-to-end. Remove hardcoded nginx block and `ui/src/app/kanban/page.tsx`.

---

## Q7: postMessage bridge scope — what capabilities for v1?

The iframe postMessage bridge could support many features. For the initial implementation, which of these are must-haves vs. nice-to-haves?

1. **Theme sync** — Host sends current theme (light/dark) + CSS variables to iframe so plugin can match kagent styling
2. **Resize/height** — iframe auto-resizes to plugin content height, or fills available space
3. **Navigation events** — Plugin can trigger navigation in the host (e.g., open agent chat)
4. **Namespace context** — Host sends current namespace to iframe so plugin can filter data
5. **Auth token forwarding** — Host passes auth context to iframe for API calls (if auth is added later)
6. **Title/badge updates** — Plugin can update its sidebar badge (e.g., task count) dynamically

Which are must-haves for v1?

**User answer:** All six are must-haves for v1: theme sync, resize/height, navigation events, namespace context, auth token forwarding, and title/badge updates.

---

## Q8: Go reverse proxy — API path structure?

The Go backend needs to route plugin UI requests. Options for the URL path:

**Option A: `/api/plugins/{name}/`** — Under the existing `/api/` prefix. Consistent with backend API pattern but nginx already proxies `/api/` to the Go backend, so this works out of the box.

**Option B: `/plugins/{name}/`** — Top-level path. Cleaner URLs but requires a new nginx location block to proxy to the Go backend (one-time change, not per-plugin).

**User answer:** Option B — `/plugins/{name}/`. Cleaner URLs. One-time nginx change to add `location /plugins/` proxying to Go backend.

---

## Q9: Plugin UI listing API — does the UI need an endpoint to discover available plugins?

The sidebar needs to know which plugins have UIs. Options:

**Option A: New `/api/plugins` endpoint** — Returns list of plugins with UI metadata (name, displayName, icon, section, pathPrefix). Sidebar fetches this on load.

**Option B: Extend existing `/api/toolservers` response** — Add UI metadata to the existing tool server list response. No new endpoint, but mixes concerns.

**User answer:** Option A — New `/api/plugins` endpoint. Clean separation of concerns.

---

---

## Q10: Nginx routing conflict — browser URL vs. proxy URL collision

**Bug found during implementation review:** `location /plugins/` in nginx catches ALL `/plugins/*` requests and sends them to the Go backend. This means:

- **Client-side navigation** (clicking sidebar `<Link>`) works — Next.js handles it, no nginx involved
- **Hard refresh or direct URL** (`/plugins/kanban`) is broken — nginx sends to Go proxy → upstream service → user gets raw plugin HTML without Next.js layout/sidebar
- **iframe src** (`/plugins/kanban/`) works correctly via nginx → Go proxy → upstream

The browser URL (`/plugins/kanban`) and the internal proxy path (`/plugins/kanban/`) collide at the nginx level.

**Option A: Separate internal proxy path** — Change Go proxy to serve at `/_p/{name}/` instead of `/plugins/{name}/`. Nginx gets `location /_p/` → Go backend. Browser URL `/plugins/kanban` falls through to `location /` → Next.js. iframe src becomes `/_p/kanban/`.

**Option B: Next.js API route proxy** — Use Next.js API route `/api/plugin-proxy/[name]/[...path]` as proxy. Remove nginx `/plugins/` block. iframe src becomes `/api/plugin-proxy/kanban/`.

**Option C: next.config.ts rewrites** — Add rewrite rule in Next.js to proxy `/_p/*` to Go backend. Similar to Option A but uses Next.js rewrites instead of nginx.

**User answer:** Option A — Separate internal proxy path `/_p/{name}/`. Cleanest separation: browser URL stays `/plugins/kanban` (nice), internal proxy uses `/_p/kanban/` (clearly distinct). Minimal changes: rename Go route, update nginx location, update iframe src.

---

## Q11: Error handling gaps — silent failures cause "UI is empty"

**Bug found during implementation review:** Multiple silent failure modes cause "UI is empty" with no user feedback:

1. **Sidebar fetch swallows errors**: `.catch(() => {})` on `/api/plugins` fetch silently ignores network errors, auth failures, 500s. User sees no plugin items with no indication of why.
2. **iframe shows blank on upstream failure**: If upstream returns 502 Bad Gateway (service not running), iframe renders nothing. No fallback UI or error message.
3. **No loading state**: Sidebar shows no "Loading plugins..." indicator during fetch. Plugins appear after a flash of content.

**Requirements:**
- Sidebar must show a loading indicator while fetching `/api/plugins`
- Sidebar must show an error indicator if `/api/plugins` fails (with retry option)
- Plugin iframe page must detect load failures (iframe `onerror`, or timeout-based) and show a fallback "Plugin unavailable" message
- Plugin iframe page must show a loading skeleton while iframe content loads

**User answer:** All four requirements are must-haves. Silent empty UI is unacceptable for debugging and user experience.

---

## Q12: Browser E2E testing — Playwright tests for full UI verification

**Gap found during implementation review:** The existing E2E test (`go/core/test/e2e/plugin_routing_test.go`) only verifies the API pipeline (CRD → Controller → DB → `/api/plugins` → `/plugins/{name}/` proxy). It does NOT test:

1. Sidebar renders plugin nav items from `/api/plugins` response
2. Clicking a plugin nav item navigates to `/plugins/{name}`
3. Plugin page renders an iframe with correct src
4. iframe loads content from upstream service
5. postMessage bridge works (theme sync, badge updates)
6. Hard refresh on `/plugins/{name}` preserves sidebar layout
7. Plugin removal causes sidebar to update

**Requirements:**
- Add Playwright browser E2E tests covering items 1–7 above
- Tests must run against a deployed kagent instance (Kind cluster)
- Tests must use a mock plugin service (simple HTTP server returning test HTML)
- Tests must verify both client-side navigation and hard refresh scenarios
- CI integration: Playwright tests run as part of `make test-e2e` or separate `make test-e2e-browser`

**User answer:** All seven scenarios are required. Use Playwright for browser testing. Mock plugin service must include `kagent-plugin-bridge.js` integration to test postMessage bridge.

---

## Q13: API verification script improvements

**Gap found during implementation review:** `scripts/check-plugins-api.sh` exists but is not integrated into CI. Additional verification needed:

1. Script should verify `/api/plugins` response shape matches `StandardResponse[[]PluginResponse]`
2. Script should verify proxy endpoint returns non-404 for registered plugins
3. Script should be callable from E2E test suite
4. Script should support `--wait` flag to poll until plugin appears (for CI use after helm install)

**User answer:** Integrate into CI. Add `--wait` polling mode for use after helm deployments.

---

## Consolidated Requirements Summary

| # | Decision | Choice |
|---|----------|--------|
| Q1 | Proxy approach | Go reverse proxy (Option B) |
| Q2 | UI metadata location | Extend RemoteMCPServer CRD with `ui` section (Option A) |
| Q3 | UI service URL | Derive from existing `spec.url` (Option A), consider MCP Apps ext spec |
| Q4 | Sidebar placement | Configurable `ui.section` field, default "PLUGINS" (Option B) |
| Q5 | UI rendering | iframe with postMessage bridge (Option C) |
| Q6 | Kanban migration | Migrate to new plugin system (Option A) |
| Q7 | postMessage bridge scope | All 6 capabilities are v1 must-haves |
| Q8 | API path structure | `/plugins/{name}/` top-level path (Option B) |
| Q9 | Plugin discovery API | New `/api/plugins` endpoint (Option A) |
| Q10 | Nginx routing conflict fix | Separate internal proxy path `/_p/{name}/` (Option A) |
| Q11 | Error handling gaps | Loading/error states for sidebar and iframe (all 4 must-haves) |
| Q12 | Browser E2E testing | Playwright tests for 7 UI scenarios |
| Q13 | API verification in CI | Integrate check script with `--wait` polling mode |

