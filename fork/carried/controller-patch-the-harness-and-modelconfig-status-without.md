---
id: controller-patch-the-harness-and-modelconfig-status-without
fork: kagent
theme: golden-boot
category: bugfix
status: carried
commits:
  - 'fix(controller): patch the Harness and ModelConfig status without a resourceVersion'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3258]
  exit: file with the Ready row
depends_on: [controller-write-the-harness-ready-condition]
verified: 2026-10-19
---
# fix(controller): patch the Harness and ModelConfig status without a resourceVersion

## Change

`fix(controller): patch the Harness and ModelConfig status without a resourceVersion`

## Reason

the controller wrote `Harness.status` and `ModelConfig.status` with `UpdateStatus` on a copy of its informer cache, so any write to the object the cache had not observed yet (a Helm upgrade applying the chart's Harness, the controller's own previous status write) failed the next status write with `the object has been modified` and sent it back to the queue; a production installation logged it once on the Harness right after the 1.4.0 rollout. Both status writes are a JSON merge patch of the status subresource that carries no resourceVersion and only the fields the controller derives: `observedGeneration` and `conditions` on the Harness (never `status.capabilities`), `observedGeneration`, `secretHash` and `conditions` on the ModelConfig

## History

to file: the ModelConfig half on its own (upstream `main` has the same `UpdateStatus`), the Harness half with branch `upstream/harness-ready-condition`
