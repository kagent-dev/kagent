---
id: test-isolate-the-claude-adapter-tests-git-from
fork: kagent
theme: test-flakes
category: test
status: carried
commits:
  - 'test(harness): isolate the claude adapter test''s git from global config'
upstream:
  outcome: to-file
  refs: []
  exit: file in the test batch
depends_on: []
verified: 2026-10-19
---
# test(harness): isolate the claude adapter test's git from global config

## Change

`test(harness): isolate the claude adapter test's git from global config`

## Reason

`skillRepository` in the Claude adapter's tests committed under the developer's global git configuration, so on a machine with `commit.gpgsign=true` and no usable key the adapter tests failed with `gpg: signing failed` though the code was fine (giantswarm/kagent-upstream#398). The fixture sets `GIT_CONFIG_GLOBAL=/dev/null` and `GIT_CONFIG_NOSYSTEM=1`

## History

to file — the Claude harness adapter is carried code, so the test has no upstream counterpart
