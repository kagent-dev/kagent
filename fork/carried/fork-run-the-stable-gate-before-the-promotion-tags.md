---
id: fork-run-the-stable-gate-before-the-promotion-tags
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci(fork): run the stable gate before the promotion tags'
upstream:
  outcome: none
  refs: []
  exit: keep as fork infra
depends_on: []
verified: 2026-10-20
---
# ci(fork): run the stable gate before the promotion tags

## Change

`ci(fork): run the stable gate before the promotion tags`

## Reason

a stable release runs on a stable Substrate. `make substrate-pin-check` (`scripts/fork/substrate-pin-check.sh`), the publish pipeline's first job (`check-substrate-pin` in `.circleci/config.yml`) and what every image build requires, refuses a `vX.Y.Z` tag whose chart or module pin is a prerelease, naming the version, before anything is published. The gate runs before the Auto Release workflow's stable promotion tags the candidate

## History

none: CI of the fork line, not upstream code; no upstream branch. Split out of `substrate-run-the-line-on-1-7-0-a-stable-tag` on 2026-10-20
