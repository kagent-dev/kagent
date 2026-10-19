---
id: test-isolate-the-claude-adapter-tests-git-from
fork: kagent
theme: test-flakes
category: test
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: file in the test batch
depends_on: []
commit: 7d3ad3491
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `test(harness): isolate the claude adapter test's git from global config` | `skillRepository` in the Claude adapter's tests committed under the developer's global git configuration, so on a machine with `commit.gpgsign=true` and no usable key the adapter tests failed with `gpg: signing failed` though the code was fine (giantswarm/kagent-upstream#398). The fixture sets `GIT_CONFIG_GLOBAL=/dev/null` and `GIT_CONFIG_NOSYSTEM=1` | to file — the Claude harness adapter is carried code, so the test has no upstream counterpart |
