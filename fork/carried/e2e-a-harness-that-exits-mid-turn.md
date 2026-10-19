---
id: e2e-a-harness-that-exits-mid-turn
fork: kagent
theme: test-flakes
category: test
status: carried
commits:
  - 'test(e2e): a Harness that exits mid-turn fails the turn'
upstream:
  outcome: fork-only
  refs: []
  exit: with the Substrate exit-report RPC upstream
depends_on: []
verified: 2026-10-19
---
# test(e2e): a Harness that exits mid-turn fails the turn

## Change

`test(e2e): a Harness that exits mid-turn fails the turn`

## Reason

proves the container exit report end to end (giantswarm/kagent-upstream#649): the kagent Harness's bash tool sends SIGQUIT to the Harness mid-turn, the Go runtime exits with status 2 while the sandbox keeps running, and the turn must end `FAILED` within two minutes naming the exited `kagent` container, with the Session still deletable. Before the report the turn waited out its deadline with the task `working`

## History

fork-only: it relies on the Substrate line's container exit report
