---
id: e2e-prove-a-sessions-credential-uri-through
fork: kagent
theme: test-flakes
category: test
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: with the upstream branch
depends_on: [egress-a-sessions-provider-credentials-by]
commit: 0e5249a10
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `chore(e2e): prove a Session's credential URI through a fake credential provider`, `chore(e2e): the credential provider's Deployment is in the client's scheme` and `chore(e2e): git's anonymous first request carries no provider credential` | a Session's provider whose credential the egress gateway fetches by `credential_uri` (`ate-secret://<authority>/<instance>/<git\|api>`) was unit-tested in the compiled egress policy and proven live only with muster as the provider, never in kagent's own suite (giantswarm/kagent-upstream#297). The e2e lane now builds a fake credential provider (`go/core/test/credentialprovider`, image `e2e-credential-provider`), registers it with Substrate's egress gateway (`credentialProvider.additionalProviders`, authority `credentials.e2e.kagent.dev`) and `TestGitAsThePersonKagentHarness` runs it behind its Service with the pod certificates the gateway requires: on the `kagent` and `claude` Harnesses, a Session granted the provider as Basic on a forge host and Bearer under `/api/` pushes with git and calls the API with wget, and every request the forge receives carries the provider's answer for that Session's Actor, never the placeholder; the sandbox's environment, `/data` and the Session's directory hold no credential; a provider instance the provider refuses fails git and gh with the provider's refusal in the gateway's 403; a Session without a grant gets the gateway's 403 on the host | to file with the Session caller-credential series it proves (the Session admission's providers and the credential authority are carried, not upstream) |
