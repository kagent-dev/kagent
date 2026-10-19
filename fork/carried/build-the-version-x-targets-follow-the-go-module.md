---
id: build-the-version-x-targets-follow-the-go-module
fork: kagent
theme: ci-publish-infra
category: bugfix
status: carried
commits:
  - 'fix(build): the version -X targets follow the Go module path'
upstream:
  outcome: to-file
  refs: []
  exit: prepare and file
depends_on: []
verified: 2026-10-19
---
# fix(build): the version -X targets follow the Go module path

## Change

`fix(build): the version -X targets follow the Go module path`

## Reason

the Makefile's `LDFLAGS` built the `-X` targets from `$(DOCKER_REPO)`, the image repository; the line publishes under `giantswarm/kagent`, so the targets missed `github.com/kagent-dev/kagent/go/core/internal/version` and the binaries carried no version, commit or Substrate version. The targets follow the module path from `go/go.mod`. The commit is the build half of the line's first publish commit, whose GitHub Actions publish to `ghcr.io` was superseded by `ci(fork): publish to gsoci from CircleCI` and dropped at the 2026-10-12 re-pin

## History

to file upstream (any build whose image repository differs from the module path)
