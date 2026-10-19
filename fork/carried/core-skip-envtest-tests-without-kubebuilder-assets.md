---
id: core-skip-envtest-tests-without-kubebuilder-assets
fork: kagent
theme: test-flakes
category: test
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: file
depends_on: []
commit: 3712c0361
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `test(core): skip envtest tests without KUBEBUILDER_ASSETS` | `TestSandboxTemplateCatalog` (grpcserver) and `TestSandboxKRTInformerQueueAndRestartCleanup` (controller) start an envtest API server and failed a plain `go test` of their package on a fresh checkout; `make test` did not set the assets either, only the CI job did. Now `make test` sets `KUBEBUILDER_ASSETS` through `setup-envtest` like the CI job, and both tests skip with a message naming the variable when it is unset (giantswarm/kagent-upstream#232) | to file (no upstream issue or pull request) |
