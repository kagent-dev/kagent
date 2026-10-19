---
id: harness-count-the-forwarders-refusals-per-agent
fork: kagent
theme: mcp-forwarder
category: gs-specific
status: carried
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3018]
  exit: 'with #3018'
depends_on: [harness-forward-the-callers-credential-on-mcp-calls]
commit: 57afe7eac
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `feat(harness): count the forwarder's refusals per Agent` | the platform alerts when something in a coding agent's sandbox other than Claude Code probes the caller-credential forwarder: the forwarder counts its own 401 (no token), 404 (a server it does not front, OAuth discovery under `/.well-known/` excepted) and 400 (a refused path) answers, every turn boundary reports them on its status message (`kagent.dev/a2a/forwarder-refusals`), and the task store counts each published boundary once per Agent in `kagent.harness.forwarder.refusals` on the controller's scraped endpoint, since a sandbox's own metrics reach no Prometheus with the Agent's labels. `TestCredentialForwarderCountsItsOwnRefusalsOnce`, `TestExecuteReportsTheForwarderRefusalsOfATurn`, `TestSettleTaskCountsTheForwarderRefusalsOfAPublishedBoundaryOnce`, `TestSettleSessionTaskPublishedReturnsEachBoundaryOnce`, `TestForwarderRefusalsSurviveTheWire` | to file with kagent-dev/kagent#3018, whose forwarder it counts |
