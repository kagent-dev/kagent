---
id: helm-the-harnesss-context-compaction-from-harness-compaction
fork: kagent
theme: chart-knobs
category: gs-specific
status: carried
commits:
  - 'feat(helm): the Harness''s context compaction from harness.compaction'
upstream:
  outcome: fork-only
  refs: []
  exit: with harnesses[] move
depends_on: []
verified: 2026-10-19
---
# feat(helm): the Harness's context compaction from harness.compaction

## Change

`feat(helm): the Harness's context compaction from harness.compaction`

## Reason

upstream kagent-dev/kagent#2790 (`7471879e`) puts context compaction on `Harness.spec.kagent.compaction`, not on the AgentTemplate, so the platform `Harness` the chart renders (`harness.create`) is the one place every admitted agent takes it from; `harness.compaction` (empty by default) renders under `spec.kagent`

## History

fork-only (the chart's `Harness` template is the fork's)
