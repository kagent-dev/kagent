# Parallel Search MCP

This directory registers the public [Parallel Search MCP](https://docs.parallel.ai/integrations/mcp/search-mcp)
endpoint in kagent as a `RemoteMCPServer`. The default endpoint requires no
account or API key.

## What it provides

| Tool | Purpose |
|------|---------|
| `web_search` | Searches the live web for current information. |
| `web_fetch` | Extracts clean Markdown from a URL. |

## Installation

```bash
kubectl apply -f parallel-search-remote-mcpserver.yaml
```

This creates a `RemoteMCPServer` named `parallel-search` that points to
`https://search.parallel.ai/mcp`. Agents must explicitly reference the server
and select its tools; applying the manifest does not change existing agents.

## Verify

```bash
kubectl get remotemcpserver parallel-search -n kagent -o yaml
```

The `Accepted` condition should become true, and `status.discoveredTools`
should list `web_search` and `web_fetch`.

## Learn more

- [Parallel Search MCP documentation](https://docs.parallel.ai/integrations/mcp/search-mcp)
- [MCP protocol](https://modelcontextprotocol.io/)
