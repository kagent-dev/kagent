---
id: ci-pin-giantswarm-architect-orb-10-10-0
fork: kagent
theme: version-pins
category: pin
status: carried
commits:
  - 'ci: pin giantswarm/architect orb 10.10.0'
upstream:
  outcome: fork-only
  refs: []
  exit: orb release
depends_on: []
verified: 2026-10-19
---
# ci: pin giantswarm/architect orb 10.10.0

## Change

`ci: pin giantswarm/architect orb 10.10.0`

## Reason

the hand-written CircleCI config pins the orb release that carries giantswarm/architect-orb#952 and #956; the line has `gen.ci.generate: false`, so align-files does not carry the orb version here

## History

fork-only
