---
id: fork-publish-to-gsoci-from-circleci
fork: kagent
theme: ci-publish-infra
category: gs-specific
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
commit: d0760c331
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): publish to gsoci from CircleCI` | the org publishes to `gsoci.azurecr.io` from CircleCI only — the architect orb's multi-arch builds, cosign keyless signatures under the project's CircleCI identity, SBOM attestations — and never from a GitHub Actions job holding a registry credential. `.circleci/config.yml` ("What is published") replaces `tag.yaml`, which pushed to ghcr.io; the stamping and the ledger row moved with it | fork-only, never upstream — ours to keep |
