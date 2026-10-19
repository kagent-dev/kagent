---
id: ci-fork-publish-the-charts-without-their-ghcr-io
fork: kagent
theme: ci-publish-infra
category: gs-specific
status: carried
upstream:
  outcome: fork-only
  refs: [kagent-dev/kagent#3223]
  exit: 're-check after #3223 re-pin'
depends_on: []
commit: bd00cc928
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): publish the charts without their ghcr.io dependencies` | gsoci is the only registry the platform pulls from: upstream's chart templates declare kmcp, kmcp-crds and kagent-tools from ghcr.io, which the platform does not run, so the published charts drop those dependencies and switch their conditions off, and `helm dependency build` of a published chart reaches no ghcr.io | fork-only |
