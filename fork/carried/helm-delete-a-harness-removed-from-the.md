---
id: helm-delete-a-harness-removed-from-the
fork: kagent
theme: chart-knobs
category: gs-specific
status: carried
commits:
  - 'fix(helm): delete a Harness removed from the values on upgrade'
upstream:
  outcome: fork-only
  refs: []
  exit: with harnesses[] move
depends_on: []
verified: 2026-10-19
---
# fix(helm): delete a Harness removed from the values on upgrade

## Change

`fix(helm): delete a Harness removed from the values on upgrade`

## Reason

the chart's Harness (`harness.create`) and every `harnesses[]` entry carried `helm.sh/resource-policy: keep`, so an entry removed from the values (an overlay that added a toolchain Harness and drops it again) left its Harness in the cluster to be deleted by hand; a Harness the values declare now carries no keep policy and goes on the next upgrade, the CRDs keep theirs (giantswarm/kagent-upstream#365)

## History

fork-only (the chart's `Harness` templates are the fork's)
