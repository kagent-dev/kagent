---
id: translator-the-claude-compiler-caps-claude-codes-output
fork: kagent
theme: providers-vertex
category: bugfix
status: carried
commits:
  - 'fix(translator): the claude compiler caps Claude Code''s output at the ModelConfig''s maxTokens'
upstream:
  outcome: to-file
  refs: []
  exit: file
depends_on: []
verified: 2026-10-19
---
# fix(translator): the claude compiler caps Claude Code's output at the ModelConfig's maxTokens

## Change

`fix(translator): the claude compiler caps Claude Code's output at the ModelConfig's maxTokens`

## Reason

an installation's default Anthropic ModelConfig sets `anthropic.maxTokens` for the Go ADK runtime (a test installation: 32000), and the claude compiler rejected any Anthropic option beyond the base URL, so a coding agent on that ModelConfig stayed `Compatible=False`. The compiler passes `maxTokens` (Anthropic and Anthropic on Vertex AI) to Claude Code as `CLAUDE_CODE_MAX_OUTPUT_TOKENS`, which caps the output of every model request and is lowered to the model's own cap; a Harness cannot set that variable itself

## History

to file
