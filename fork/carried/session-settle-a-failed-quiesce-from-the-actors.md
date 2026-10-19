---
id: session-settle-a-failed-quiesce-from-the-actors
fork: kagent
theme: durable-pause
category: bugfix
status: carried
commits:
  - 'fix(session): settle a failed quiesce from the Actor''s state'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3010]
  exit: 'file against #3010'
depends_on: []
verified: 2026-10-19
---
# fix(session): settle a failed quiesce from the Actor's state

## Change

`fix(session): settle a failed quiesce from the Actor's state`

## Reason

the idle lifecycle worker claims a turn's boundary and then pauses or suspends the Actor; when that call returned an error, for example a `SuspendActor` that outlived its two-minute deadline because the sandbox teardown hung (giantswarm/substrate#124), the worker only logged `runtime boundary outcome unknown` and kept the claim, so the session refused every later send and lifecycle operation until its row was edited with SQL (giantswarm/kagent-upstream#166). The worker now reads the Actor again with a backoff (1 s, doubling to a minute) while it is `SUSPENDING`, `PAUSING` or Substrate cannot answer; a `SUSPENDED` Actor's external snapshot finishes the claim, a `PAUSED` Actor finishes a pause, and any other state releases the claim without a snapshot (`ReleaseSessionQuiescence`), so the next send is admitted and a lost runtime fails the session there. Docs: `docs/architecture/runtime-and-lifecycle.md`

## History

prepared branch `upstream/session-settle-failed-quiesce` for kagent-dev/kagent#3010 (its case B, reached through a failed quiesce)
