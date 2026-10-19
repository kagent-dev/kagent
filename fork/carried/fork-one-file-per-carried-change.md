---
id: fork-one-file-per-carried-change
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci(fork): one file per carried change under fork/carried, checked in CI'
  - 'ci(fork): name the ledger check fork-ledger and record this change'
  - 'docs(fork): the header is the ledger''s single source, commits named by subject'
upstream:
  outcome: fork-only
  refs: [giantswarm/kagent-upstream#324]
  exit: keep; add structured header (plan A1)
depends_on: []
verified: 2026-10-19
---
# ci(fork): one file per carried change under fork/carried, checked in CI

## Change

`ci(fork): one file per carried change under fork/carried, checked in CI`, `ci(fork): name the ledger check fork-ledger and record this change` and `docs(fork): the header is the ledger's single source, commits named by subject`

## Reason

every carried pull request appended its row to the end of FORK.md's table, so the first one to merge turned every other open one DIRTY on GitHub (#318 → #322 → #323, #313 → #317): the rows sat on the same line, and GitHub's test merge and rebase merge ignore local merge drivers. One file per change under `fork/carried/` never shares a line with another; `scripts/fork/carried.sh` prints the table in the order the commits sit on top of the pin, and its `--check`, the `fork-ledger` workflow on every pull request and push, keeps every file one row keyed to commits on top of the pin, each recorded once

## History

fork-only, never upstream (giantswarm/kagent-upstream#324)
