---
id: session-fail-a-session-whose-resume-finds
fork: kagent
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(session): fail a session whose resume finds its snapshot lost'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#2952]
  exit: design issue per family
depends_on: []
verified: 2026-10-19
---
# fix(session): fail a session whose resume finds its snapshot lost

## Change

`fix(session): fail a session whose resume finds its snapshot lost`

## Reason

a resume that Substrate answers with `DataLoss` (the Actor's snapshot or the golden one it builds on is gone from the store) can never succeed, yet the workflow left the claimed `RESUME` operation pending as if its outcome were uncertain: every later resume and every delete was refused as an unfinished lifecycle operation, and only recreating the database cleared the instance (giantswarm/kagent-upstream#151). The claiming executor now records the known failure: the session goes `FAILED` with the reason `RuntimeLost` and the Substrate message, the operation is cleared, the resume RPC answers `FailedPrecondition`, and a delete removes the Actor and tombstones the session as for any other. Any other resume error stays pending as before

## History

to file: upstream's session workflow (kagent-dev/kagent#2952) left a claimed resume pending on the same answer; the change is ported onto that workflow since the 2026-10-05 re-pin and is filed from there
