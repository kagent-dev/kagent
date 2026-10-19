---
id: e2e-a-paused-session-survives-its-workers-loss
fork: kagent
theme: test-flakes
category: test
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: with the upstream design
depends_on: []
commit: 8e6fc97e0
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `test(e2e): a paused session survives its worker's loss` | from Substrate 1.9 a paused Actor holds no worker, so the carried e2e that deleted a paused session's worker and expected `RuntimeLost` found the session answering; the e2e proves the resume on the isolated pool's replacement worker instead, and the `RuntimeLost` failure on a DataLoss resume stays with the workflow's unit tests | fork-only with the carried session lifecycle it proves |
