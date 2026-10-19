---
id: harness-the-codex-turns-usage-on-its-final
fork: kagent
theme: harness-variety
category: feature
status: carried
commits:
  - 'feat(harness): the codex turn''s usage on its final task status'
upstream:
  outcome: fork-only
  refs: [kagent-dev/kagent#2461]
  exit: 're-check against #2461'
depends_on: [api-withdraw-the-harness-turn-limits]
verified: 2026-10-19
---
# feat(harness): the codex turn's usage on its final task status

## Change

`feat(harness): the codex turn's usage on its final task status`

## Reason

a turn on the codex Harness ended with no usage on its final task status, so a client that reads a turn's tokens from `kagent.dev/a2a/usage` (as it does for the claude Harness) showed nothing for codex (giantswarm/kagent-upstream#608). The codex driver now sums each `thread/tokenUsage/updated` of the turn — its own thread and its subagent threads, a replayed notification once, across an input-required pause — onto the outcome of a completed, failed or interrupted turn. Codex reports tokens, never a price: the usage's cost is optional, so a codex turn carries prompt, cached and output tokens and no `costUsd` (nor `numTurns`); a claude turn's carries both as before

## History

fork-only: it extends the turn usage that `api-withdraw-the-harness-turn-limits.md` carries, which upstream `main` does not have (upstream reports a codex turn's tokens per model call, on its chat spans). The re-pin that drops those commits drops this one with them
