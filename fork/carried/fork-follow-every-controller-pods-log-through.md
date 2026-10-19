---
id: fork-follow-every-controller-pods-log-through
fork: kagent
theme: ci-publish-infra
category: infra
status: carried
commits:
  - 'ci(fork): follow every controller pod''s log through the e2e run'
upstream:
  outcome: fork-only
  refs: []
  exit: keep
depends_on: []
verified: 2026-10-19
---
# ci(fork): follow every controller pod's log through the e2e run

## Change

`ci(fork): follow every controller pod's log through the e2e run`

## Reason

the e2e job read the controller log from the Deployment once the tests were over, which holds only the final pod; the tests that roll the controller (`withControllerEnv`, the sandbox restart) lost the log of every earlier pod, among them the window of every failure analysed for giantswarm/kagent-upstream#204. The job follows every controller pod from the moment it runs, one file per pod

## History

fork-only
