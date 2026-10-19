---
id: telemetry-count-a-turn-by-its-segment
fork: kagent
theme: identity-authorizer
category: bugfix
status: carried
commits:
  - 'fix(telemetry): count a turn by its segment, not by its invoke_agent spans'
upstream:
  outcome: to-file
  refs: []
  exit: file
depends_on: []
verified: 2026-10-19
---
# fix(telemetry): count a turn by its segment, not by its invoke_agent spans

## Change

`fix(telemetry): count a turn by its segment, not by its invoke_agent spans`

## Reason

Claude Code 2.1.302 marks its `claude_code.interaction` span with `gen_ai.operation.name=invoke_agent` (2.1.285 emits the span without it), beneath the `invoke_agent` span kagent opens for the harness, so the tracing e2e saw two invoke_agent spans and the Claude Code bump could not land; the contract now states that a native agent may mark its own span `invoke_agent` under kagent's and that turns are counted by `kagent.invocation.segment`, and the test selects the invocation that way and requires Claude Code's span under it

## History

to file — prepared in the fork: branch `upstream/telemetry-count-turns-by-segment`
