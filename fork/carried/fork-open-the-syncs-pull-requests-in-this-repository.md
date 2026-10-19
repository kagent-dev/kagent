---
id: fork-open-the-syncs-pull-requests-in-this-repository
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
commit: eacc3b028
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): open the sync's pull requests in this repository, not the parent` | in a checkout of a fork, `gh pr create` resolves the base repository to the parent unless `--repo` is given, and the App is not installed there: the sync's second run failed at its "checks red" step with `Resource not accessible by integration (createPullRequest)`; both calls name this repository, and `git-push-as.sh` masks its credential line only under GitHub Actions, where a manual re-pin had printed it to the terminal | fork-only, never upstream |
