---
id: fork-stamp-the-golang-adk-digest-and-substrate
fork: kagent
theme: chart-knobs
category: gs-specific
status: carried
commits:
  - 'ci(fork): stamp the golang-adk digest and SUBSTRATE_VERSION into the chart at publish'
upstream:
  outcome: fork-only
  refs: [kagent-dev/kagent#3151]
  exit: 're-check against #3151 at re-pin'
depends_on: []
verified: 2026-10-19
---
# ci(fork): stamp the golang-adk digest and SUBSTRATE_VERSION into the chart at publish

## Change

`ci(fork): stamp the golang-adk digest and SUBSTRATE_VERSION into the chart at publish`

## Reason

the published chart carries the index digests of its own runtime images and the Substrate worker image of the Makefile's pin, so the platform's meta chart stops re-pinning by hand what the line already knows

## History

fork-only
