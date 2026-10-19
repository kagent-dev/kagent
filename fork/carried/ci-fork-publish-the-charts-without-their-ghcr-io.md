---
id: ci-fork-publish-the-charts-without-their-ghcr-io
fork: kagent
theme: ci-publish-infra
category: gs-specific
status: carried
commits:
  - 'ci(fork): publish the charts without their ghcr.io dependencies'
upstream:
  outcome: fork-only
  refs: [kagent-dev/kagent#3223]
  exit: 're-check after #3223 re-pin'
depends_on: []
verified: 2026-10-19
---
# ci(fork): publish the charts without their ghcr.io dependencies

## Change

`ci(fork): publish the charts without their ghcr.io dependencies`

## Reason

gsoci is the only registry the platform pulls from: upstream's chart templates declare kmcp, kmcp-crds and kagent-tools from ghcr.io, which the platform does not run, so the published charts drop those dependencies and switch their conditions off, and `helm dependency build` of a published chart reaches no ghcr.io

## History

fork-only
