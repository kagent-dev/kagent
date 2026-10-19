---
id: fork-commit-the-vendors-checksums-to-renovates
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci(fork): commit the vendors'' checksums to Renovate''s harness CLI bumps'
  - 'ci(fork): the harness-pins push sends only the App''s credential'
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
verified: 2026-10-19
---
# ci(fork): commit the vendors' checksums to Renovate's harness CLI bumps

## Change

`ci(fork): commit the vendors' checksums to Renovate's harness CLI bumps` and `ci(fork): the harness-pins push sends only the App's credential`

## Reason

every Claude Code and Codex release is a pull request and a release of the line; Renovate moves the version but not the per-architecture checksums the images verify, so its pull requests stayed red until someone ran `scripts/fork/harness-pins.sh` by hand. The `Harness CLI pins` workflow commits them to Renovate's branch as the `heraldbot` App, so the pull request carries the complete bump and its checks run on it

## History

fork-only, never upstream
