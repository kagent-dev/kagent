# Plugin path consistency: Go, Plugins (Helm/CRD), UI

## Intended contract

| Layer | Path | Purpose |
|-------|------|---------|
| **Browser URL** | `/plugins/{pathPrefix}` | Next.js page (sidebar + iframe). User-facing. |
| **Proxy (Go)** | `/_p/{pathPrefix}/` | Go backend reverse-proxies to plugin service. Used as iframe `src`. |
| **API** | `/api/plugins` | Returns list of plugins; each has `pathPrefix` used in the two paths above. |

- **pathPrefix** comes from `RemoteMCPServer.spec.ui.pathPrefix` (or defaults to server name). Stored in DB; must be a single token (e.g. `kanban`, `temporal`).
- **UI** builds: sidebar link `href={/plugins/${p.pathPrefix}}`, iframe `src={/_p/${name}/}` where `name` is the route param (same as pathPrefix).

## Inconsistencies found and fixed

### 1. Temporal E2E test used wrong URL for proxy check (fixed)

- **File:** `go/core/test/e2e/temporal_test.go`
- **Was:** `proxyURL := baseURL + "/plugins/temporal/"` — that hits nginx `location /` → Next.js, not the Go proxy.
- **Should be:** `proxyURL := baseURL + "/_p/temporal/"` — same as `plugin_routing_test.go`, which correctly uses `/_p/test-plugin/` to verify the proxy.

### 2. CRD description incomplete (doc-only)

- **File:** `go/api/config/crd/bases/kagent.dev_remotemcpservers.yaml` (generated from Go types)
- **CRD says:** "When ui.enabled is true, the server's UI is accessible at /plugins/{ui.pathPrefix}/"
- **Go types say:** "accessible via /_p/{ui.pathPrefix}/ (proxy) and browser URL /plugins/{ui.pathPrefix}"
- The CRD does not mention the `/_p/` proxy path. For a single source of truth, the comment in `go/api/v1alpha2/remotemcpserver_types.go` is correct; the CRD description is generated and only mentions the browser URL. No code change required; keep Go types as the source.

## Consistency checklist

- [x] **Go:** Proxy route `/_p/{name}`, lookup by pathPrefix in DB.
- [x] **UI:** Sidebar links to `/plugins/${p.pathPrefix}`; iframe `src=/_p/${name}/` (name = pathPrefix from route).
- [x] **Helm (kanban-mcp):** `pathPrefix: "kanban"` → `/plugins/kanban`, `/_p/kanban/`.
- [x] **Helm (temporal):** `pathPrefix: "temporal"` → `/plugins/temporal`, `/_p/temporal/`.
- [x] **E2E:** Use `/_p/{pathPrefix}/` when asserting the Go proxy; use `/plugins/{pathPrefix}` only for browser/Next.js flows.
