---
id: core-give-the-task-store-tests-json-rpc
fork: kagent
theme: test-flakes
category: test
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: file
depends_on: []
commit: 1caefd305
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `test(core): give the task store test's JSON-RPC calls a connection each` | `TestRuntimeTaskStoreThroughGRPC` failed now and then with `failed to read task` (giantswarm/kagent-upstream#673): its JSON-RPC client reused one bufconn connection, and bufconn cannot stop a read-deadline timer that already fired, so the past deadline net/http sets to end one keep-alive request's background read could land on the next request and cancel its context mid-handler. The client opens a connection per call; the two push callback waits are bounded by 30 s instead of the 5 s that equals PushWorker's idle poll | to file — prepared in the fork: branch `upstream/task-store-test-connection-per-call` |
