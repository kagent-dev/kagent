---
id: e2e-name-the-sessions-actor-when-a-send
fork: kagent
theme: test-flakes
category: test
status: carried
commits:
  - 'chore(e2e): name the Session''s Actor when a send stays refused'
upstream:
  outcome: to-file
  refs: []
  exit: file in the test batch
depends_on: []
verified: 2026-10-19
---
# chore(e2e): name the Session's Actor when a send stays refused

## Change

`chore(e2e): name the Session's Actor when a send stays refused`

## Reason

`TestClaudeSessionWorkspace` flaked in the gVisor lane (giantswarm/kagent-upstream#635) with only `input was not accepted; retry after the session becomes available`: the refusal says the Session did not come up, not why. A send whose refusal outlasts its window now fails naming the Session's Substrate Actor, its state and its crash message (there: the NFS volume mount timing out after 110 s). The resend window becomes the fixture's, `sendRetryWindow` by default, so a Session whose Actor comes up slower can set a longer one. Its unit tests show the window and the report

## History

to file with `chore(e2e): run each send under the caller's context`, whose resend window it makes the caller's
