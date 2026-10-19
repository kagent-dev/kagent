---
id: api-a-per-source-credential-for-private-git
fork: kagent
theme: credential-injection
category: feature
status: carried
upstream:
  outcome: superseded
  refs: [kagent-dev/kagent#2899, kagent-dev/kagent#3099, kagent-dev/kagent#3258]
  exit: '#3099 lands or git sources deferred (#3258)'
depends_on: []
commit: 6c243cb82
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `feat(api): a per-source credential for private git skill and plugin sources` (with `fix(skills): git sends the Authorization the egress gateway replaces`) | two of the platform's live agents take their skills from private repositories, and `main` has no credential path for artifact sources (the materialiser shells out to git with the process environment and no helper). `skills[].source.git.credentialRef` (and `plugins[].source.git.credentialRef`) names a same-namespace Secret key; the compiler binds it to the source's host as `Authorization: Basic <Secret value>` through the egress gateway's credential injection (upstream kagent-dev/kagent#2868), the same path a model key takes; the actor holds an inert placeholder in `KAGENT_ARTIFACT_CREDENTIAL_<hash>` and clones with no credential of its own. Substrate 1.5's egress effect replaces a credential header the request carries and adds none, so the compiled source is marked `authenticated` and the materialiser has git send `Authorization: Basic <placeholder>` for that source's URL only (`http.<url>.extraHeader` in the invocation's environment, `GIT_TERMINAL_PROMPT=0`); a public source sends none (giantswarm/kagent-upstream#251). The Secret key holds the base64 of `<username>:<token>` (GitHub: `x-access-token:<token>`). One credential serves every credentialed source of the agent tree on that host; a public source on the same host sends no header and gets no credential; two Secrets for one host are refused. Reshaped at the 2026-09-22 re-pin: before it the token was resolved into the actor's environment and offered by a git credential helper, a design kept only until the egress gateway could inject the credential. Chosen over an ambient per-Harness credential, which would reach every actor of the Harness: a missing Secret fails only the referencing templates, and the token reaches only the actors that reference it | kagent-dev/kagent#3099 (draft, `GitArtifact.authorizationFrom`: the Secret holds the full header value, fetched at the first task inside the Session actor), which replaced the line's closed kagent-dev/kagent#2899; the re-pin that carries it adopts `authorizationFrom` here, in the e2e, the agentlab fixture and the agent chart, the golden-only scope unchanged |
