---
id: substrate-run-the-line-on-1-11-1
fork: kagent
theme: version-pins
category: pin
status: carried
commits:
  - 'chore(substrate): run the line on 1.11.1 for the atelet terminate fix'
upstream:
  outcome: fork-only
  refs: []
  exit: squash into the current pin
depends_on: [giantswarm/substrate:atelet-terminates-actor-whose-restore-failed]
verified: 2026-10-19
---
# chore(substrate): run the line on 1.11.1 for the atelet terminate fix

## Change

`chore(substrate): run the line on 1.11.1 for the atelet terminate fix`

## Reason

the Substrate line's `1.11.1` carries atelet's terminate of an Actor whose sandbox died during `runsc restore` (giantswarm/substrate#436): on `1.9.0` every later terminate failed with `failed to read sandbox record`, so a crashed Actor could be neither reverted nor deleted and the Session's revert-and-resume (giantswarm/kagent-upstream#580) stalls. The `go/go.mod` replace target and the Makefile's `SUBSTRATE_VERSION` move to it together; the ate-api contract is unchanged from `1.9.0` (v0.5.0-alpha2), the controller sets no `MintActorJWTRequest` field and writes no egress policy through `kubectl-ate`

## History

fork-only: the pin is the line's own
