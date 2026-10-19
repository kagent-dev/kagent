---
id: ui-one-test-per-claim-for-a-bare
fork: kagent
theme: test-flakes
category: test
status: carried
commits:
  - 'test(ui): one test per claim for a bare build''s extension points'
upstream:
  outcome: to-file
  refs: []
  exit: file in the UI test batch
depends_on: []
verified: 2026-10-19
---
# test(ui): one test per claim for a bare build's extension points

## Change

`test(ui): one test per claim for a bare build's extension points`

## Reason

"a bare build renders the application and nothing else" timed out in its last `page.goto` on a loaded Firefox run (giantswarm/kagent-upstream#79): five independent claims, six page loads on the thirty-second budget of one. Four tests of one to three loads each

## History

to file — same branch and row as the line above
