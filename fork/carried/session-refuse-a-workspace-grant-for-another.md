---
id: session-refuse-a-workspace-grant-for-another
fork: kagent
theme: workspace-volumes
category: bugfix
status: carried
commits:
  - 'fix(session): refuse a workspace grant for another Agent namespace'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3155, kagent-dev/kagent#3156]
  exit: with row 209
depends_on: [session-configurable-admission-verifying-a-signed]
verified: 2026-10-19
---
# fix(session): refuse a workspace grant for another Agent namespace

## Change

`fix(session): refuse a workspace grant for another Agent namespace`

## Reason

the jwt Session admission verified a grant's signature, issuer, audience, expiry, subject, volume and mounts but never compared it with the Session's Agent, so a valid grant for one namespace's workspace admitted a Session on an Agent in any namespace, which then mounted the workspace's volume and was compiled with the grant's providers. The grant carries `agent_namespace` (signed by the platform's workspace issuer on every grant), `session.VolumeAdmission.Admit` receives the request's Agent reference, and the admission refuses `PermissionDenied` "admission grant is for another namespace" when the claim differs from the Agent's namespace or is missing. A request without a volume source and admission token is unchanged. `TestAdmitsOnlyTheGrantsAgentNamespace`, `TestSessionAdmittedByAGrantOnTheWire`

## History

to file — prepared in the fork: folded into the upstream-ready commit of the jwt admission, branch `upstream/session-admission-jwt-5` (`859b5ed81`, on `upstream/session-volume-source-3`, the mirror `main` @ `b42a6c53a`), where the claim's name is configuration like the others (`sessions.admission.jwt.claims.agentNamespace`, default `agent_namespace`), for kagent-dev/kagent#3156's owner to move the pull request to. At the re-pin onto an upstream with #3156 this commit drops Rebased onto upstream `main` @ `7a0a4fd0` and folded with the jwt admission into one upstream-ready commit as branch `upstream/session-volume-admission` (`d1365420`, DCO-signed, no fork references, `make -C go test` and lint green there); it supersedes the heads above, and a human decides whether it replaces kagent-dev/kagent#3155 and #3156.
