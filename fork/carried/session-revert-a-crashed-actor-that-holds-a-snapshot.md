---
id: session-revert-a-crashed-actor-that-holds-a-snapshot
fork: kagent
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: design issue per family
depends_on: []
commit: 13bbf1f2b
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(session): revert a crashed Actor that holds a snapshot and resume it again`, `fix(session): revert a crashed Actor right after the turn its crash ended`, `test(session): the remaining session workflow fakes answer RevertCrashed` | a gVisor sandbox that died under its runtime right after a restore (Substrate's sandbox exit report marks the Actor `CRASHED` at once) ended the turn with "the conversation goes on from its last snapshot", yet the next send was refused `runtime lost: Actor … crashed and cannot be resumed; start a new conversation`: Substrate refuses to resume a crashed Actor (`FailedPrecondition`) and only `RevertActor` takes it back to `SUSPENDED`, which `AwaitRuntime` called only for a session holding no task. It now reverts every crashed Actor that holds an external snapshot (the last suspend, or the template's golden it started from) and resumes it once more; only a second crash, or a crashed Actor with nothing to return to, is the loss. `FailLostTurn` reverts such an Actor as it ends the turn its crash ended (`RevertCrashed`): Substrate's router otherwise asks again to resume the crashed Actor on the next send's dial for its whole 30 s parking budget before the send's own revert. `TestAwaitRuntime`, `TestRevertCrashed`. Docs: `docs/architecture/a2a-gateway.md`, `docs/architecture/runtime-and-lifecycle.md` | to file with the lost-runtime family: upstream has no `RuntimeLost` or `AwaitRuntime` to patch, the family goes up as one |
