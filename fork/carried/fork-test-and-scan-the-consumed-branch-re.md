---
id: fork-test-and-scan-the-consumed-branch-re
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci(fork): test and scan the consumed branch, re-pin weekly by workflow, keep a ledger'
  - 'ci(fork): the re-pin checks every carried hunk against the candidate'
upstream:
  outcome: fork-only
  refs: []
  exit: 'keep; unblock #709'
depends_on: []
verified: 2026-10-19
---
# ci(fork): test and scan the consumed branch, re-pin weekly by workflow, keep a ledger

## Change

`ci(fork): test and scan the consumed branch, re-pin weekly by workflow, keep a ledger`

## Reason

this document, `ci.yaml`/`image-scan.yaml` on the consumed branch, `sync-upstream.yaml`, the ledger

## History

fork-only
