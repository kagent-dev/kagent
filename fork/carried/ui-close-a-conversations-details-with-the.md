---
id: ui-close-a-conversations-details-with-the
fork: kagent
theme: test-flakes
category: test
status: carried
commits:
  - 'test(ui): close a conversation''s details with the details modal''s own Close'
upstream:
  outcome: to-file
  refs: []
  exit: file in the UI test batch
depends_on: []
verified: 2026-10-19
---
# test(ui): close a conversation's details with the details modal's own Close

## Change

`test(ui): close a conversation's details with the details modal's own Close`

## Reason

the agent rail's rename journey failed loaded Firefox runs on a strict-mode violation (giantswarm/kagent-upstream#508): it presses Close right after a rename saved from inside the details modal, while the rename box is still animating out and its close button answers to the same name. The press is scoped to the details modal

## History

to file — prepared in the fork: branch `upstream/ui-details-modal-close` (mirror `main` @ `7433d70b` + the patch, DCO-signed, no fork references)
