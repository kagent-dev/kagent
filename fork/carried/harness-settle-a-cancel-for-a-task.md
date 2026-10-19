---
id: harness-settle-a-cancel-for-a-task
fork: kagent
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(harness): settle a cancel for a task the runtime holds nothing for as canceled'
upstream:
  outcome: drops-at-repin
  refs: [kagent-dev/kagent#2939]
  exit: verify and drop
depends_on: []
verified: 2026-10-19
---
# fix(harness): settle a cancel for a task the runtime holds nothing for as canceled

## Change

`fix(harness): settle a cancel for a task the runtime holds nothing for as canceled`

## Reason

a Stop pressed right after a turn starts reaches the runtime before its first event; the runtime then ends the dispatch stream with gRPC `INTERNAL` instead of a `CANCELED` update and no longer answers for the task, so the gateway took it for a dispatch the runtime never took and recorded `TASK_STATE_FAILED`, and the cancel answered `FAILED` (2 of 3 Stop runs in the agentlab, giantswarm/kagent-upstream#128). The gateway now marks the run before it forwards the cancel, and a stream that ends with an error while its task is being cancelled settles the task `TASK_STATE_CANCELED` through the terminal path (quiesce, snapshot, one event to every observer); a lost runtime still fails the task first

## History

superseded structurally by kagent-dev/kagent#2939 at the re-pin (`cancelTask` recovers a cancel through `GetCancelResult`); still carried as `ce687161` after the 2026-10-12 re-pin onto `92dd6caa`, to verify against this race on the new path before re-porting or dropping
