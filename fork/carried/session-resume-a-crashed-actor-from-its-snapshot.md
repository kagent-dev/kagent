---
id: session-resume-a-crashed-actor-from-its-snapshot
fork: kagent
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: design issue per family
depends_on: [giantswarm/substrate#322, session-resume-again-while-substrate-has-no-free-worker]
commit: 3bb941cbd
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(session): resume a crashed Actor from its snapshot` | a conversation whose Actor Substrate reported `CRASHED` was failed for good (`FAILED`, reason `RuntimeLost`, "start a new conversation") although the Actor's last durable snapshot was intact: stepping Substrate under a worker drain on a test cluster (every worker pod deleted every 8 s for a minute), the resumed conversation's suspend failed while its worker was torn down (`suspend failed: atelet Checkpoint: … ateom.sock: connect: no such file or directory (worker unreachable while tearing down the failed actor)`), the Actor went `CRASHED`, and the next turn's refused send marked the session lost (giantswarm/kagent-upstream#420; the Substrate half, returning such an Actor to its snapshot, is giantswarm/substrate#322). `RuntimeLost` now reports a crashed Actor lost only without an external snapshot; one with a snapshot is resumed by the gateway's wait after the refused send (`AwaitRuntime`, whose wait already takes the `Unavailable` Substrate answers while it returns the Actor to the snapshot), and a resume Substrate refuses for good (`FailedPrecondition` on an Actor still crashed: a crash on a live worker, which Substrate does not recover from) records the loss there, so a session is never failed for a crash Substrate recovers from and still fails for one it does not. Docs: `docs/architecture/a2a-gateway.md`, `docs/architecture/runtime-and-lifecycle.md` | to file with the lost-runtime family: upstream has no `RuntimeLost` or `AwaitRuntime` to patch, the family goes up as one; builds on the carried `session-resume-again-while-substrate-has-no-free-worker` change |
