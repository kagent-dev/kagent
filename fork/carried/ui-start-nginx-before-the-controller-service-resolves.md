---
id: ui-start-nginx-before-the-controller-service-resolves
fork: kagent
theme: chart-knobs
category: bugfix
status: carried
commits:
  - 'fix(ui): start nginx before the controller Service resolves'
  - 'fix(ui): proxy the JWKS location through the resolved controller'
upstream:
  outcome: to-file
  refs: []
  exit: file
depends_on: []
verified: 2026-10-19
---
# fix(ui): start nginx before the controller Service resolves

## Change

`fix(ui): start nginx before the controller Service resolves`

## Reason

the UI's nginx named the controller in a static `upstream` block, which nginx resolves once at start and refuses to start without, so a UI pod that starts before cluster DNS or the controller Service answers (after a node restart) crash-loops until they do (giantswarm/kagent-upstream#142); `/api/`, `/a2a/`, `/mcp` and `/.well-known/jwks.json` now proxy through a variable resolved per request by a resolver `init.sh` renders from the pod's `resolv.conf`, and a request before the controller answers gets a 502

## History

to file — prepared in the fork: branch `upstream/ui-nginx-resolver` (mirror `main` + the patch, DCO-signed, no fork references)
