---
id: fork-fire-the-e2e-go-test-timeout
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
commit: 5e4a4df47
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `ci(fork): fire the e2e go test timeout before the job's` | the hosted runners spend up to 16 minutes on the setup before the e2e tests start, so `go test -timeout=30m` could fire only after the job's 40-minute `timeout-minutes` had cancelled it, and a hung test left no goroutine dump (giantswarm/kagent-upstream#388). `go test` stops at 20 minutes (a green run takes up to 15) and the job at 45 | fork-only: upstream's Blacksmith runners finish the setup in time for its 30/40 pair |
