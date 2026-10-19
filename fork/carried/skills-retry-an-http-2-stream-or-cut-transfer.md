---
id: skills-retry-an-http-2-stream-or-cut-transfer
fork: kagent
theme: harness-variety
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: OCI skills (#3203) make it moot
depends_on: [skills-retry-a-git-fetch-on-a-transient-network]
commit: a3ab2affa
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(skills): retry an HTTP/2 stream or cut transfer within a time budget` | a restored Go ADK agent re-fetches its git skills at its start, before it serves `/readyz`; a fetch that failed on a refused HTTP/2 stream (`REFUSED_STREAM`, the egress gateway answering 503) was not classified as transient, and the retry of `fix(skills): retry a git fetch on a transient network failure` was bounded only by its count (four attempts of an ~11 s connect timeout outlast the 30 s wakeup probe), so the actor was marked crashed and its conversation lost in a Substrate step test on a test lab (giantswarm/kagent-upstream#541). HTTP/2 stream errors, a cut transfer (`early EOF`, `transfer closed with outstanding read data remaining`, `the remote end hung up unexpectedly`) and TLS read errors now retry, and no attempt starts once 15 s have passed since the first | to file — prepared in the fork: branch `upstream/skills-fetch-retry-budget` (`upstream/skills-fetch-retry` + the patch, DCO-signed, no fork references; it builds on that branch's still unfiled change) |
