---
id: fork-mirror-as-the-app-and-cancel-the-runs
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci(fork): mirror as the App and cancel the runs upstream''s files start'
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
verified: 2026-10-19
---
# ci(fork): mirror as the App and cancel the runs upstream's files start

## Change

`ci(fork): mirror as the App and cancel the runs upstream's files start`

## Reason

the sync's first run was refused at the mirror: a workflow's own token may not update a workflow file, not even in a mirrored upstream commit, whatever `permissions:` grants, so the mirror is pushed with the App's token like the candidate and the consumed branch; an App push starts upstream's workflow files at the mirrored refs (runners the fork does not have, registries it must not touch), so `mirror.sh` records what it pushed and `mirror-quiet.sh` cancels those runs right after, and the ledger stays on `GITHUB_TOKEN`

## History

fork-only, never upstream
