---
id: fork-stamp-the-golang-adk-digest-and-substrate
fork: kagent
theme: chart-knobs
category: gs-specific
status: carried
upstream:
  outcome: fork-only
  refs: [kagent-dev/kagent#3151]
  exit: 're-check against #3151 at re-pin'
depends_on: []
commit: 3d68ff1e9
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): stamp the golang-adk digest and SUBSTRATE_VERSION into the chart at publish` | the published chart carries the index digests of its own runtime images and the Substrate worker image of the Makefile's pin, so the platform's meta chart stops re-pinning by hand what the line already knows | fork-only |
