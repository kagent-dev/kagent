---
id: session-allow-the-admitted-provider-hosts
fork: kagent
theme: credential-injection
category: feature
status: carried
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3156]
  exit: with row 210
depends_on: [egress-a-sessions-provider-credentials-by]
commit: 13c384bee
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(session): allow the admitted provider hosts on the Session's own Actor` | a Session on a workspace volume sets up its directory before the first turn by fetching the repositories that moved since the last sync from its providers, and then works there with git and the provider's CLI, all through the egress gateway. Its Actor's policy was compiled from the revision alone, so an Agent whose `spec.egress` did not list the provider's origin had every fetch refused with a 403 and worked on the last sync, and an author cannot know which provider a person's workspace uses. Each admitted provider's host (`Session.providers`) is now an HTTPS origin of the Session's own policy on create, on a repoint and on the move back after a lost race; the template's default the Actor starts with, the revision and the Agent's other Sessions are unchanged. `TestSessionEgressPolicyAllowsTheSessionsProviders` | to file with the Session's `providers`: the upstream form of the signed-grant admission (kagent-dev/kagent#3156) leaves `providers` out until the per-Session caller credentials are opened upstream; this commit goes with that filing The upstream form is prepared as branch `upstream/session-egress-destinations-2` (`9c6eab5a`, one DCO-signed commit on `upstream/session-volume-admission`, no fork references, a session header refused on a caller-token passthrough origin, `make -C go test` and lint green there), queued as review-list row 210, superseding the `upstream/egress-*` branches: `Session.egress` (field 20) in the vocabulary of an `EgressPolicy` destination, each header from a `credential_uri` under the installation's `sessions.admission.jwt.credentialAuthority` or an `actor_jwt`; the installation-wide actor-JWT providers become grant entries with `actor_jwt`, the path prefix waits for Substrate's `CredentialHeader.path_prefix` upstream. At the re-pin onto an upstream with it, the fork's `Session.providers` (field 18) ships its automatic migration to `egress`. |
