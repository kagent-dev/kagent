---
id: build-retry-go-mod-download-on-a-transient-proxy
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
upstream:
  outcome: none
  refs: []
  exit: keep as fork infra
depends_on: []
commit: 7fecd371a
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(build): retry go mod download on a transient proxy error` | the `build (claude-harness)` job failed on one dropped stream from proxy.golang.org (`stream error: stream ID 1; INTERNAL_ERROR; received from peer`, then `go mod download` did not complete) in a pull request that did not touch the image (giantswarm/kagent-upstream#448, giantswarm/kagent-upstream#449). The module download step of `go/Dockerfile` (the controller and the other Go binaries), `go/harness/claude` and `go/harness/codex` runs in the same loop as the Alpine step: three attempts, 5 s apart; a missing module still fails after the third with Go's error and exit code. The image content is unchanged | none — CI of the fork line (the image builds of this repository's pipeline), not upstream code; no upstream branch |
