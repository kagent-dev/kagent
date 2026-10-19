---
id: helm-a-harnesses-entry-runs-a-byo-harness
fork: kagent
theme: harness-variety
category: gs-specific
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: move harnesses[] rendering to agent-platform
depends_on: [helm-harnesses-renders-further-harnesses-beside-the-charts]
commit: 9e77c5c9d
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `feat(helm): a harnesses[] entry runs a BYO Harness on its own image` | the platform offers oh-my-pi (omp) as a coding Harness an installation opts into with values, and omp is a BYO Harness (kagent has no omp runtime; `omp-harness` is this line's adapter image); the chart's `harnesses[]` failed the render on `runtime: byo`. An entry renders `spec.byo: {}` on the image and the `command` the entry names (both required: the Harness API's CEL rule wants `workload.command` on a BYO Harness), the image by digest, and its `displayName` as the `ui.giantswarm.io/display-name` annotation the Dev Portal titles the Harness by (required on a BYO entry, since the image alone does not say what runs; optional on any other); the runtime-images ConfigMap lists that image through the entry as it does every entry's. The chart defaults no image for a BYO entry: an installation pins the digest it runs | fork-only: extends the fork's `harnesses[]` (row `feat(helm): harnesses renders further Harnesses beside the chart's own`), which upstream declined |
