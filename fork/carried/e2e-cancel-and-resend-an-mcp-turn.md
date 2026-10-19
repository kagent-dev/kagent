---
id: e2e-cancel-and-resend-an-mcp-turn
fork: kagent
theme: test-flakes
category: test
status: carried
commits:
  - 'chore(e2e): cancel and resend an MCP turn lost in working once'
upstream:
  outcome: to-file
  refs: []
  exit: masks a lost model response in the gVisor lane; fix the cause instead
depends_on: [e2e-name-the-sessions-actor-when-a-send]
verified: 2026-10-19
---
# chore(e2e): cancel and resend an MCP turn lost in working once

## Change

`chore(e2e): cancel and resend an MCP turn lost in working once`

## Reason

`TestMCPSessionInteraction/byo-adk` waited two minutes for a task stuck in `working` in the gVisor lane (giantswarm/kagent-upstream#641): the runtime's model request got the mock's 200 at the egress gateway within 35 ms, but the response never reached the runtime inside the sandbox, so the turn waited on the model client's 30-minute timeout with its Actor idle. A task-capable invocation now gives its task one minute to leave `working`, then cancels it, waits for the cancellation and invokes the turn once more; only a task last seen `working` is resent. A lost turn is logged, and every failed MCP task wait reports, with the Session's, its tasks' and its Actor's state. Its unit test shows which outcomes are resent

## History

to file with `chore(e2e): name the Session's Actor when a send stays refused`, whose Actor report it uses
