---
id: e2e-wait-for-an-agent-through-the-golden
fork: kagent
theme: golden-boot
category: test
status: carried
commits:
  - 'chore(e2e): wait for an Agent through the golden boot''s first retries'
upstream:
  outcome: fork-only
  refs: []
  exit: with the retry row
depends_on: [controller-start-a-crashed-golden-boot-over-instead]
verified: 2026-10-19
---
# chore(e2e): wait for an Agent through the golden boot's first retries

## Change

`chore(e2e): wait for an Agent through the golden boot's first retries`

## Reason

`TestGitArtifactCredentialDelivery/claude` flaked in the gVisor lane (giantswarm/kagent-upstream#265): the golden boot's git fetch failed twice on transient sandbox networking — the egress gateway's first connection to the mock-origin Service created a second earlier timed out (503), and the next boot's sandbox could not resolve the name the cluster had answered 50 s before. The controller starts a crashed golden boot over (20 s, then 40 s later), but the Agent wait gave up after 2 minutes, before the third boot. The wait now covers a boot and its first two retries (4 minutes); a boot that crashes every time still fails it, and the assertions are unchanged

## History

fork-only while `fix(controller): start a crashed golden boot over instead of failing the Agent for good` is; goes upstream with it
