# Rough Idea

**Input:** Support dynamic MCP UI routing for plugins

## Description

Currently, MCP tool servers (like kanban-mcp, gitrepo-mcp) that provide their own web UIs require hardcoded nginx proxy rules and hardcoded Next.js routes/sidebar entries. Adding a new plugin UI means modifying nginx.conf, adding a Next.js page, and updating sidebar navigation — all in the core kagent codebase.

The goal is to make MCP plugin UIs dynamically discoverable and routable:

1. **CRD extension**: Add optional UI metadata to RemoteMCPServer CRD (UI path prefix, display name, icon, sidebar section)
2. **Dynamic proxy**: Replace hardcoded nginx location blocks with a dynamic proxy mechanism that routes `/plugins/<name>/` to the plugin's service based on CRD metadata
3. **Dynamic sidebar**: Sidebar navigation auto-discovers plugins with UIs and renders nav items dynamically
4. **Plugin UI hosting**: Each MCP server can optionally serve its own web UI, which gets embedded under the kagent shell (sidebar + header remain visible)
5. **Plugins status page** - UI /plugins and show there internal info on /api/plugins and status of plugin backends if they are available - healthcheck status page. Also allow enable/disable plugins

This decouples plugin UI integration from the core codebase — deploying a new RemoteMCPServer CRD with UI metadata automatically makes its UI accessible.
