---
id: fork-publish-codex-harness-to-gsoci-and-stamp
fork: kagent
theme: ci-publish-infra
category: gs-specific
status: carried
commits:
  - 'ci(fork): publish codex-harness to gsoci and stamp its digest into the chart'
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
verified: 2026-10-19
---
# ci(fork): publish codex-harness to gsoci and stamp its digest into the chart

## Change

`ci(fork): publish codex-harness to gsoci and stamp its digest into the chart`

## Reason

a codex Harness pins its runtime image by digest, and the line published four images without it; upstream builds the same Dockerfile in its own CI, the line now publishes it from CircleCI like the other four — multi-arch, cosign-signed, attested — stamps `runtimeImages.codexHarness.digest` into the published chart and records the digest in `builds.md` (a new last column)

## History

fork-only, never upstream — ours to keep
