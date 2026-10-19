---
id: ui-wait-on-the-agent-lists-reread
fork: kagent
theme: test-flakes
category: test
status: carried
commits:
  - 'test(ui): wait on the Agent list''s re-read in the agent CRUD journeys'
upstream:
  outcome: to-file
  refs: []
  exit: file in the UI test batch
depends_on: []
verified: 2026-10-19
---
# test(ui): wait on the Agent list's re-read in the agent CRUD journeys

## Change

`test(ui): wait on the Agent list's re-read in the agent CRUD journeys`

## Reason

the agent CRUD journeys failed the line's `ui-tests` on loaded runners: the row assertion after create, save or delete had five seconds for the list's mount and its re-read of the Agents, and the longest case spent the thirty-second budget with every step passing. The journeys now wait for the list's answered read (the mock backend counts answers beside calls), press the row controls once they hold still, and carry `LIFECYCLE_TIMEOUT`; the mock counts the Agent and ScheduledRun RPCs it registered after its counters were published

## History

to file — prepared in the fork: branch `upstream/ui-agent-crud-list-reread`
