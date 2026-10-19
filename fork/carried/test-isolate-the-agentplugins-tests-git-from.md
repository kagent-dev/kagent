---
id: test-isolate-the-agentplugins-tests-git-from
fork: kagent
theme: test-flakes
category: test
status: carried
commits:
  - 'test(core): isolate the agentplugins tests'' git from global config'
upstream:
  outcome: to-file
  refs: []
  exit: file in the test batch
depends_on: []
verified: 2026-10-19
---
# test(core): isolate the agentplugins tests' git from global config

## Change

`test(core): isolate the agentplugins tests' git from global config`

## Reason

`commitFiles` in the agentplugins tests committed under the developer's global git configuration, so on a machine with `commit.gpgsign=true` and no usable key five agentplugins tests failed with `gpg: signing failed` though the code was fine (giantswarm/kagent-upstream#493). The fixture sets `GIT_CONFIG_GLOBAL=/dev/null` and `GIT_CONFIG_NOSYSTEM=1`

## History

to file — prepared in the fork: branch `upstream/agentplugins-test-git-config`
