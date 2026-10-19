---
id: skills-retry-a-git-fetch-on-a-transient-network
fork: kagent
theme: harness-variety
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#2646, kagent-dev/kagent#3099, kagent-dev/kagent#3258]
  exit: OCI skills (#3203) make it moot
depends_on: []
commit: ce450faf6
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(skills): retry a git fetch on a transient network failure` | a golden boot fetched every git skill and plugin source once (`CloneGitCommit`: `git init`, `remote add`, one `git fetch --depth 1`), so one dropped connection on the sandbox's egress path crashed the whole boot and cost the controller's restart backoff (20 s, 40 s, …): the gvisor-lane e2e `TestGitArtifactCredentialDelivery/claude` met a 503 from the egress gateway on one boot and an unresolvable origin name on the next (giantswarm/kagent-upstream#271, #265). The fetch now runs up to four times, 1 s, 2 s and 4 s apart, while git fails on a transient network error (name resolution, a refused, timed-out or dropped connection, a 5xx answer); a 401, 403, 404 or unknown commit fails at once with git's error. The OCI source (crane's retry transport) and the S3 source (the AWS SDK's standard retryer) already retry and are unchanged | to file — prepared in the fork: branch `upstream/skills-fetch-retry` (mirror `main` @ `92dd6caa` + the patch, `a6f70bba`, DCO-signed, no fork references, unit tests green there; its message is the pull-request text). kagent-dev/kagent searched 2026-10-12: nothing on a fetch retry; kagent-dev/kagent#3099 changes the same file for credentials and kagent-dev/kagent#2646 is the adjacent skills-init idempotency report |
