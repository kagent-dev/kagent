# Research: Current Architecture for Plugin UI Routing

## Current State

### Plugins (go/plugins/)
Two Go MCP server plugins exist:
- **kanban-mcp** — Kanban board with embedded SPA UI, REST API, SSE, MCP tools
- **gitrepo-mcp** — Git repo search with REST API, MCP tools (no embedded UI currently)

Both are standalone HTTP servers deployed as K8s Services + RemoteMCPServer CRDs via Helm charts in `helm/tools/`.

### Nginx Routing (ui/conf/nginx.conf)
Static proxy rules:
- `/kanban-mcp/` → `http://kanban-mcp.kagent.svc.cluster.local:8080` (hardcoded, DNS-resolved)
- `/api/` → Go backend
- `/api/ws/` → WebSocket backend
- `/a2a/` → A2A routes
- `/` → Next.js UI

**Problem**: Adding a new plugin UI requires manually editing nginx.conf.

### UI Sidebar (ui/src/components/sidebars/AppSidebarNav.tsx)
Navigation is hardcoded in `NAV_SECTIONS` array. Kanban is a static entry under AGENTS section.

### Kanban UI Integration Pattern
- `ui/src/app/kanban/page.tsx` — Full React page with hardcoded `KANBAN_BASE_URL = "/kanban-mcp/"`
- Fetches REST API and SSE events from the proxied plugin backend
- Not an iframe — direct fetch calls from the Next.js page

### RemoteMCPServer CRD
- Has: url, protocol, description, headers, timeouts, allowedNamespaces
- Missing: any UI metadata (path prefix, display name, icon, sidebar section)

### Helm Tool Charts
- `helm/tools/kanban-mcp/` — Deploys: Service, Deployment, ConfigMap, RemoteMCPServer
- Service name follows Helm `fullname` template
- RemoteMCPServer URL uses internal service URL for MCP tool discovery

## Key Observations

1. **Split responsibility**: MCP tools discovered dynamically; UIs integrated statically
2. **Plugins serve on root `/`**: kanban-mcp serves its SPA at `/`, API at `/api/*` — nginx strips the prefix
3. **DNS resolution**: nginx uses `resolver kube-dns...` with variable-based proxy_pass for graceful startup
4. **No plugin registry**: No API endpoint lists available plugin UIs
