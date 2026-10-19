---
id: session-leave-an-expired-pause-on-a-lost
fork: kagent
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: drops-at-repin
  refs: [kagent-dev/kagent#2939]
  exit: drop the commit
depends_on: []
commit: 86739af98
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(session): leave an expired pause on a lost node alone, and delete an Actor that cannot be suspended as it is` | the pause TTL's first sweep on a test installation (2026-09-15 13:46Z) tried to suspend three paused Actors whose checkpoint nodes were gone: Substrate fails the upload at once (`UploadPausedCheckpoint: while getting atelet conn for node …`) and leaves the Actor `SUSPENDING`, which the lost-runtime patch's `Delete` then suspends first — and fails the same way — so sessions that were deletable while `PAUSED` were not any more. `Idle` counts a `PAUSED` Actor only while a worker runs on its checkpoint's node (`ListWorkers`), so the sweep leaves a pause on a lost node to the node-loss handling (the Substrate line's companion patch); `Delete` falls back to an any-state delete when the pre-delete suspend fails | superseded structurally with the row above (kagent-dev/kagent#2939); drops at the re-pin (still carried as `86739af9` after the 2026-10-12 re-pin onto `92dd6caa`) |
