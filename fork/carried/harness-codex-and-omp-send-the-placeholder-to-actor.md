---
id: harness-codex-and-omp-send-the-placeholder-to-actor
fork: kagent
theme: credential-injection
category: feature
status: carried
commits:
  - 'feat(harness): codex and omp send the placeholder to actor-JWT hosts'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3018]
  exit: 'codex half with #3018 / the actor-JWT providers; omp is the line''s own'
depends_on: [harness-send-the-placeholder-to-actor-jwt-hosts]
verified: 2026-10-19
---
# feat(harness): codex and omp send the placeholder to actor-JWT hosts

## Change

`feat(harness): codex and omp send the placeholder to actor-JWT hosts`

## Reason

the egress gateway replaces only an `Authorization` header a request already carries, and on codex and omp a workspace Session's MCP calls to muster carried none, so muster answered 401 and served the person on claude only. The codex adapter now rewrites `config.toml` before each turn's App Server starts, and the omp adapter `mcp.json` before each omp process and its tool bridge per request, with `Authorization: Bearer kagent-credential-injected` for a server on one of the turn's actor-JWT hosts (`x-kagent-actor-jwt-hosts`) that sets no `Authorization` of its own. `TestRenderConfigSendsThePlaceholderToActorJWTHosts`, `TestProcessDriverParksAndResolvesMCPApproval`, `TestMCPFileSendsThePlaceholderToActorJWTHosts`, `TestBridgeTransportSendsThePlaceholderDuringAnActorJWTTurn`, `TestRunPreparesForTheTurnsActorJWTHosts`, `TestActorJWTHosts`, `TestTakesActorJWT`

## History

the codex half to file with `feat(harness): no caller credential to actor-JWT hosts`, after the actor-JWT providers it serves (`upstream/session-egress-destinations-2`); the omp harness is the line's own
