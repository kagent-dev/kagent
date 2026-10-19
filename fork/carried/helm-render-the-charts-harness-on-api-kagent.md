---
id: helm-render-the-charts-harness-on-api-kagent
fork: kagent
theme: chart-knobs
category: gs-specific
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: keep until upstream chart renders one
depends_on: []
commit: e62a1dba3
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(helm): render the chart's Harness on api.kagent.dev/v1alpha3 without the admission selector` | upstream kagent-dev/kagent#2952 moved the API to `api.kagent.dev/v1alpha3` and replaced the admission labels by an explicit `Agent` whose `harnessRef` names its Harness, so the chart's Harness (`harness.create`) renders on the new group with no `selector`, and the meta chart's override of the selector key becomes a no-op | fork-only |
