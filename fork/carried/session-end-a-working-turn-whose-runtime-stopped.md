---
id: session-end-a-working-turn-whose-runtime-stopped
fork: kagent
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(session): end a working turn whose runtime stopped saving events'
upstream:
  outcome: superseded
  refs: [kagent-dev/kagent#2939]
  exit: 'verify against #2939 and drop'
depends_on: []
verified: 2026-10-19
---
# fix(session): end a working turn whose runtime stopped saving events

## Change

`fix(session): end a working turn whose runtime stopped saving events`

## Reason

a turn's events reach its task only through the gateway run that dispatched or observes it, so a runtime lost after the turn's first event (a spot reclaim, a worker roll) or a controller restarted mid-turn left the task `TASK_STATE_WORKING` for good: the instance kept it as its active task, refused every later message as a conflict, and every client showed a turn that never ends. `SUBMITTED` had the dispatch grace period; `WORKING` had no bound. The controller's leader now sweeps working tasks that recorded no event for longer than `KAGENT_STALLED_TURN_TIMEOUT` (1h; `controller.stalledTurnTimeout`; 0 disables) and that no run of the gateway follows, and fails each with the interruption message, under the instance's quiesce lock taken without waiting. Docs: `docs/architecture/runtime-and-lifecycle.md`

## History

superseded structurally by kagent-dev/kagent#2939 at the re-pin, rebuilt as `KAGENT_STALLED_TURN_TIMEOUT` on the Session model in the candidate; it goes upstream with the lost-runtime family's issue
