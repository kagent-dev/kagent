---
id: chore-declare-renovate-off-on-the-fork-line
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
commit: 6483b00ae
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `chore: declare Renovate off on the fork line` | upstream owns the dependency graph: a bump opened on the line would diverge from the pin and every re-pin would have to carry or revert it, so the consumed branch carries a hand-written `renovate.json` that runs only the managers the line owns, with the reason, and dependencies move when the line is re-pinned; a real GitHub fork is skipped by Renovate until its `renovate.json` sets `forkProcessing: "enabled"`, so that file both admits the line and keeps it to its own managers | fork-only, never upstream |
