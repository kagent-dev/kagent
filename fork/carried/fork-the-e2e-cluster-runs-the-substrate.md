---
id: fork-the-e2e-cluster-runs-the-substrate
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci(fork): the e2e cluster runs the Substrate the charts depend on'
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
verified: 2026-10-19
---
# ci(fork): the e2e cluster runs the Substrate the charts depend on

## Change

`ci(fork): the e2e cluster runs the Substrate the charts depend on`

## Reason

the Makefile's `SUBSTRATE_VERSION` and `SUBSTRATE_REPO` are the one Substrate pin: a `substrate-pin` target prints them, the e2e job reads them and installs the line's `substrate-crds` and `substrate` charts and its gVisor worker image, where the job had exported upstream's fixed version, which also drove `make helm-version` and made the registry look for a chart the line never published

## History

fork-only, never upstream
