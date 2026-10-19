---
id: test-isolate-the-agentplugins-tests-git-from
fork: kagent
theme: test-flakes
category: test
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: file in the test batch
depends_on: []
commit: f7bbc1233
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `test(core): isolate the agentplugins tests' git from global config` | `commitFiles` in the agentplugins tests committed under the developer's global git configuration, so on a machine with `commit.gpgsign=true` and no usable key five agentplugins tests failed with `gpg: signing failed` though the code was fine (giantswarm/kagent-upstream#493). The fixture sets `GIT_CONFIG_GLOBAL=/dev/null` and `GIT_CONFIG_NOSYSTEM=1` | to file — prepared in the fork: branch `upstream/agentplugins-test-git-config` |
