---
id: helm-a-configmap-of-the-charts-runtime-image
fork: kagent
theme: chart-knobs
category: feature
status: carried
commits:
  - 'feat(helm): a ConfigMap of the chart''s runtime image references for the Substrate image cache'
  - 'fix(helm): label the runtime images ConfigMap for helm-controller to watch'
upstream:
  outcome: to-file
  refs: []
  exit: after Substrate pinned images
depends_on: [giantswarm/substrate:atelet-reads-pinned-images-from-file-re]
verified: 2026-10-19
---
# feat(helm): a ConfigMap of the chart's runtime image references for the Substrate image cache

## Change

`feat(helm): a ConfigMap of the chart's runtime image references for the Substrate image cache` and `fix(helm): label the runtime images ConfigMap for helm-controller to watch`

## Reason

the pinned-image set of Substrate's image cache follows the chart's digests instead of a third copy in the meta chart; the label `reconcile.fluxcd.io/watch: Enabled` (helm-controller's default `--watch-configs-label-selector`) makes the substrate HelmRelease that reads the ConfigMap through `valuesFrom` follow a Harness-image-only change at once instead of on its 10-minute interval

## History

to file — prepared in the fork as one commit: branch `upstream/helm-runtime-images-configmap`; queued behind the Substrate `pinnedImages` flag it feeds
