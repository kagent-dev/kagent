---
id: adk-fall-back-to-the-durable-copy-of
fork: kagent
theme: harness-variety
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3203, kagent-dev/kagent#3258]
  exit: skills via OCI images (#3203) make the git path moot
depends_on: [skills-retry-a-git-fetch-on-a-transient-network, skills-retry-an-http-2-stream-or-cut-transfer]
commit: 9836c34c4
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(adk): fall back to the durable copy of a skill a restart cannot fetch` | a restored Go ADK actor starts with an empty root filesystem and materializes its Agent Plugin resources again before it serves `/readyz`; one source it could not fetch ended the process (`materialize Agent Plugins: ... exit status 128`), the wakeup probe timed out after 30 s and Substrate marked the actor crashed, so the conversation on its durable volume was lost, although the skill had been fetched at the earlier start (giantswarm/kagent-upstream#541). Every fetched skill or plugin root is now copied to `/data/agent-plugins` on the durable volume, keyed by the exact source, version included; a restart whose fetch fails uses the copy of the same source and logs it, a start without a copy of that exact source (the first start, a new commit) fails as before, so a pin is never served by the version it replaced, and copies no longer named are pruned. A restart on the artifact it already ran keeps the conversation; a crashed actor loses it | to file — prepared in the fork: branch `upstream/adk-skill-durable-copy-exact` (mirror `main` @ `92dd6caa` + the patch, DCO-signed, no fork references) |
