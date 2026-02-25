# kanban-mcp Helm Chart

Helm chart for deploying the `kanban-mcp` server into Kubernetes.

The server exposes:
- HTTP API
- MCP endpoint
- Embedded board UI
- SQLite (default) or Postgres-backed storage

Default image: `ghcr.io/kagent-dev/kanban-mcp:latest`  
Default service port: `8080`  
Default namespace in examples: `kagent`

## Chart Location

From repo root:

`contrib/tools/kanban-mcp`

## Quick Start

Install:

```bash
helm upgrade --install kanban-mcp ./contrib/tools/kanban-mcp \
  -n kagent \
  --create-namespace \
  --wait --timeout 5m
```

Check release:

```bash
helm status kanban-mcp -n kagent
```

Uninstall:

```bash
helm uninstall kanban-mcp -n kagent
```

## Local Development Image Workflow (Kind)

If you built the image locally and want the cluster to use it:

```bash
# from repo root
docker build -f go/cmd/kanban-mcp/Dockerfile \
  -t ghcr.io/kagent-dev/kanban-mcp:latest .

kind load docker-image ghcr.io/kagent-dev/kanban-mcp:latest --name kagent

helm upgrade --install kanban-mcp ./contrib/tools/kanban-mcp \
  -n kagent \
  --create-namespace \
  --wait --timeout 5m
```

## Configuration

Primary values in `values.yaml`:

| Key | Default | Description |
|---|---|---|
| `replicaCount` | `1` | Number of pod replicas |
| `image.repository` | `ghcr.io/kagent-dev/kanban-mcp` | Container image repository |
| `image.tag` | `latest` | Container image tag |
| `image.pullPolicy` | `IfNotPresent` | Pull policy |
| `service.type` | `ClusterIP` | Kubernetes service type |
| `service.port` | `8080` | Service port |
| `config.addr` | `:8080` | Server bind address (`KANBAN_ADDR`) |
| `config.transport` | `http` | Transport mode (`KANBAN_TRANSPORT`) |
| `config.dbType` | `sqlite` | `sqlite` or `postgres` (`KANBAN_DB_TYPE`) |
| `config.dbPath` | `/data/kanban.db` | SQLite DB file path (`KANBAN_DB_PATH`) |
| `config.dbUrl` | _empty_ | Postgres DSN (`KANBAN_DB_URL`) |
| `config.logLevel` | `info` | Log level (`KANBAN_LOG_LEVEL`) |
| `persistence.enabled` | `true` | Creates PVC and mounts `/data` |
| `persistence.accessMode` | `ReadWriteOnce` | PVC access mode |
| `persistence.size` | `1Gi` | PVC requested size |
| `persistence.storageClass` | _empty_ | Optional PVC storageClass |
| `remoteMCPServer.enabled` | `true` | Creates a `RemoteMCPServer` for kagent runtime discovery |
| `remoteMCPServer.protocol` | `STREAMABLE_HTTP` | Remote MCP transport protocol |
| `remoteMCPServer.timeout` | `30s` | Per-request timeout for remote MCP calls |
| `remoteMCPServer.sseReadTimeout` | `5m0s` | Read timeout used for SSE transport |
| `remoteMCPServer.terminateOnClose` | `true` | Whether to terminate stream session on close |
| `remoteMCPServer.allowedNamespaces.from` | `All` | Namespace policy for cross-namespace references (`All`, `Same`, `Selector`) |

### Use Postgres

Pass overrides:

```bash
helm upgrade --install kanban-mcp ./contrib/tools/kanban-mcp \
  -n kagent \
  --set config.dbType=postgres \
  --set config.dbUrl='postgres://user:pass@host:5432/kanban?sslmode=disable' \
  --set persistence.enabled=false \
  --wait --timeout 5m
```

## Notes

- The chart names resources using `<release>-kanban-mcp` by default.
- When `persistence.enabled=true`, the chart creates a PVC with the same computed name as the deployment/service.
- Environment variables are injected from `values.yaml` into the container (`KANBAN_*`).
- The chart also creates a `RemoteMCPServer` pointing at `http://<release>-kanban-mcp.<namespace>:<service.port>/mcp` so kagent can invoke kanban tools through runtime MCP.

## Troubleshooting

- **Build fails with Go version error**: ensure the Dockerfile builder image uses Go `1.25+` (the module requires `go >= 1.25.7`).
- **Pod keeps using old image in Kind**: rebuild image, run `kind load docker-image ...`, then redeploy with `helm upgrade --install`.
- **Release not ready**: inspect Helm state with `helm status kanban-mcp -n kagent` and `helm get all kanban-mcp -n kagent`.
