---
id: session-revert-a-crashed-actor-whose-session-holds-no-task
fork: kagent
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: design issue per family
depends_on: []
commit: 4969ca9a3
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(session): revert a crashed Actor whose session holds no task and resume it again` | a conversation's first send resumes its Actor from the template's golden snapshot; a gVisor restore whose runtime never came up (the wakeup probe on `:8081` refused for 30 s, the restored runtime silent) crashed the Actor, and since a crashed Actor without a snapshot was a lost runtime, the brand-new conversation was failed `RuntimeLost` ("start a new conversation") before it had taken a turn (the e2e flake `TestSessionTaskPersistenceAndReconnect/claude`, `runtime lost: Actor … crashed and cannot be resumed`). A session that holds no task gave its runtime nothing to keep, so `RuntimeLost` no longer reports it lost, and `AwaitRuntime` reverts its crashed Actor (Substrate `RevertActor`) to the snapshot it started from and resumes it once more; only a second crash is the loss, and the revert is logged with Substrate's crash message. `TestAwaitRuntime`, `TestRuntimeLost`, `TestHasSessionTasks`. Docs: `docs/architecture/a2a-gateway.md`, `docs/architecture/runtime-and-lifecycle.md` | to file with the lost-runtime family: upstream has no `RuntimeLost` or `AwaitRuntime` to patch, the family goes up as one |
