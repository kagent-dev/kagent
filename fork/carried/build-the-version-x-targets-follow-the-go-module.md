---
id: build-the-version-x-targets-follow-the-go-module
fork: kagent
theme: ci-publish-infra
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: prepare and file
depends_on: []
commit: 0f29d8fe1
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(build): the version -X targets follow the Go module path` | the Makefile's `LDFLAGS` built the `-X` targets from `$(DOCKER_REPO)`, the image repository; the line publishes under `giantswarm/kagent`, so the targets missed `github.com/kagent-dev/kagent/go/core/internal/version` and the binaries carried no version, commit or Substrate version. The targets follow the module path from `go/go.mod`. The commit is the build half of the line's first publish commit, whose GitHub Actions publish to `ghcr.io` was superseded by `ci(fork): publish to gsoci from CircleCI` and dropped at the 2026-10-12 re-pin | to file upstream (any build whose image repository differs from the module path) |
