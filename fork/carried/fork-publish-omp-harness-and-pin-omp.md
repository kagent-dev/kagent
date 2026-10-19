---
id: fork-publish-omp-harness-and-pin-omp
fork: kagent
theme: harness-variety
category: gs-specific
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: omp moves to agent-platform as a BYO image
depends_on: [harness-omp-harness-a-byo-adapter-for]
commit: 62bbe668e
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): publish omp-harness and pin omp with Renovate` | a BYO Harness pins its image by digest; the line publishes omp-harness from CircleCI like the other five images — multi-arch, cosign-signed, attested — stamps `runtimeImages.ompHarness.digest` into the published chart (which renders nothing from it), records the digest in `builds.md` (a new last column), and moves the omp pin with Renovate, its checksums from the release's `SHA256SUMS.txt` through `scripts/fork/harness-pins.sh` and the `Harness CLI pins` workflow | fork-only, never upstream — ours to keep |
