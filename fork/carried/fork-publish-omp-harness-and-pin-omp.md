---
id: fork-publish-omp-harness-and-pin-omp
fork: kagent
theme: harness-variety
category: gs-specific
status: carried
commits:
  - 'ci(fork): publish omp-harness and pin omp with Renovate'
upstream:
  outcome: fork-only
  refs: []
  exit: omp moves to agent-platform as a BYO image
depends_on: [harness-omp-harness-a-byo-adapter-for]
verified: 2026-10-19
---
# ci(fork): publish omp-harness and pin omp with Renovate

## Change

`ci(fork): publish omp-harness and pin omp with Renovate`

## Reason

a BYO Harness pins its image by digest; the line publishes omp-harness from CircleCI like the other five images — multi-arch, cosign-signed, attested — stamps `runtimeImages.ompHarness.digest` into the published chart (which renders nothing from it), records the digest in `builds.md` (a new last column), and moves the omp pin with Renovate, its checksums from the release's `SHA256SUMS.txt` through `scripts/fork/harness-pins.sh` and the `Harness CLI pins` workflow

## History

fork-only, never upstream — ours to keep
