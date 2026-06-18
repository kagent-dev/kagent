# EP-2047: UI Plugins — register MCP server web UIs via RemoteMCPServer.spec.ui

* Issue: [#2047](https://github.com/kagent-dev/kagent/issues/2047)

## Background

Some MCP servers ship their own web UI (e.g. a Kanban board, a dashboard). Today
there is no way to surface that UI inside the kagent console — users must open a
separate URL, losing the kagent theme, namespace context, and navigation chrome.

This EP lets any MCP server that ships a web UI register itself as a first-class
**plugin** in the kagent UI sidebar, framed in an iframe, theme- and
namespace-aware — **without** the UI needing to know it is embedded and **without**
adding a new CRD. Registration is fully declarative: a single `RemoteMCPServer`
resource with a `spec.ui` block is the entire contract.

The full design (architecture diagram, postMessage protocol, proxy contract) lives
in `specs/mcp-ui-registration/DESIGN.md`; this EP summarizes it.

## Motivation

- Let MCP servers contribute UI surfaces to the kagent console with zero controller
  changes and no new CRD.
- Keep the embedding declarative and namespace/theme-aware.
- Provide the foundation for shipped plugins such as `kanban-mcp` (EP-2048).

### Goals

- Add an optional `spec.ui` block to the `RemoteMCPServer` v1alpha2 CRD.
- Backend: list enabled plugins (`GET /api/plugins`) and reverse-proxy each
  plugin's web root under `/_p/{pathPrefix}/`, with optional CSS injection.
- UI: a "Plugins" navigation section, a plugin frame page, and a host↔plugin
  `postMessage` protocol (context, navigate, resize, badge, title, ready).

### Non-Goals

- A new CRD for plugins (reuse `RemoteMCPServer`).
- Shipping a specific plugin (the Kanban plugin is EP-2048).
- Cross-origin plugin hosting (plugins are proxied same-origin via `/_p/`).

## Implementation Details

### The contract: `RemoteMCPServer.spec.ui` (`RemoteMCPServerUI`)

Defined in `go/api/v1alpha2/remotemcpserver_types.go`; the CRD
(`go/api/config/crd/bases/kagent.dev_remotemcpservers.yaml`, mirrored in
`helm/kagent-crds/templates/`) and `zz_generated.deepcopy.go` are generated from it.

| Field | Type | Default | Validation | Purpose |
|-------|------|---------|------------|---------|
| `enabled` | bool | `false` | — | Opt-in: this server provides a web UI. |
| `pathPrefix` | string | `<name>` | `maxLength=63`, `^[a-z0-9][a-z0-9-]*[a-z0-9]$` | URL segment for `/_p/{pathPrefix}/`. |
| `displayName` | string | `<name>` | — | Sidebar label. |
| `icon` | string | `puzzle` | — | `lucide-react` icon name. |
| `section` | enum | `PLUGINS` | `OVERVIEW\|AGENTS\|RESOURCES\|ADMIN\|PLUGINS` | Sidebar section. |
| `defaultPath` | string | `/` | — | Initial sub-path at plugin root. |
| `injectCSS` | string | — | — | CSS injected into proxied HTML. |

### Backend (`go/core/internal/httpserver`)

- `GET /api/plugins` — `PluginsHandler.HandleListPlugins` lists `RemoteMCPServer`s
  across watched namespaces where `spec.ui.enabled`, projects each to
  `api.PluginResponse` (`{name, namespace, pathPrefix, displayName, icon, section,
  defaultPath}`), applies the defaults, and sorts by `pathPrefix`. Authorized as a
  `ToolServer` resource.
- `/_p/{pathPrefix}/*` — `PluginsHandler.HandleProxy` resolves `{pathPrefix}` to its
  `RemoteMCPServer`, authorizes against the backing `ToolServer`, derives the target
  from the **host** of `spec.url` (proxying to the web root `/`), resolves
  `spec.headersFrom` via `RemoteMCPServer.ResolveHeaders`, injects
  `spec.ui.injectCSS` into `text/html` responses, and returns `502` when the
  upstream is unreachable.
- Wiring (added to the shared `handlers.go`/`server.go`/`httpapi/types.go`): the
  `Plugins` handler, the `/api/plugins` route, and the `/_p/{pathPrefix}` proxy
  prefix. Only the plugins-related hunks of these shared files are included in this
  PR; the MCP-apps hunks belong to EP-2046.

### UI (`ui/src`)

- **Navigation** — `components/sidebars/AppSidebar.tsx` + `AppSidebarNav.tsx` render
  a "Plugins" section driven by `useSidebarStatus()`, with live badge updates via the
  `kagent:plugin-badge` event. Supporting nav pieces: `NamespaceSelector`,
  `StatusIndicator`, `SidebarCollapseButton`, `MobileTopBar`, and the
  `sidebar-status-context` / `namespace-context` providers. The root `layout.tsx` is
  refactored to a Server Component delegating to a `providers.tsx` client boundary.
- **Plugin list** — `app/actions/plugins.ts` (`getPlugins()` → `GET /api/plugins`,
  `checkPluginBackend()` health probe) and the BFF route `app/api/plugins/route.ts`.
- **Plugin frame** — `app/plugins/[name]/[[...path]]/page.tsx` renders
  `<iframe src="/_p/{name}{subPath}">`, sandboxed
  (`allow-scripts allow-same-origin allow-forms allow-popups`), speaking the
  `kagent:*` postMessage protocol: host→plugin `kagent:context` (theme, namespace,
  authToken); plugin→host `kagent:navigate | resize | badge | title | ready`.
  `app/plugins/page.tsx` lists available plugins.

### Path contract

`getBackendUrl()` ends in `/api`, so the registry is at `/api/plugins`; the reverse
proxy is reached via `getBackendRoot()` (strips `/api`) so it stays at the root
`/_p/...`. (`getBackendRoot` added to `ui/src/lib/utils.ts`.)

### Dependencies

- The sidebar `UserMenu` gains a `variant="sidebar"` rendering used by `AppSidebar`;
  that `UserMenu` change ships in this PR. The SSO session-status behavior is
  separate (**EP-2045**, #2045) and does not block this PR.

## Test Plan

- **Unit (Go):** `plugins_test.go` covers `HandleListPlugins` projection/defaults
  and `HandleProxy` target derivation, header resolution, CSS injection, and 502
  handling. `go build ./core/... ./api/...` passes.
- **Unit (UI):** plugin list/health actions; nav rendering from `useSidebarStatus`.
- **e2e / manual:** create a `RemoteMCPServer` with `spec.ui.enabled`; confirm it
  appears under "Plugins", the iframe loads via `/_p/{pathPrefix}/`, theme/namespace
  propagate, and badge/title updates work.

## Alternatives

- **New `Plugin` CRD** — more moving parts; rejected in favor of reusing
  `RemoteMCPServer`, which already models the server identity and auth.
- **Cross-origin iframe to the plugin's own host** — breaks same-origin auth/theme
  propagation and complicates CSP; the same-origin `/_p/` proxy avoids this.

## Open Questions

- Should `section` support custom (non-enum) sidebar sections?
- Should plugin health/badges be server-pushed (SSE) rather than polled?
