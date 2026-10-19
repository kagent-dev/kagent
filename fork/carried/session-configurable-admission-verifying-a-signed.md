---
id: session-configurable-admission-verifying-a-signed
fork: kagent
theme: workspace-volumes
category: feature
status: carried
commits:
  - 'feat(session): configurable admission verifying a signed grant'
  - 'fix(session): a JWKS that never answers makes the grant keys unavailable'
  - 'test(e2e): retry a Session create while the grant keys are unavailable'
  - 'fix(session): log the refused admission on the controller''s logger'
upstream:
  outcome: open
  refs: [kagent-dev/kagent#3153, kagent-dev/kagent#3155, kagent-dev/kagent#3156]
  exit: '#3156 rebased and merged'
depends_on: [api-workspace-volume-reference-and-admission-token, session-refuse-a-workspace-grant-for-another]
verified: 2026-10-19
---
# feat(session): configurable admission verifying a signed grant

## Change

`feat(session): configurable admission verifying a signed grant`

## Reason

a Session on a workspace's shared volume is admitted by a short-lived JWT grant the platform's workspace issuer signs once it has checked that the person can read every repository of the workspace, so the check holds for every surface and for a direct API call alike. The built-in `jwt` admission (`go/core/internal/service/session/jwtadmission`, configured by `KAGENT_SESSION_ADMISSION_JWT_{JWKS_URL,ISSUER,AUDIENCE}` and the chart's `controller.session.admission.jwt`) verifies the signature against the issuer's JWKS (cached, refetched on an unknown `kid`, so a rotation needs no restart), `iss`, `aud`, `exp` and `nbf`, that `sub` is the caller, that the grant's volume (CSI driver and handle) is the request's and every requested mount one it allows (the Session's `sessions/${SESSION_ID}` read-write, `mirrors` read-only). What it admits, the providers (hostname, audience, scheme, Basic user name, broker) and the repositories changed since the last sync, is stored on the Session as `providers` and `changed` and shown on get and list; the token is stored or shown nowhere. Every refusal is `PermissionDenied` with its reason, an unreachable JWKS `Unavailable`. The test signer (`go/core/test/grant`) signs the volume and its allowed mounts. Unset, a volume source is refused as before

## History

open upstream as kagent-dev/kagent#3156 (branch `upstream/session-admission-jwt`, stacked on #3155). The upstream form makes the grant's claim names configuration (`KAGENT_SESSION_ADMISSION_JWT_{VOLUME,MOUNTS,CHANGED}_CLAIM`, chart `controller.session.admission.jwt.claims`, defaults `volume`, `mounts`, `changed`) and leaves out `providers`, which belongs to the per-Session caller credentials still to be opened; `changed` is `Session` field 19 as here. At the re-pin onto an upstream with #3156 this commit shrinks to the `providers` delta: `Session` field 18, persisted on the Session row, keeps its number or ships an automatic migration. Since kagent-dev/kagent#3153 removed the controller's application environment variables, the rebased upstream form is configured by the controller config's `sessions.admission.jwt` (`jwksUrl`, `issuer`, `audience`, `claims`; chart `config.sessions.admission.jwt`) instead: branch `upstream/session-admission-jwt-5` (`859b5ed81`, `upstream/session-admission-jwt-4` (`952973fd5`) plus the `agent_namespace` claim of `session-refuse-a-workspace-grant-for-another.md`; on `upstream/session-volume-source-3`, the mirror `main` @ `b42a6c53a`; `upstream/session-admission-jwt-3` plus an https-only `jwksUrl` with the `allowHttp` opt-in, a plain http one without it logged and refusing every volume source while the controller keeps running), for #3156's owner to move the pull request to; the line keeps its environment variables until the re-pin onto that `main` Rebased onto upstream `main` @ `7a0a4fd0` and folded with the jwt admission into one upstream-ready commit as branch `upstream/session-volume-admission` (`d1365420`, DCO-signed, no fork references, `make -C go test` and lint green there); it supersedes the heads above, and a human decides whether it replaces kagent-dev/kagent#3155 and #3156.
