---
id: e2e-retry-a-session-delete-on-aborted
fork: kagent
theme: test-flakes
category: test
status: carried
commits:
  - 'test(e2e): retry a Session delete on Aborted as on FailedPrecondition'
upstream:
  outcome: to-file
  refs: []
  exit: file in the test batch
depends_on: []
verified: 2026-10-19
---
# test(e2e): retry a Session delete on Aborted as on FailedPrecondition

## Change

`test(e2e): retry a Session delete on Aborted as on FailedPrecondition`

## Reason

the e2e cleanup `deleteIdleSession` retried only `FailedPrecondition` and failed the test on `Aborted`, DeleteSession's answer while a lifecycle operation the Session admitted is still pending (the deleted-Agent sweep's delete, a suspend), so a test whose Session was mid-lifecycle at cleanup failed twice, on the defect it hit and on its cleanup: the sweep race of giantswarm/kagent-upstream#241 showed up that way (giantswarm/kagent-upstream#363). The helper retries both codes on a backoff doubling from 100 ms to a 2 s cap within its minute, fails any other code at once and names the last answer on the deadline; its unit tests script the answers

## History

to file — prepared in the fork: branch `upstream/e2e-delete-session-aborted-retry` (mirror `main` @ the pin `bf8afa56` + the patch, DCO-signed, no fork references; its message is the pull-request text). kagent-dev/kagent searched 2026-10-11: nothing on the cleanup's `Aborted`
