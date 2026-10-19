---
id: fork-land-a-hand-rebased-re-pin-candidate
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
commit: 5da4bff05
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): land a hand-rebased re-pin candidate through the sync` | a re-pin a carried commit blocks is rebased by hand; the sync workflow's `candidate` input lands that `sync/**` branch after the same checks as its own rebase (`scripts/fork/take-candidate.sh`: on `upstream_ref`, a rebase and not a merge, the ledger accepted; then `ci-ok` and `scan-ok`) and the same lease on the consumed head, so no person force-pushes `giantswarm` under a ruleset bypass | fork-only: the re-pin of the fork's consumed branch onto its upstream |
