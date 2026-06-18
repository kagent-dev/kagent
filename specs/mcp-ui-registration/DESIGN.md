# MCP UI Plugin Registration via RemoteMCPServer CRD

**Status:** Draft v1.0
**Date:** 2026-06-01
**Branch:** `feature/mcp-kanban-server`
**Scope:** How an MCP server with an embedded web UI (e.g. `kanban-mcp`) registers
itself as a first-class plugin in the kagent UI sidebar, using the existing
`RemoteMCPServer` CRD.

---

## 1. Goal

Let any MCP server that ships its own web UI surface that UI **inside** the kagent
console — listed in the sidebar, framed in an iframe, theme- and namespace-aware —
**without** the UI needing to know it is embedded and **without** adding a new CRD.

Registration is fully declarative: a single `RemoteMCPServer` resource with a
`spec.ui` block is the entire contract.

---

## 2. The contract: `RemoteMCPServer.spec.ui`

The `ui` block was added to the `RemoteMCPServer` CRD and is now backed by the Go
type `RemoteMCPServerUI` in `go/api/v1alpha2/remotemcpserver_types.go`. Source of
truth is the Go type; the CRD at
`go/api/config/crd/bases/kagent.dev_remotemcpservers.yaml` and the deepcopy methods
are generated from it (`make -C go generate`).

