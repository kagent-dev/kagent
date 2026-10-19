---
id: e2e-git-and-gh-act-as-the-person
fork: kagent
theme: test-flakes
category: test
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: with the upstream branch
depends_on: [egress-a-sessions-actor-jwt-on-its-bearer, egress-a-sessions-provider-credentials-by]
commit: 7739e8021
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `chore(e2e): git and gh act as the person on the kagent and claude Harnesses`, `chore(e2e): read the gateway's refusals in the git and gh as the person e2e` and `chore(e2e): the commit is the person's on the claude Harness too` | the Session pieces of a coding agent working as the person each had their own proof (the workspace directory, the admission grant, the actor-JWT credential on a provider's host, the placeholder git and gh credentials, the turn's git identity), but nothing proved the whole path end to end: a Declarative agent on the `kagent` Harness, in a Session of a workspace, fetching, committing and pushing a private repository and calling the forge's API as the person through the `bash` tool (giantswarm/kagent-upstream#292). `TestGitAsThePersonKagentHarness` runs it on the `kagent` and `claude` Harnesses against a forge that verifies the Session Actor's JWT: the forge receives the Actor's JWT for its audience and never the placeholder, the sandbox's environment, `/data` and the Session's directory hold no JWT, the commit is the person's on both, a credential the egress gateway cannot resolve fails the push and the `gh` call with the gateway's 403 (`no credential provider configured for …`) and nothing reaching the forge, a Session without a grant gets the gateway's 403 (`actor egress policy denied TLS destination`), and each Session writes only its own directory. The git fixture takes an authorizer, the TLS serving and the fixed-Service exposure become helpers, and the workspace fixture mirrors a given origin | to file with the Session caller-credential series it proves (the actor-JWT providers and the Session admission are carried, not upstream) |
