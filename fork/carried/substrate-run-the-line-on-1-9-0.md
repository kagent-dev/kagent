---
id: substrate-run-the-line-on-1-9-0
fork: kagent
theme: version-pins
category: pin
status: carried
commits:
  - 'chore(substrate): run the line on 1.9.0, the re-pin onto v0.5.0-alpha2'
upstream:
  outcome: fork-only
  refs: []
  exit: squash into the current pin
depends_on: []
verified: 2026-10-19
---
# chore(substrate): run the line on 1.9.0, the re-pin onto v0.5.0-alpha2

## Change

`chore(substrate): run the line on 1.9.0, the re-pin onto v0.5.0-alpha2`

## Reason

upstream `main@92dd6ca` builds against kagent-dev/substrate `v0.5.0-alpha2` (`pkg/postgressetup`, the ateom/actor identity split); the Substrate line's `1.9.0` is that release plus the line's patches, so the `go/go.mod` replace target and the charts' Substrate dependency move to it together. The line's chart version is set ahead of `LDFLAGS`, where upstream derives it from a kagent-dev replace target the line does not have

## History

fork-only: the pin is the line's own
