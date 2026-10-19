---
id: docker-apk-upgrade-in-the-go-runtime-image
fork: kagent
theme: ci-publish-infra
category: bugfix
status: carried
commits:
  - 'fix(docker): apk upgrade in the Go runtime image'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#2951, kagent-dev/kagent#3159]
  exit: 're-check against #3159'
depends_on: []
verified: 2026-10-19
---
# fix(docker): apk upgrade in the Go runtime image

## Change

`fix(docker): apk upgrade in the Go runtime image`

## Reason

the same Alpine finding in the controller and Go ADK images (`go/Dockerfile`), which #2756 did not cover

## History

to file (upstream's `go/Dockerfile` still has no `apk upgrade`; kagent-dev/kagent#2951 pins packages in `ui/Dockerfile` only)
