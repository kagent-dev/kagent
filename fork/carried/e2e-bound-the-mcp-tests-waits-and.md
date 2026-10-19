---
id: e2e-bound-the-mcp-tests-waits-and
fork: kagent
theme: test-flakes
category: test
status: carried
commits:
  - 'chore(e2e): bound the MCP tests'' waits and name the step they wait on'
upstream:
  outcome: to-file
  refs: []
  exit: file in the test batch
depends_on: []
verified: 2026-10-19
---
# chore(e2e): bound the MCP tests' waits and name the step they wait on

## Change

`chore(e2e): bound the MCP tests' waits and name the step they wait on`

## Reason

`TestMCPCheckpointFork/claude` hung in the gVisor lane until the job's 40-minute timeout cancelled it, with no goroutine dump and no message (giantswarm/kagent-upstream#388): the claude runtime, restored from its golden snapshot, opened the turn and never called the model, and `mcpCall` posted the synchronous `invoke_session` with no deadline, so `mcpInvoke`'s 30 s poll never got to check its window. Every MCP step now runs under a two-minute deadline through `waitForStep`, which fails naming the step, the state it waited for and the last state seen; a synchronous invocation that times out adds the Session's runtime state and its tasks' states. Its unit test shows the messages

## History

to file — prepared in the fork: branch `upstream/e2e-mcp-bounded-waits` (on `upstream/e2e-rollout-and-mcp-refusal-waits`, whose `mcpInvoke` refusal retry it bounds; mirror `main` @ `bf8afa56` below it; DCO-signed, no fork references; its message is the pull-request text). kagent-dev/kagent searched 2026-10-11: nothing on unbounded MCP calls in the e2e tests
