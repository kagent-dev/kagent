---
id: fork-pull-docker-hub-images-through-mirror-gcr-io
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci(fork): pull Docker Hub images through mirror.gcr.io'
upstream:
  outcome: fork-only
  refs: [giantswarm/kagent-upstream#339]
  exit: keep
depends_on: []
verified: 2026-10-19
---
# ci(fork): pull Docker Hub images through mirror.gcr.io

## Change

`ci(fork): pull Docker Hub images through mirror.gcr.io`

## Reason

GitHub-hosted runners share IPs that exhaust Docker Hub's unauthenticated pull limit, failing the build, scan and e2e jobs before or during `make build`; the service containers, QEMU and BuildKit images name `mirror.gcr.io`, BuildKit resolves `docker.io` base images through it, and `test-e2e` pulls Kind's registry and node images from it under their Docker Hub names (the runner's daemon ignores `registry-mirrors`), `go-unit-tests` prefixes testcontainers' Docker Hub images with it, and the telemetry contract check installs the pinned Weaver release instead of pulling its image. Upstream builds on Blacksmith runners

## History

fork-only, never upstream (giantswarm/kagent-upstream#339)
