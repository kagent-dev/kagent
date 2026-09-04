# Server Everything MCP

This directory wires the public [Server Everything](https://servereverything.dev/mcp)
reference MCP server into kagent as a `RemoteMCPServer`, including the
`show-weather-dashboard` **MCP App** (an interactive UI widget rendered inline
in chat).

It is useful for verifying the chat MCP UI widget integration (EP-2046) against a
public server that uses a single dual-visibility (`["model", "app"]`) UI tool.

## What it provides

`server-everything` exposes these demo tools:

| Tool | Purpose |
|------|---------|
| `show-weather-dashboard` | Renders an interactive weather dashboard MCP App (UI widget). Advertised via `_meta.ui.resourceUri = ui://server-everything/weather-dashboard`, visibility `["model", "app"]`. |
| `echo` | Reflects text back. |
| `get-sum` | Adds two numbers. |
| `get-tiny-image` | Returns a small sample image. |
| `get-structured-content` | Demonstrates structured tool output. |

## Installation

```bash
kubectl apply -f server-everything-remote-mcpserver.yaml
```

This creates:
- a `RemoteMCPServer` named `server-everything` pointing at `https://servereverything.dev/mcp`

No extra Helm values are needed — MCP App (UI widget) rendering is detected
automatically from each tool's `_meta.ui` metadata.

## Verify

```bash
# RemoteMCPServer should reach Accepted and discover tools
kubectl get remotemcpserver server-everything -n kagent -o yaml
```

Bind the server to an `AgentTemplate` to exercise its tools from an agent.

## Learn More

- [Server Everything](https://servereverything.dev/mcp)
- [MCP Protocol](https://modelcontextprotocol.io/)
- MCP UI widgets in kagent: `design/EP-2046-chat-mcp-ui-widgets.md`
