---
id: session-require-https-for-the-jwt-admissions-jwks
fork: kagent
theme: workspace-volumes
category: bugfix
status: carried
commits:
  - 'fix(session): require https for the jwt admission''s JWKS'
  - 'fix(session): keep the controller up on a plain http JWKS URL'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3155, kagent-dev/kagent#3156]
  exit: with row 209
depends_on: [session-configurable-admission-verifying-a-signed]
verified: 2026-10-19
---
# fix(session): require https for the jwt admission's JWKS

## Change

`fix(session): require https for the jwt admission's JWKS`, `fix(session): keep the controller up on a plain http JWKS URL`

## Reason

the jwt Session admission fetched its verification keys from any http or https URL, so over plain http anyone on the network path could replace the JWKS in transit and sign admission grants the controller accepts (giantswarm/kagent-upstream#559). `KAGENT_SESSION_ADMISSION_JWT_JWKS_URL` must be https; a plain http URL is refused unless `KAGENT_SESSION_ADMISSION_JWT_ALLOW_HTTP` (chart `controller.session.admission.jwt.allowHTTP`) is set — the controller logs it and keeps running on an admission that refuses every volume source, so a consumer that has not set the opt-in loses volume sources, never the controller (giantswarm/kagent-upstream#573), the explicit opt-in for an in-cluster issuer that serves no TLS yet, mirroring `KAGENT_A2A_PUSH_ALLOW_HTTP`

## History

to file — prepared in the fork: folded into the upstream-ready commit of the jwt admission, branch `upstream/session-admission-jwt-5` (`859b5ed81`, the same fold as `upstream/session-admission-jwt-4` (`952973fd5`), `upstream/session-admission-jwt-3` with `sessions.admission.jwt.allowHttp`; an http `jwksUrl` without it is logged and the controller runs on an admission refusing every volume source, both commits of this row folded; on `upstream/session-volume-source-3`, the mirror `main` @ `b42a6c53a`), for kagent-dev/kagent#3156's owner to move the pull request to; no upstream issue or pull request covers it (searched kagent-dev/kagent for JWKS https). At the re-pin onto an upstream with #3156 this commit drops Rebased onto upstream `main` @ `7a0a4fd0` and folded with the jwt admission into one upstream-ready commit as branch `upstream/session-volume-admission-2` (`5d3f43d1`, on upstream `main` @ `2261632c`, Substrate v0.6.0-alpha3; DCO-signed, no fork references, `make -C go test` green there); it supersedes the heads above, and a human decides whether it replaces kagent-dev/kagent#3155 and #3156.
