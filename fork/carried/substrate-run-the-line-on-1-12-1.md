---
id: substrate-run-the-line-on-1-12-1
fork: kagent
theme: version-pins
category: pin
status: carried
commits:
  - 'chore(substrate): run the line on the stable 1.12.1'
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: [giantswarm/substrate:ateom-reports-workload-container-exit]
verified: 2026-10-19
---
# chore(substrate): run the line on the stable 1.12.1

## Change

`chore(substrate): run the line on the stable 1.12.1`

## Reason

the Substrate line's `1.12.1` reports an application container that exits while its gVisor sandbox keeps running (giantswarm/substrate#464): on `1.11.1` the pause container kept the sandbox up, the Actor stayed `RUNNING` with nothing behind it, the turn's task stayed `working` with no settled boundary, so `DeleteSession` was refused as unsettled and a scheduled execution never ended (giantswarm/kagent-upstream#649). The `go/go.mod` replace target and the Makefile's `SUBSTRATE_VERSION` move to it together; the step changes no persisted format, and the ate-api proto only adds RPCs

## History

fork-only: the pin is the line's own
