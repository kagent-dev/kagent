---
id: adk-name-the-person-of-the-turn
fork: kagent
theme: identity-authorizer
category: feature
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: file with identity headers; share path dropped
depends_on: [a2agateway-run-a-turn-a-share, runtime-name-the-agent-and-the-user]
commit: 442146502
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(adk): name the person of the turn as the request span's end user` | the request span took `enduser.id` from `x-user-id`, which names the owner of the session; since a turn a session share authorizes runs in the owner's session (the row "run a shared turn in the instance owner's session" above), the traces of a collaborator's turn named the owner while its model usage named the visitor. The span now takes the person of the turn from `x-kagent-user` and keeps `x-user-id` only for a turn whose caller the gateway did not resolve | to file with "name the agent and the user on every model call", which brings `x-kagent-user` upstream, and the shared-turn row |
