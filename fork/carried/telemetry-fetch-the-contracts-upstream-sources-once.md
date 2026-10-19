---
id: telemetry-fetch-the-contracts-upstream-sources-once
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci(telemetry): fetch the contract''s upstream sources once at their pins'
upstream:
  outcome: to-file
  refs: []
  exit: file
depends_on: []
verified: 2026-10-19
---
# ci(telemetry): fetch the contract's upstream sources once at their pins

## Change

`ci(telemetry): fetch the contract's upstream sources once at their pins`

## Reason

the Telemetry Contract Check cloned three GitHub repositories on every Weaver run (the two registries the manifest depends on, the GenAI one re-cloning the core one through its own manifest, and the Weaver policy packages), eleven clones per `make semconv-verify`; one flaked clone turned the check red on a pull request's first run (giantswarm/kagent-upstream#192). The sources are pinned in `telemetry/versions.env` and fetched once by `make semconv-deps` into a git-ignored checkout the manifest and the check read from disk, cached in CI keyed on the pins; Weaver's schema URL overrides cannot avoid the clone in v0.26.1 or v0.27.0, whose loader clones a manifest dependency before consulting them

## History

to file — prepared in the fork: branch `upstream/telemetry-deps` (mirror `main` + the patch, DCO-signed, no fork references)
