---
id: fork-publish-to-gsoci-from-circleci
fork: kagent
theme: ci-publish-infra
category: gs-specific
status: carried
commits:
  - 'ci(fork): publish to gsoci from CircleCI'
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
verified: 2026-10-19
---
# ci(fork): publish to gsoci from CircleCI

## Change

`ci(fork): publish to gsoci from CircleCI`

## Reason

the org publishes to `gsoci.azurecr.io` from CircleCI only — the architect orb's multi-arch builds, cosign keyless signatures under the project's CircleCI identity, SBOM attestations — and never from a GitHub Actions job holding a registry credential. `.circleci/config.yml` ("What is published") replaces `tag.yaml`, which pushed to ghcr.io; the stamping and the ledger row moved with it

## History

fork-only, never upstream — ours to keep
