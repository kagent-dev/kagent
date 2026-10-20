---
id: session-revert-a-crashed-actor-whose-session-holds-no-task
fork: kagent
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(session): revert a crashed Actor whose session holds no task and resume it again'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3255]
  exit: read against #3255, then the design issue per family
depends_on: []
verified: 2026-10-20
---
# fix(session): revert a crashed Actor whose session holds no task and resume it again

## Change

`fix(session): revert a crashed Actor whose session holds no task and resume it again`

## Reason

a conversation's first send resumes its Actor from the template's golden snapshot; a gVisor restore whose runtime never came up (the wakeup probe on `:8081` refused for 30 s, the restored runtime silent) crashed the Actor, and since a crashed Actor without a snapshot was a lost runtime, the brand-new conversation was failed `RuntimeLost` ("start a new conversation") before it had taken a turn (the e2e flake `TestSessionTaskPersistenceAndReconnect/claude`, `runtime lost: Actor … crashed and cannot be resumed`). A session that holds no task gave its runtime nothing to keep, so `RuntimeLost` no longer reports it lost, and `AwaitRuntime` reverts its crashed Actor (Substrate `RevertActor`) to the snapshot it started from and resumes it once more; only a second crash is the loss, and the revert is logged with Substrate's crash message. `TestAwaitRuntime`, `TestRuntimeLost`, `TestHasSessionTasks`. Docs: `docs/architecture/a2a-gateway.md`, `docs/architecture/runtime-and-lifecycle.md`

## History

to file with the lost-runtime family: upstream has no `RuntimeLost` or `AwaitRuntime` to patch, the family goes up as one

2026-10-20: [kagent-dev/kagent#3255](https://github.com/kagent-dev/kagent/pull/3255) (adalundhe, opened 2026-10-16) recovers accepted turns whose Actor crashed and adds `SessionService.RevertSession` on the same session workflow paths; this change is read against it before the family's design issue, the outcome stays to-file.