| Field         | Type     | Default   | Validation                          | Purpose |
|---------------|----------|-----------|-------------------------------------|---------|
| `enabled`     | bool     | `false`   | —                                   | Opt-in: this server provides a web UI. |
| `pathPrefix`  | string   | `<name>`  | `maxLength=63`, `^[a-z0-9][a-z0-9-]*[a-z0-9]$` | URL segment for routing: `/_p/{pathPrefix}/`. |
| `displayName` | string   | `<name>`  | —                                   | Human-readable label in the sidebar. |
| `icon`        | string   | `puzzle`  | —                                   | `lucide-react` icon name (e.g. `kanban`). |
| `section`     | enum     | `PLUGINS` | `OVERVIEW\|AGENTS\|RESOURCES\|ADMIN\|PLUGINS` | Sidebar section the entry appears under. |
| `defaultPath` | string   | `/`       | —                                   | Initial sub-path to open at plugin root. |
| `injectCSS`   | string   | —         | —                                   | Custom CSS injected into proxied HTML (e.g. hide the plugin's own nav). |

> `pathPrefix` and `displayName` default to the `RemoteMCPServer` metadata name when
> omitted. The pattern on `pathPrefix` guarantees it is a safe single URL path
> segment for the reverse proxy.

### Example — registering `kanban-mcp`

```yaml
apiVersion: kagent.dev/v1alpha2
kind: RemoteMCPServer
metadata:
  name: kanban-mcp
  namespace: kagent
spec:
  description: Kanban board MCP server (tasks, board, attachments)
  protocol: STREAMABLE_HTTP
  url: http://kanban-mcp.kagent:8080/mcp        # MCP endpoint (tools discovery)
  timeout: 30s
  sseReadTimeout: 5m0s
  terminateOnClose: true
  ui:
    enabled: true
    pathPrefix: kanban                          # → /_p/kanban/ , /plugins/kanban
    displayName: Kanban
    icon: kanban
    section: PLUGINS
    defaultPath: /
    # injectCSS: '[data-testid="navigation-header"] { display: none !important; }'
```

Note the two distinct endpoints on the same service:
- **`spec.url`** (`/mcp`) — the MCP protocol endpoint the controller connects to for
  tool discovery (`status.discoveredTools`) and that agents call as a tool.
- **`spec.ui` → `/_p/{pathPrefix}/`** — the HTTP UI the backend reverse-proxies to the
  server's web root (`/` on the same `kanban-mcp:8080` service).

---

## 3. End-to-end architecture

```
┌────────────────────────────────────────────────────────────────────────────┐
│ Browser (kagent UI, Next.js)                                                 │
│                                                                              │
│  Sidebar  ──GET /plugins──►  [list of enabled plugins]                       │
│    │                                                                         │
│    │ click "Kanban"                                                          │
│    ▼                                                                         │
│  /plugins/kanban  (Next.js page, keeps app chrome: sidebar, theme, ns)       │
│    │                                                                         │
│    └── <iframe src="/_p/kanban/">  ◄──postMessage──► plugin                  │
│            kagent:context (theme, namespace)                                 │
│            kagent:navigate / resize / badge / title / ready                  │
└───────────────────────────────┬──────────────────────────────────────────────┘
                                 │ /_p/kanban/...   and   /plugins (list)
                                 ▼
┌────────────────────────────────────────────────────────────────────────────┐
│ kagent HTTP server (Go, go/core/internal/httpserver)                         │
│                                                                              │
│  GET /plugins              ── list RemoteMCPServers where spec.ui.enabled    │
│                               → [{name, pathPrefix, displayName, icon,       │
│                                   section}]                                  │
│                                                                              │
│  /_p/{pathPrefix}/*        ── reverse proxy to the RemoteMCPServer whose     │
│                               spec.ui.pathPrefix == {pathPrefix}, targeting  │
│                               the host of spec.url (web root "/"), with      │
│                               spec.ui.injectCSS spliced into HTML responses  │
└───────────────────────────────┬──────────────────────────────────────────────┘
                                 │ http://kanban-mcp.kagent:8080/...
                                 ▼
┌────────────────────────────────────────────────────────────────────────────┐
│ kanban-mcp pod (go/plugins/kanban-mcp)                                       │
│   /          embedded SPA (board UI)                                         │
│   /api/...   REST                                                            │
│   /events    SSE                                                             │
│   /mcp       MCP over Streamable HTTP   ◄── spec.url                         │
└────────────────────────────────────────────────────────────────────────────┘
```

---

## 4. What already exists vs. what is missing

### Implemented (UI side, `ui/`)
- **Sidebar** `ui/src/components/sidebars/AppSidebarNav.tsx` — a static "Plugins"
  entry plus `PluginNav` items ({label, pathPrefix, icon, section}) driven by
  `useSidebarStatus()`, with live badge updates via the `kagent:plugin-badge` event.
- **Plugin list action** `ui/src/app/actions/plugins.ts` — `getPlugins()` calls
  `GET /plugins` and expects `PluginItem[] = {name, pathPrefix, displayName, icon,
  section}`; `checkPluginBackend()` probes `GET /_p/{pathPrefix}/` for health.
- **Plugin frame** `ui/src/app/plugins/[name]/[[...path]]/page.tsx` — renders an
  `<iframe src="/_p/{name}{subPath}">`, sandboxed
  (`allow-scripts allow-same-origin allow-forms allow-popups`), and speaks the
  `kagent:*` postMessage protocol: host → plugin `kagent:context` (theme, namespace,
  authToken); plugin → host `kagent:navigate | resize | badge | title | ready`.

### Implemented (API side)
- **CRD + Go type alignment** — `spec.ui` is defined on `RemoteMCPServerUI`, the CRD
  and `zz_generated.deepcopy.go` regenerate cleanly from it (verified: regeneration
  reproduces the `ui` block byte-for-byte; `go build ./...` passes).
> **Path contract.** The UI's BFF route `ui/src/app/api/plugins/route.ts` fetches
> `getBackendUrl() + "/plugins"`, and `getBackendUrl()` already ends in `/api` — so the
> backend registry path is **`/api/plugins`**. The reverse proxy is reached via
> `getBackendRoot()` (which strips `/api`), so it stays at the **root** `/_p/...`.

- **`GET /api/plugins`** — `PluginsHandler.HandleListPlugins`
  (`go/core/internal/httpserver/handlers/plugins.go`) lists `RemoteMCPServer`s across
  the watched namespaces (all namespaces when none configured) where
  `spec.ui.enabled == true`, projects each to `api.PluginResponse`
  (`{name, namespace, pathPrefix, displayName, icon, section, defaultPath}`), applies
  the `pathPrefix`/`displayName` → name and `icon`=`puzzle` / `section`=`PLUGINS`
  defaults, and sorts by `pathPrefix`. Authorized as a `ToolServer` resource.
- **`/_p/{pathPrefix}/*` reverse proxy** — `PluginsHandler.HandleProxy` resolves
  `{pathPrefix}` to its `RemoteMCPServer` (by effective pathPrefix), authorizes
  against the backing `ToolServer`, derives the target from the **host** of
  `spec.url` (stripping `/_p/{pathPrefix}` and proxying the remainder to the web
  root), resolves `spec.headersFrom` via `RemoteMCPServer.ResolveHeaders`, injects
  `spec.ui.injectCSS` into `text/html` responses, and returns `502` when the upstream
  is unreachable. Registered in `server.go` as
  `GET /api/plugins` + `PathPrefix("/_p/{pathPrefix}")`.

Covered by `plugins_test.go`: list filtering/defaults/sorting, proxy root + subpath
forwarding, CSS injection, unknown-prefix `404`, and real mux `PathPrefix` wiring.

### Remaining / future work
- `spec.allowedNamespaces` is **not yet enforced** by the proxy/list path (currently
  scoped to `WatchedNamespaces`); cross-namespace attachment filtering is a follow-up.
- `pathPrefix` collision handling: first match wins in `findUIServerByPrefix`; a
  validation/warning pass for duplicate prefixes across namespaces is a follow-up.
- `authToken` in `kagent:context` is still `null` (see open questions).

---

## 5. Registration flow (declarative, per server)

1. The MCP server's Helm chart (e.g. `contrib/plugins/kanban-mcp`) ships a
   `RemoteMCPServer` template with a `spec.ui` block, gated behind a values flag
   (`remoteMCPServer.enabled`) and behind the kagent CRDs being installed.
2. The kagent controller reconciles the resource and populates
   `status.discoveredTools` from `spec.url` (`/mcp`).
3. `GET /plugins` surfaces the server because `spec.ui.enabled == true`; the sidebar
   renders an entry under `spec.ui.section` with `spec.ui.icon` /
   `spec.ui.displayName`.
4. Navigating to `/plugins/{pathPrefix}` frames `/_p/{pathPrefix}/`, which the
   backend proxies to the server's web root; the plugin receives theme + namespace
   via `kagent:context`.

---

## 6. Helm wiring for kanban-mcp (implemented)

Added to `contrib/plugins/kanban-mcp`:

- `templates/remotemcpserver.yaml` — emits the resource above, with `spec.url` built
  from a `kanban-mcp.serverUrl` helper (`http://<fullname>.<ns>:<port>/mcp`,
  mirroring `helm/tools/grafana-mcp`) and a `spec.ui` block from values.
- `values.yaml`:
  ```yaml
  remoteMCPServer:
    enabled: true
    description: "Kanban board MCP server (tasks, board, attachments)"
    timeout: 30s
    sseReadTimeout: 5m0s
    terminateOnClose: true
    ui:
      enabled: true
      pathPrefix: kanban
      displayName: Kanban
      icon: kanban
      section: PLUGINS
      defaultPath: /
      injectCSS: ""
  ```
- `_helpers.tpl` — add the `kanban-mcp.serverUrl` helper.

---

## 7. Security & operational considerations

- **Iframe sandbox** — the host already restricts the frame to
  `allow-scripts allow-same-origin allow-forms allow-popups`; the proxy must set
  appropriate `Content-Security-Policy` / `X-Frame-Options` so only the kagent origin
  may frame the plugin.
- **AuthN/Z** — the proxy must apply the same authorizer used elsewhere (cf.
  `ToolServer` resource checks in `mcpapps.go`) and honor `spec.allowedNamespaces`
  before proxying cross-namespace.
- **Header injection** — secrets referenced by `spec.headersFrom` must be resolved
  server-side via `RemoteMCPServer.ResolveHeaders` and never exposed to the browser.
- **`pathPrefix` collisions** — `GET /plugins` / the proxy registry must reject or
  de-duplicate two enabled servers sharing a `pathPrefix`.
- **Health/degradation** — `checkPluginBackend` already classifies
  `ok|unreachable|not_found`; the proxy should return `502/503` (not `404`) when the
  upstream is down so the sidebar can show a degraded state.

---

## 8. Open questions

1. Should `/plugins` be namespace-scoped (per the active UI namespace) or
   cluster-wide with `allowedNamespaces` filtering?
2. Where does the proxy derive the UI base URL — strictly the host of `spec.url`, or
   a separate optional `spec.ui.url` for servers whose UI is on a different
   host/port than their `/mcp` endpoint?
3. Is `authToken` in `kagent:context` (currently `null`) required for plugins that
   call back into kagent APIs from inside the iframe?

---

## 9. Acceptance criteria

- [x] `RemoteMCPServerUI` Go type matches the CRD `spec.ui` block; `make generate`
      reproduces the CRD and deepcopy with no drift; `go build ./...` passes.
- [x] `GET /plugins` returns enabled UI servers as `PluginItem[]` (with defaults +
      sorting). Covered by `TestHandleListPlugins`.
- [x] `/_p/{pathPrefix}/*` reverse-proxies to the resolved server with header
      resolution, `injectCSS`, auth checks, and `502` on upstream failure. Covered by
      `TestHandleProxy` (root, subpaths, CSS, 404, mux wiring).
- [x] `kanban-mcp` Helm chart ships a `RemoteMCPServer` with `spec.ui`; `helm lint`
      passes and the template renders the expected resource. (End-to-end "Kanban in
      sidebar" requires a live cluster deploy — see test plan below.)
- [ ] `allowedNamespaces` enforcement and `pathPrefix` collision handling (follow-up).

## 10. Test plan

### A. Minimal standalone demo (no kagent — fastest) ✅ ready
Exercises the kanban server itself: UI + MCP + REST + SSE on one port.
```
cd go/plugins/kanban-mcp && docker compose up --build
# → http://localhost:8080/         embedded Kanban board UI
# → http://localhost:8080/mcp      MCP Streamable HTTP endpoint
# → http://localhost:8080/api/board  REST
# → http://localhost:8080/events   SSE
```
`docker compose` brings up Postgres + the server; the server self-applies its
`kanban` schema migrations on startup.

### B. kagent plugin integration (live cluster)
1. Install kagent CRDs + app, then `helm install kanban contrib/plugins/kanban-mcp -n
   kagent` with a Postgres `database.url`/`existingSecret`. Build/push the image first
   (`docker build -f go/plugins/kanban-mcp/Dockerfile -t <repo>/kanban-mcp .`) and set
   `image.repository`/`image.tag`.
2. `kubectl get remotemcpserver -n kagent` → `kanban-mcp` present; `status.discoveredTools`
   populated by the controller.
3. `curl $KAGENT/api/plugins?user_id=admin@kagent.dev` → JSON array containing the kanban
   entry with `pathPrefix=kanban`, `icon=kanban`, `section=PLUGINS`.
4. `curl $KAGENT/_p/kanban/` → the board HTML (200); `curl $KAGENT/_p/kanban/api/board`
   → board JSON. (Note the backend root path `/_p`, not `/api/_p`.)
5. Open the kagent UI → "Kanban" appears in the sidebar Plugins section and loads the
   board in the iframe.
