---
id: cli-install-on-the-substrate-line-without-podcert
fork: kagent
theme: chart-shape
category: gs-specific
status: carried
upstream:
  outcome: fork-only
  refs: []
  exit: Substrate chart shape converges with upstream (T15)
depends_on: [giantswarm/substrate:chart-keeps-lines-shape-on-v0-5-0-alpha2-base]
commit: ecb2b54f3
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(cli): install on the Substrate line without substrate-podcert` | upstream `kagent install` installs a standalone `substrate-podcert` chart and a bundled PostgreSQL that authenticates with `postgres.podcert.ate.dev` certificates. The Giant Swarm Substrate line keeps the pod certificate controller in its `substrate` chart (servicedns and podidentity signers only) and publishes no `substrate-podcert` chart, which would collide with the substrate release in Helm. The CLI skips that release, hands the namespace to the substrate release and requires `--skip-database-setup`; `scripts/fork/dev-postgres.sh` gives the e2e lane and `make kagent-cli-deploy` a development database | fork-only: follows the Substrate line's chart layout; falls away if the line ships the controller as upstream does |
