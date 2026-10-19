---
id: fork-publish-through-the-orbs-stock-jobs
fork: kagent
theme: ci-publish-infra
category: gs-specific
status: carried
commits:
  - 'ci(fork): publish through the orb''s stock jobs with decoupled versions'
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
verified: 2026-10-19
---
# ci(fork): publish through the orb's stock jobs with decoupled versions

## Change

`ci(fork): publish through the orb's stock jobs with decoupled versions`

## Reason

the RFCs (giantswarm/rfc `semantic-versioning-of-upstream-software`, `semver-based-automatic-upgrades`) give a package of upstream software its own stable semver, `-rc.N` and gitsemver's dev shape, nothing else: the images are built by the orb's `build-image` (native per architecture, the Makefile's build arguments through `build-args`) and `push-to-registries` (merge, sign, attest, the upstream-pin annotation), the version is gitsemver's (`vX.Y.Z` tags → `X.Y.Z`, first `1.0.0`), the pipeline's own `version` and image jobs and the `-gs.N` scheme are gone, pull request branches build without pushing, and `mirror.sh` refuses an upstream tag the fork already holds at another commit ("What is published", "Release scheme")

## History

fork-only, never upstream — ours to keep
