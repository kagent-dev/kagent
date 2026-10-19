---
id: helm-keep-the-charts-kagent-api-remotemcpserver-out
fork: kagent
theme: chart-knobs
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#2971]
  exit: file in the chart batch
depends_on: []
commit: 0ec897f37
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(helm): keep the chart's kagent-api RemoteMCPServer out of tool discovery under trusted-proxy auth` | under `controller.auth.mode: trusted-proxy` the controller validates a bearer on every call, its own MCP endpoint included, and the discovery it runs on the chart's `kagent-api` RemoteMCPServer carries none: every listing answered 401 and the reconcile backed off forever (agentlab, 2026-10-05). The chart labels the object `kagent.dev/discovery=disabled` under trusted-proxy auth | to file: upstream's chart has the same gap under its trusted-proxy mode (kagent-dev/kagent#2971) |
