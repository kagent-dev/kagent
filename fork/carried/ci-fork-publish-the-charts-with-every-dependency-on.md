| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): publish the charts with every dependency on gsoci` | a chart pulled from gsoci builds its dependencies as it is, from gsoci alone: grafana-mcp, a `file://` path in the source, is published there and oauth2-proxy mirrored there, and the published chart depends on both from there | fork-only |
