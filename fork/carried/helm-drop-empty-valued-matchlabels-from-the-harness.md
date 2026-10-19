---
id: helm-drop-empty-valued-matchlabels-from-the-harness
fork: kagent
theme: chart-knobs
category: bugfix
status: carried
upstream:
  outcome: drops-at-repin
  refs: [kagent-dev/kagent#2952]
  exit: drop the dead commit
depends_on: []
commit: ebb2eb9ec
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(helm): drop empty-valued matchLabels from the Harness admission selector` | the platform Harness admits templates by `agent-platform.giantswarm.io/harness` alone, so the meta chart has to remove the chart's default selector key `kagent.dev/harness`; the null that deletes a key on coalesce does not survive the JSON merge patch of a kagent HelmRelease that pre-existed the cut-over (a test installation, 2026-09-13: the selector kept both labels and no AgentTemplate was admitted — giantswarm/agent-platform#418). The template now drops every `matchLabels` entry whose value is the empty string, which every values path stores, and fails the render when nothing is left to select by (an empty selector would admit every template) | superseded upstream: kagent-dev/kagent#2952 removed the Harness admission selector, so the patch drops at the re-pin (still carried as `ebb2eb9e` after the 2026-10-12 re-pin onto `92dd6caa`); the selector itself left the chart with `fix(helm): render the chart's Harness on api.kagent.dev/v1alpha3 without the admission selector` below; the commit stays in the stack |
