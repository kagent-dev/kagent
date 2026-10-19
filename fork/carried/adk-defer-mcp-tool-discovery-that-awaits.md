---
id: adk-defer-mcp-tool-discovery-that-awaits
fork: kagent
theme: mcp-forwarder
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: file the branch
depends_on: []
commit: d6b399c2c
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(adk): defer MCP tool discovery that awaits the caller's credential` | an agent's golden boot runs the Go ADK to readiness with no person behind it, and the ADK listed the tools of every MCP server at startup to classify its MCP App tools. A server that takes the caller's token (`KAGENT_PROPAGATE_TOKEN`, the per-agent muster `RemoteMCPServer`) answered `401` to that unauthenticated list, and the egress access log of every golden boot read like an authentication outage. The ADK now leaves such a server alone at startup when the boot has no `Authorization` for it, logs the deferral at debug, and discovers the tools at the first turn with the caller's token, as the 401 fallback already did; a server with a static credential is still classified at startup (giantswarm/kagent-upstream#99) | to file |
