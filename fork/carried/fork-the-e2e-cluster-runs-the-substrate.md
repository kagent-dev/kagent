---
id: fork-the-e2e-cluster-runs-the-substrate
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
commit: 2c14e0233
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): the e2e cluster runs the Substrate the charts depend on` | the Makefile's `SUBSTRATE_VERSION` and `SUBSTRATE_REPO` are the one Substrate pin: a `substrate-pin` target prints them, the e2e job reads them and installs the line's `substrate-crds` and `substrate` charts and its gVisor worker image, where the job had exported upstream's fixed version, which also drove `make helm-version` and made the registry look for a chart the line never published | fork-only, never upstream |
