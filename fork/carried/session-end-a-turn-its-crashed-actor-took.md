---
id: session-end-a-turn-its-crashed-actor-took
fork: kagent
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(session): end a crashed Actor''s turn, also one back at its snapshot'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3255]
  exit: read against #3255, then the design issue per family
depends_on: []
verified: 2026-10-20
---
# fix(session): end a crashed Actor's turn, also one back at its snapshot

## Change

`fix(session): end a crashed Actor's turn, also one back at its snapshot`

## Reason

a turn in flight when its worker goes away (a drain, a replaced pod) crashes the Session's Actor; once a crashed Actor that holds a durable snapshot was no longer a lost runtime, the gateway kept that turn as active work, the restored runtime never finished it, and the conversation refused every send until the stalled-turn sweep (`KAGENT_STALLED_TURN_TIMEOUT`, one hour) ended it. `FailLostTurn` now ends the turn `FAILED` with the crash's message whenever the Actor crashed (`TurnCrash`, which asks again every second for up to 15 s while Substrate still reports the Actor `RUNNING`); it records `RuntimeLost` on the session only when the runtime is lost, and the gateway answers the send with the ended turn. `TestRuntimeLost`, `TestTurnCrash`, `TestFailLostTurn`, `TestGatewayFailsTheTurnWhenRuntimeIsLostAfterTakingTheInput`

## History

to file with the lost-runtime family: upstream has no `RuntimeLost` or `FailLostTurn` to patch, the family goes up as one

2026-10-20: [kagent-dev/kagent#3255](https://github.com/kagent-dev/kagent/pull/3255) (adalundhe, opened 2026-10-16) recovers accepted turns whose Actor crashed and adds `SessionService.RevertSession` on the same session workflow paths; this change is read against it before the family's design issue, the outcome stays to-file.
