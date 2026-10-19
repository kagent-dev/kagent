---
id: e2e-the-egress-test-waits-on-the-denied
fork: kagent
theme: test-flakes
category: test
status: carried
commits:
  - 'chore(e2e): the egress test waits on the denied redirect''s verdict'
upstream:
  outcome: to-file
  refs: []
  exit: file in the test batch
depends_on: []
verified: 2026-10-19
---
# chore(e2e): the egress test waits on the denied redirect's verdict

## Change

`chore(e2e): the egress test waits on the denied redirect's verdict`

## Reason

`TestSessionEgressDeniesUnconfiguredDestination/claude` flaked in the gVisor lane with `send A2A message: DeadlineExceeded` (giantswarm/kagent-upstream#263, beside #205): the denied redirect turn was sent with the blocking send, whose retry poll bounds each call at 30 s, and a harness that retries the denied model call before it fails the turn outlasted it. The test now streams that turn and waits for its terminal state under the fixture's bound, then reads the task; the assertions are unchanged

## History

to file
