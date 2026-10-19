---
id: harness-send-the-placeholder-to-actor-jwt-hosts
fork: kagent
theme: credential-injection
category: feature
status: carried
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3018]
  exit: 'with #3018'
depends_on: [egress-the-actor-jwt-on-the-installations-providers, harness-forward-the-callers-credential-on-mcp-calls]
commit: 6fd246162
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `feat(harness): no caller credential to actor-JWT hosts` | a Session bound to muster through the Actor's own JWT (`KAGENT_SESSION_ACTOR_JWT_PROVIDERS`) has its calls to muster credentialed by the egress gateway, which replaces the Authorization header there; the Claude harness's forwarder still put the caller's bearer on them and held it for the turn. The gateway now names a turn's actor-JWT hosts (`x-kagent-actor-jwt-hosts`), the forwarder sends the placeholder bearer to a server on one of them and holds no caller credential, and bounds the turn by no bearer expiry, when every server that would take it is on one, so the person's bearer leaves the sandbox's MCP path. `TestCredentialForwarderSendsThePlaceholderToActorJWTHosts`, `TestProcessDriverBindsTheCallerCredentialPerTurn`, `TestProcessDriverIgnoresTheBearerOfAnActorJWTTurn`, `TestRuntimeDialerNamesTheActorJWTHosts`, `TestActorJWTHosts` | to file with kagent-dev/kagent#3018, whose forwarder it changes; `KAGENT_SESSION_ACTOR_JWT_PROVIDERS` is the line's own |
