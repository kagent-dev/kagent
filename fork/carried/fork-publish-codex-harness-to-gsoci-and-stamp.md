---
id: fork-publish-codex-harness-to-gsoci-and-stamp
fork: kagent
theme: ci-publish-infra
category: gs-specific
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
commit: 5c5b129e9
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): publish codex-harness to gsoci and stamp its digest into the chart` | a codex Harness pins its runtime image by digest, and the line published four images without it; upstream builds the same Dockerfile in its own CI, the line now publishes it from CircleCI like the other four — multi-arch, cosign-signed, attested — stamps `runtimeImages.codexHarness.digest` into the published chart and records the digest in `builds.md` (a new last column) | fork-only, never upstream — ours to keep |
