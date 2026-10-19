---
id: helm-the-harnesss-context-compaction-from-harness-compaction
fork: kagent
theme: chart-knobs
category: gs-specific
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: with harnesses[] move
depends_on: []
commit: 3113061f8
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `feat(helm): the Harness's context compaction from harness.compaction` | upstream kagent-dev/kagent#2790 (`7471879e`) puts context compaction on `Harness.spec.kagent.compaction`, not on the AgentTemplate, so the platform `Harness` the chart renders (`harness.create`) is the one place every admitted agent takes it from; `harness.compaction` (empty by default) renders under `spec.kagent` | fork-only (the chart's `Harness` template is the fork's) |
