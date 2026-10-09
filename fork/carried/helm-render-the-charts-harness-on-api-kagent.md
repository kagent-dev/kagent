| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(helm): render the chart's Harness on api.kagent.dev/v1alpha3 without the admission selector` | upstream kagent-dev/kagent#2952 moved the API to `api.kagent.dev/v1alpha3` and replaced the admission labels by an explicit `Agent` whose `harnessRef` names its Harness, so the chart's Harness (`harness.create`) renders on the new group with no `selector`, and the meta chart's override of the selector key becomes a no-op | fork-only |
