# Redis Agent Memory Server MCP Sample

This is a sample implementation of an MCP server that delegates to a running Redis Agent Memory Server instance using the `agent-memory-client` SDK.

## Prerequisites

- A running instance of Redis Agent Memory Server (see [agent-memory-server](https://github.com/redis/agent-memory-server)).
- Docker installed.
- Kubernetes cluster with `kagent` installed.

## Configuration

The server is configured via environment variables:

- `MEMORY_API_URL`: The URL of the Redis Agent Memory Server API (default: `http://localhost:8000`).
- `MEMORY_API_KEY`: API key for authentication (optional).
- `MCP_PORT`: Port for the MCP server if running in SSE mode (default: `8000`).
- `MCP_TRANSPORT`: Transport mode, either `stdio` (default) or `sse`.

## Usage

### Building the Docker Image

```bash
docker build -t redis-agent-memory-mcp .
```

### Running Locally (Stdio)

To use this server locally with an MCP client (like Claude Desktop or Gemini CLI) via Stdio:

```bash
docker run -i --rm -e MEMORY_API_URL=http://host.docker.internal:8000 redis-agent-memory-mcp
```

*Note: Use `host.docker.internal` to access services running on the host machine from within the container.*

### Kubernetes Deployment (kagent)

The recommended way to use this with `kagent` is as an `MCPServer` resource. When you define an `MCPServer`, `kagent` automatically manages its lifecycle and creates a matching `RemoteMCPServer` that serves it over SSE.

1.  Build and push the Docker image to a registry accessible by your cluster.
2.  Update `mcp-server.yaml` with your image and Redis Memory Server URL.
3.  Apply the manifest:

```bash
kubectl apply -f mcp-server.yaml
```

#### Configuring Agent Memory

To enable the agent to use this server for memory, configure the `memory` section in your `Agent` resource.

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: my-agent
spec:
  declarative:
    memory:
      type: Mcp
      mcp:
        name: redis-memory-mcp
        # kind: MCPServer # Optional, default
        # apiGroup: kagent.dev # Optional, default
```

You do not need to list the MCP server in the `tools` section if you only intend to use it for memory. The controller will automatically configure the connection for the agent.
