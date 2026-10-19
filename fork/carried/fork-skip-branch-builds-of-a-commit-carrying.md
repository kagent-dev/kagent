---
id: fork-skip-branch-builds-of-a-commit-carrying
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci(fork): skip branch builds of a commit carrying a release tag'
upstream:
  outcome: fork-only
  refs: [giantswarm/kagent-upstream#310]
  exit: orb release
depends_on: []
verified: 2026-10-19
---
# ci(fork): skip branch builds of a commit carrying a release tag

## Change

`ci(fork): skip branch builds of a commit carrying a release tag`

## Reason

the Auto Release workflow tags a merge's head right after the push; a branch job that checks out after the tag landed hit the architect orb's release-version guard and failed into the same `ci/circleci: <job>` status context the tag pipeline reports into, so the release candidate's combined status read `failure` and `devctl release promote` refused it. A pre-step on every job ends such a branch job green before checkout; the tag pipeline publishes the version

## History

fork-only, never upstream (giantswarm/kagent-upstream#310)
