---
id: e2e-prove-the-paused-runtime-ttl-the-lost
fork: kagent
theme: test-flakes
category: test
status: carried
upstream:
  outcome: fork-only
  refs: [kagent-dev/kagent#3018]
  exit: with T1 upstream
depends_on: [api-withdraw-the-harness-turn-limits, session-end-a-turn-whose-runtime-is-lost, session-suspend-a-runtime-paused-for-input-once]
commit: ad03bb0d1
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `test(e2e): prove the paused-runtime TTL, the lost runtime, the Claude turn limit and the caller credential` | the e2e suite on the gVisor matrix proves four carried behaviours the unit tests could only fake: the durable suspend of a pause older than `KAGENT_PAUSED_RUNTIME_TTL` and the resume from its snapshot, the `RuntimeLost` failure of a paused session whose worker pod is deleted (on an isolated one-worker WorkerPool), a completed Claude turn's usage on its final status (the turn-limit case it began with left with the limits, `fork/carried/api-withdraw-the-harness-turn-limits.md`), and the caller's bearer on an MCP call from the Claude harness under `KAGENT_PROPAGATE_TOKEN` | fork-only until the behaviours are upstream; the credential case belongs with kagent-dev/kagent#3018 |
