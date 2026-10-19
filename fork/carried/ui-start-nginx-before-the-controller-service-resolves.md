---
id: ui-start-nginx-before-the-controller-service-resolves
fork: kagent
theme: chart-knobs
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: file
depends_on: []
commit: 2f5f39a4d
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(ui): start nginx before the controller Service resolves` | the UI's nginx named the controller in a static `upstream` block, which nginx resolves once at start and refuses to start without, so a UI pod that starts before cluster DNS or the controller Service answers (after a node restart) crash-loops until they do (giantswarm/kagent-upstream#142); `/api/`, `/a2a/`, `/mcp` and `/.well-known/jwks.json` now proxy through a variable resolved per request by a resolver `init.sh` renders from the pod's `resolv.conf`, and a request before the controller answers gets a 502 | to file — prepared in the fork: branch `upstream/ui-nginx-resolver` (mirror `main` + the patch, DCO-signed, no fork references) |
