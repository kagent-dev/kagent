---
id: e2e-the-egress-test-waits-on-the-denied
fork: kagent
theme: test-flakes
category: test
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: file in the test batch
depends_on: []
commit: 2fe249df2
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `chore(e2e): the egress test waits on the denied redirect's verdict` | `TestSessionEgressDeniesUnconfiguredDestination/claude` flaked in the gVisor lane with `send A2A message: DeadlineExceeded` (giantswarm/kagent-upstream#263, beside #205): the denied redirect turn was sent with the blocking send, whose retry poll bounds each call at 30 s, and a harness that retries the denied model call before it fails the turn outlasted it. The test now streams that turn and waits for its terminal state under the fixture's bound, then reads the task; the assertions are unchanged | to file |
