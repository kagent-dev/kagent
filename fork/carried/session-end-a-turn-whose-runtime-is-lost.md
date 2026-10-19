---
id: session-end-a-turn-whose-runtime-is-lost
fork: kagent
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(session): end a turn whose runtime is lost after it took the input'
upstream:
  outcome: to-file
  refs: []
  exit: design issue per family
depends_on: []
verified: 2026-10-19
---
# fix(session): end a turn whose runtime is lost after it took the input

## Change

`fix(session): end a turn whose runtime is lost after it took the input`

## Reason

a turn that a runtime took before its worker went away, so that its stream broke without a boundary, hung the caller's send in the gateway's wait for the turn's boundary until the caller's own deadline, and the session kept the turn as active work until the stalled-turn sweep an hour later; and the idle worker's claim on the session's last boundary, whose Pause Substrate refused on the crashed Actor, outlived the session's failure, so the failed session's delete was refused for as long as the claim stood (giantswarm/kagent-upstream#204: `TestSessionRuntimeLostFailsTheSession` flaked in the gVisor e2e lane on whichever of the two the probe send met, 81 s or 305 s instead of 39 s). The gateway asks whether the runtime is lost before waiting on a turn the runtime took, ends the turn `FAILED` with the loss and answers the send with it at once; `FailSession` releases the session's idle work, claimed or not; the e2e bounds each probe send and reports the last answer

## History

to file with the lost-runtime family
