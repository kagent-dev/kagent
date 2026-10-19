---
id: fork-the-e2e-lane-installs-the-lines-substrate
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
commit: 95e17dbf2
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): the e2e lane installs the line's Substrate through kagent install` | upstream's e2e lane installs Substrate and its database through `kagent install`, which replaced the line's hand-written Substrate install; the line's settings move onto that path: the Substrate of the Makefile's pin from gsoci (`KAGENT_SUBSTRATE_HELM_REPO`/`_VERSION`, not the CLI's upstream default), the `ate-golden` credential policy, the egress upstream CA as a `--set-file` upgrade, plain `--set` values (the CLI splits its extra arguments on `--set`), and the NFS storage after the install | fork-only, never upstream |
