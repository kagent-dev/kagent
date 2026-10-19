---
id: build-retry-the-alpine-package-step-on-a
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'fix(build): retry the Alpine package step on a transient mirror error'
upstream:
  outcome: open
  refs: [kagent-dev/kagent#3167]
  exit: '#3167 merges'
depends_on: []
verified: 2026-10-19
---
# fix(build): retry the Alpine package step on a transient mirror error

## Change

`fix(build): retry the Alpine package step on a transient mirror error`

## Reason

an image build failed on one transient error from the Alpine mirror (`TLS: unspecified error` fetching the APKINDEX, then `unable to select packages`, in the `build (golang-adk, golang-adk)` job of a pull request that did not touch the image, giantswarm/kagent-upstream#280), turning its required checks red. Every apk step (`go/Dockerfile`, `go/harness/claude`, `go/harness/codex`, `go/sandbox/guest`, both stages of `ui/Dockerfile`) runs in one shared loop: three attempts, 5 s apart, each the whole upgrade and install; a missing package still fails after the third with apk's error and exit code. The image content is unchanged

## History

kagent-dev/kagent#3167, a draft pending the team's review before it is proposed (prepared as `upstream/apk-retry`); fork-only: the GitHub and GitLab CLI pins inside the loops of `go/Dockerfile` and `go/harness/claude`, and the same loop around `apk add gettext` in `.circleci/config.yml`'s `push-charts` job (upstream has no CircleCI pipeline)
