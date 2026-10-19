---
id: ci-fork-publish-the-charts-with-every-dependency-on
fork: kagent
theme: ci-publish-infra
category: gs-specific
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
commit: f7b675121
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): publish the charts with every dependency on gsoci` | a chart pulled from gsoci builds its dependencies as it is, from gsoci alone: grafana-mcp, a `file://` path in the source, is published there and oauth2-proxy mirrored there, and the published chart depends on both from there | fork-only |
