---
id: egress-keep-artifact-credentials-off-a-sessions
fork: kagent
theme: credential-injection
category: bugfix
status: carried
commits:
  - 'feat(egress): keep artifact credentials off a Session''s egress policy'
  - 'refactor(session): drop the execute path''s dead revision writes'
  - 'fix(substrate): replace a new Actor''s inherited template allowlist'
  - 'test(e2e): Codex mock turns call exec_command'
upstream:
  outcome: to-file
  refs: [agent-substrate/substrate#2117, agent-substrate/substrate#2159, kagent-dev/kagent#2117, kagent-dev/kagent#2604, kagent-dev/kagent#3099]
  exit: 'after #3099 and as#2159'
depends_on: [agent-substrate/substrate#2117, agent-substrate/substrate#2159, api-a-per-source-credential-for-private-git]
verified: 2026-10-19
---
# feat(egress): keep artifact credentials off a Session's egress policy

## Change

`feat(egress): keep artifact credentials off a Session's egress policy` (with `refactor(session): drop the execute path's dead revision writes`, `fix(substrate): replace a new Actor's inherited template allowlist` and `test(e2e): Codex mock turns call exec_command`)

## Reason

a private skill or plugin source binds its git credential (an App or deploy token) as `authorization: Basic` on the source's host, and the revision's one credential list compiled both the golden template's default policy and every Session actor's own, so any request a sandbox sent to that host with an `Authorization` header (a shell tool, an MCP client, `gh`) left with the token and read what the token can reach rather than what the person of the turn can. A binding now carries `scope`: empty for every actor (model and MCP bindings, unchanged), `golden` for an artifact binding; the golden template keeps every binding, the Session's create, repoint and rollback policies drop the golden ones (`egress.ForActors`), and the host stays reachable without the header. Substrate gives a new Actor its template's default policy, so Session creation replaces that inherited default before the Actor first runs and still refuses any other existing allowlist. The scope is a revision input: an agent with a credentialed source renders a new revision once and its READY sessions are repointed onto it; other agents keep their digest. `TestGitArtifactCredentialDelivery` adds a sandbox turn that requests the source with and without the placeholder header and requires the Secret value on none of them. Docs: `docs/architecture/credential-injection.md`; the e2e commit's Codex turns call `exec_command`, because Codex 0.163 no longer accepts the `shell_command` name and the sandbox turn otherwise completed without running git, and its mocks answer only once the command's output reaches the model (the builtin-tool mock is upstream's own form, kagent-dev/kagent `0a57368a`)

## History

upstream has no credential path for artifact sources yet: kagent-dev/kagent#3099 (draft) adds `GitArtifact.authorizationFrom` and fetches standalone skills at the first task inside the Session actor, so its credential is on Session egress by design until golden-time egress exists upstream (agent-substrate/substrate#2159, #2117, kagent-dev/kagent#2604); exit: the follow-up a review note on kagent-dev/kagent#3099 names (the fetch back at golden boot, scoped as here), prepared as an `upstream/…` branch on the mirror plus that pull request's head once it is merged, after agent-substrate/substrate#2159 and #2117
