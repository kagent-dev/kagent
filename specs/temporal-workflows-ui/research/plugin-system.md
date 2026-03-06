# Plugin System Research

## How Plugins Work in KAgent

The plugin system is CRD-driven. Navigation and menu items are managed by RemoteMCPServer CRD, not hardcoded in the UI.

### Plugin Lifecycle

1. **CRD Creation:** RemoteMCPServer with `spec.ui.enabled: true`
2. **Controller reconciles:** `reconcilePluginUI()` creates Plugin DB record
3. **Sidebar discovery:** Browser fetches `GET /api/plugins`, plugins merged into nav sections
4. **Navigation:** Click → `/plugins/{pathPrefix}` → Next.js page with iframe
5. **Iframe proxy:** iframe src=`/_p/{pathPrefix}/` → Go reverse proxy → upstream service
6. **Plugin bridge:** postMessage protocol for theme, badges, navigation

### CRD Spec: PluginUISpec

```go
type PluginUISpec struct {
    Enabled      bool   `json:"enabled,omitempty"`
    PathPrefix   string `json:"pathPrefix,omitempty"`    // URL segment
    DisplayName  string `json:"displayName,omitempty"`   // Sidebar label
    Icon         string `json:"icon,omitempty"`          // lucide-react icon
    Section      string `json:"section,omitempty"`       // OVERVIEW, AGENTS, RESOURCES, ADMIN, PLUGINS
}
```

### Kanban MCP Example

```yaml
apiVersion: kagent.dev/v1alpha2
kind: RemoteMCPServer
metadata:
  name: kanban-mcp
spec:
  protocol: STREAMABLE_HTTP
  url: http://kanban-mcp.kagent.svc.cluster.local:8080/mcp
  ui:
    enabled: true
    pathPrefix: "kanban"
    displayName: "Kanban Board"
    icon: "kanban"
    section: "AGENTS"
```

### Temporal UI Already Registered as Plugin

```yaml
# helm/kagent/templates/temporal-ui-remotemcpserver.yaml
kind: RemoteMCPServer
spec:
  url: http://temporal-ui:8080
  ui:
    enabled: true
    pathPrefix: "temporal"
    displayName: "Temporal Workflows"
    section: "PLUGINS"
```

### Key Architecture Points

- **Two URL paths:** `/plugins/{name}` (browser, with sidebar) vs `/_p/{name}/` (iframe proxy)
- **Plugin bridge SDK:** `go/plugins/kagent-plugin-bridge.js` — lightweight JS for connect, badges, theme
- **SSE support:** Reverse proxy uses `FlushInterval: -1`
- **Icon resolution:** kebab-case → PascalCase → lucide-react component, fallback to Puzzle

### Implications for Workflows Page

The current `/workflows` stub page should be **removed** in favor of the Temporal UI plugin. Since the Temporal UI is already registered as a RemoteMCPServer with `ui.enabled: true`, it will appear in the sidebar automatically when Temporal is enabled.

However, the current Temporal UI (temporalio/ui) is a generic Temporal dashboard. The question is: do we want a **custom workflows page** that's kagent-aware (shows agent names, links to sessions) or is the stock Temporal UI sufficient?
