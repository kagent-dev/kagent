---
id: e2e-follow-up-mocks-match-the-calls-output
fork: kagent
theme: test-flakes
category: test
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: file on mockllm
depends_on: []
commit: bb6acd5ee
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `test(e2e): Claude and chat-completions follow-up mocks match the call's output` | mockllm compares a Claude `tool_result` by its `tool_use_id` alone, and several chat-completions follow-ups matched a marker the call's own command echoes, which the Go ADK's bash tool quotes back when the command does not run: a tool call the runtime rejected still completed its turn, the blind spot that hid why the Codex sandbox turn ran no git. The e2e mock server answers a Claude `tool_result` mock only for a result that is not an error and contains the mock's `content`, every Claude tool-result mock names the output it expects, and every marker a follow-up matches is one only the command's output prints. `TestToolCallMocksRefuseARejectedCall` sends each mock file's rejections (Claude's `tool_use_error`, the Go ADK's unknown tool and failed command) and requires that none is answered | to file — the Claude match belongs in kagent-dev/mockllm's Anthropic matcher, which takes a `tool_result`'s content as it takes a text block's; the markers follow with it |
