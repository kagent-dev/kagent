---
id: ui-read-the-agent-list-once
fork: kagent
theme: test-flakes
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: file
depends_on: []
commit: c3116381c
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(ui): read the Agent list once, not once per namespace` | the UI sent one `ListAgents` per namespace from every component showing Agents, so a Dashboard load plus one Refresh on an installation with 35 namespaces sent about 70 requests in a burst (giantswarm/kagent-upstream#552). `ListAgents` accepts an empty namespace (every namespace the caller's scope allows, as `ListSandboxTemplates`), the UI reads it once under one key every caller shares, a Refresh sends one request, and the settling poll runs about once an interval however many callers are mounted | branch `upstream/ui-read-the-agent-list-once` (the same change on upstream main, without the settling poll, which upstream main does not have) |
