---
id: fork-the-consumed-branch-is-giantswarm
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci(fork): the consumed branch is giantswarm'
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
verified: 2026-10-19
---
# ci(fork): the consumed branch is giantswarm

## Change

`ci(fork): the consumed branch is giantswarm`

## Reason

the consumed branch was renamed from `poc/agent-platform` to `giantswarm` (GitHub's rename retargets the default branch, the ruleset and the open pull requests), and every reference followed: the push and pull-request triggers of `ci.yaml` and the scan, the sync's `CONSUMED`, the CircleCI filters, this file

## History

fork-only, never upstream
