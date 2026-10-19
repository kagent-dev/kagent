---
id: session-resume-again-while-substrate-has-no-free-worker
fork: kagent
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(session): resume again while Substrate has no free worker'
upstream:
  outcome: to-file
  refs: []
  exit: design issue per family
depends_on: [a2a-wait-for-an-in-flight-actor-operation]
verified: 2026-10-19
---
# fix(session): resume again while Substrate has no free worker

## Change

`fix(session): resume again while Substrate has no free worker`

## Reason

a `ResumeSession` issued while Substrate's worker pool rolled (a Substrate upgrade draining every worker on a test installation) failed at once: ate-api answered the one `ResumeActor` with `ResourceExhausted: no free workers available`, kagent mapped it to `Unavailable` "Failed to resume Session" and returned, so the Actor stayed `SUSPENDED` with its snapshot intact and the person saw a failed session that only had to wait seconds for a worker (giantswarm/kagent-upstream#412). Within its two-minute attempt, the resume is now issued again while ate-api answers `ResourceExhausted` (no free worker), `Unavailable` (a departing worker or a restarting ate-api cut the resume short; the Actor is back at its snapshot) or `Aborted` (another operation holds the Actor), from 500 ms doubling to 5 s, the answers Substrate's own router parks a routed request on; the answer at the attempt's end is returned as before, for the next request to continue the operation, and any other answer ends the attempt at once. The A2A gateway's wait after a refused send (`a2a-wait-for-an-in-flight-actor-operation`) takes the same three answers instead of `Aborted` alone. Docs: `docs/architecture/runtime-and-lifecycle.md`, `docs/architecture/a2a-gateway.md`

## History

to file
