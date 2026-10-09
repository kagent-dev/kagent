| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `feat(helm): the Harness's context compaction from harness.compaction` | upstream kagent-dev/kagent#2790 (`7471879e`) puts context compaction on `Harness.spec.kagent.compaction`, not on the AgentTemplate, so the platform `Harness` the chart renders (`harness.create`) is the one place every admitted agent takes it from; `harness.compaction` (empty by default) renders under `spec.kagent` | fork-only (the chart's `Harness` template is the fork's) |
