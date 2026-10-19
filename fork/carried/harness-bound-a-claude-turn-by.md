---
id: harness-bound-a-claude-turn-by
fork: kagent
theme: mcp-forwarder
category: feature
status: carried
commits:
  - 'feat(harness): bound a Claude turn by a time limit and the bearer''s exp'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3018]
  exit: 'after #3018'
depends_on: [api-withdraw-the-harness-turn-limits, harness-forward-the-callers-credential-on-mcp-calls]
verified: 2026-10-19
---
# feat(harness): bound a Claude turn by a time limit and the bearer's exp

## Change

`feat(harness): bound a Claude turn by a time limit and the bearer's exp`

## Reason

a coding agent a person invokes runs on the person's forwarded bearer (`harness-forward-the-callers-credential-on-mcp-calls.md`): a turn that outlived it kept calling MCP servers that answered 401, which the agent cannot recover from, and nothing bounded how long a turn ran. The Claude driver now ends every run of Claude between two inputs of the person at `KAGENT_TURN_TIMEOUT` from the Harness environment and 30 seconds before the forwarded JWT's `exp`, read without verification, a run Claude keeps alive after its result for a backgrounded tool included; the turn completes with a cap result, its message on the final status and the bound under `kagent.dev/a2a/stop-reason`, and the person continues it with the next message and a fresh bearer

## History

to file: it bounds the bearer kagent-dev/kagent#3018 forwards and writes the cap result beside the turn usage of the withdrawn limits commits, so it is filed on top of #3018 once that lands
