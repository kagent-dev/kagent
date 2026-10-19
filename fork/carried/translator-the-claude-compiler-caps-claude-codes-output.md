---
id: translator-the-claude-compiler-caps-claude-codes-output
fork: kagent
theme: providers-vertex
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: file
depends_on: []
commit: 6306e73cd
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(translator): the claude compiler caps Claude Code's output at the ModelConfig's maxTokens` | an installation's default Anthropic ModelConfig sets `anthropic.maxTokens` for the Go ADK runtime (a test installation: 32000), and the claude compiler rejected any Anthropic option beyond the base URL, so a coding agent on that ModelConfig stayed `Compatible=False`. The compiler passes `maxTokens` (Anthropic and Anthropic on Vertex AI) to Claude Code as `CLAUDE_CODE_MAX_OUTPUT_TOKENS`, which caps the output of every model request and is lowered to the model's own cap; a Harness cannot set that variable itself | to file |
