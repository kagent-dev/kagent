---
id: translator-the-claude-compiler-accepts-a-modelconfig
fork: kagent
theme: providers-vertex
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: file
depends_on: []
commit: b4be9a04e
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(translator): the claude compiler accepts a ModelConfig with prompt caching on` | the platform's default ModelConfig turns on `anthropic.promptCaching` with the 5m `cacheTTL` for the Go ADK runtime (kagent-dev/kagent#2788), and the claude compiler rejected it, so a coding agent needed a second ModelConfig. Claude Code caches prompts on its own, so the compiler accepts promptCaching with the default `cacheTTL` on Anthropic and Bedrock, and one ModelConfig serves both runtimes | to file |
