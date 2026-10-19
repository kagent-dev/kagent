---
id: ci-fork-the-ledger-cites-public-repositories-only
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
commit: 6622d52d1
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): the ledger cites public repositories only` | this repository is public, and `FORK.md` and `fork/carried/` had cited issues of a private repository more than once after they were removed, so a reader could not follow the citation. The `public-links` job of `fork-ledger.yaml` (`scripts/fork/public-links.sh`) looks up every giantswarm repository a pull request's added ledger lines cite and fails the pull request when one is not public | fork-only, never upstream |
