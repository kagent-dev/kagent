| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): the consumed branch is giantswarm` | the consumed branch was renamed from `poc/agent-platform` to `giantswarm` (GitHub's rename retargets the default branch, the ruleset and the open pull requests), and every reference followed: the push and pull-request triggers of `ci.yaml` and the scan, the sync's `CONSUMED`, the CircleCI filters, this file | fork-only, never upstream |
