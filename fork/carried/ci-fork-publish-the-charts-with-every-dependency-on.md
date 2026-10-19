---
id: ci-fork-publish-the-charts-with-every-dependency-on
fork: kagent
theme: ci-publish-infra
category: gs-specific
status: carried
commits:
  - 'ci(fork): publish the charts with every dependency on gsoci'
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
verified: 2026-10-19
---
# ci(fork): publish the charts with every dependency on gsoci

## Change

`ci(fork): publish the charts with every dependency on gsoci`

## Reason

a chart pulled from gsoci builds its dependencies as it is, from gsoci alone: grafana-mcp, a `file://` path in the source, is published there and oauth2-proxy mirrored there, and the published chart depends on both from there

## History

fork-only
