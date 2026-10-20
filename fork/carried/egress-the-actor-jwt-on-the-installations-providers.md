---
id: egress-the-actor-jwt-on-the-installations-providers
fork: kagent
theme: credential-injection
category: gs-specific
status: carried
commits:
  - 'feat(egress): the actor JWT on the installation''s providers that verify it'
  - 'fix(session): the actor JWT providers on a Session created without a grant'
  - 'test(e2e): a Session without a grant reaches the actor JWT provider as its Actor'
  - 'fix(session): actor JWT providers only on a Session its creator binds'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3156]
  exit: with row 210
depends_on: []
verified: 2026-10-20
---
# feat(egress): the actor JWT on the installation's providers that verify it

## Change

`feat(egress): the actor JWT on the installation's providers that verify it`

## Reason

a workspace Session's sandbox calls muster as its person with no credential and no loopback token in the sandbox: muster verifies Substrate's actor JWT itself and serves the person the Session's actor is bound to with the reserved provider `muster`. The installation names such provider instances with their hostname and audience (`KAGENT_SESSION_ACTOR_JWT_PROVIDERS`, chart `controller.session.actorJWTProviders`); every Session admitted with a grant gets each as a `Bearer` provider on its host, stored with the grant's own providers (so the binding agent-manager makes from `Session.providers` lists it), compiled on create, repoint and the move back after a lost race into a `CredentialHeader` with `actor_jwt` bound to the instance's audience (agent-substrate/substrate#1960) instead of a credential URI, on every origin of the host the Actor's policy allows. It needs no credential authority; a grant naming the instance or a provider on its host on any path is refused. A Session created without a volume source (a chat thread started outside a workspace) gets the same providers alone when its creator binds its Actor (`CreateSessionRequest.bind_actor`, which agent-manager's `start_session` sets), so its person binds it in muster as a workspace one and a Harness without a caller forwarder (codex) reaches muster's tools; one nobody binds (a Session created straight through the API) lists none and its runtime keeps forwarding the caller's credential, since muster refuses an actor nobody bound. `TestSessionEgressPolicyBindsTheActorJWTOfAProviderThatVerifiesIt`, `TestAdmitsEverySessionWithTheActorJWTProviders`, `TestParseActorJWTProviders`, `TestServiceCreateGivesASessionWithoutAVolumeTheActorJWTProviders`, the chart test for the value

## History

The grant-less Session's providers: to file — prepared in the fork: branch `upstream/actor-jwt-providers-without-grant` (one commit on `upstream/egress-actor-jwt-providers`).
to file — prepared in the fork: branch `upstream/egress-actor-jwt-providers` (one DCO-signed commit on `upstream/egress-provider-credential-uri`), filed with or after kagent-dev/kagent#3156 The upstream form is prepared as branch `upstream/session-egress-destinations-2` (`9c6eab5a`, one DCO-signed commit on `upstream/session-volume-admission`, no fork references, a session header refused on a caller-token passthrough origin, `make -C go test` and lint green there), superseding the `upstream/egress-*` branches: `Session.egress` (field 20) in the vocabulary of an `EgressPolicy` destination, each header from a `credential_uri` under the installation's `sessions.admission.jwt.credentialAuthority` or an `actor_jwt`; the installation-wide actor-JWT providers become grant entries with `actor_jwt`, the path prefix waits for Substrate's `CredentialHeader.path_prefix` upstream. At the re-pin onto an upstream with it, the fork's `Session.providers` (field 18) ships its automatic migration to `egress`.
