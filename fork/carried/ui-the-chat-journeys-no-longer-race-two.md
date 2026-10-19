---
id: ui-the-chat-journeys-no-longer-race-two
fork: kagent
theme: test-flakes
category: test
status: carried
commits:
  - 'fix(ui): the chat journeys no longer race two tooltips and the reply'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#2773, kagent-dev/kagent#2871]
  exit: file in the UI test batch
depends_on: []
verified: 2026-10-19
---
# fix(ui): the chat journeys no longer race two tooltips and the reply

## Change

`fix(ui): the chat journeys no longer race two tooltips and the reply`

## Reason

the fork's `ui-tests` runs upstream's browser suite on GitHub-hosted runners, slower than upstream's, and chat journeys timed out on Firefox (giantswarm/kagent-upstream#68). Upstream fixed the checkpoints, transcript and template races its own way (kagent-dev/kagent#3118, #3132); this commit carries the rest: the rail row's ellipsis tooltip opens above the row, because opened to the right it covered the row's own menu button and a pointer moving from the name to the menu kept it open with the button unreachable under it (both engines, every run, reproduced locally with the trace), and the twelve-step questions journey, one test around five page loads since upstream merged its two journeys (kagent-dev/kagent#3132), runs on twice the lifecycle budget (a boot of the dev server is ten seconds and more on a busy runner); `test(ui): one test per way out of a parked question` replaces that doubled budget with one budget per journey

## History

to file — prepared in the fork: branch `upstream/ui-chat-journeys` (mirror `main` + the patch, DCO-signed, no fork references; its message is the pull-request text). kagent-dev/kagent searched 2026-09-23: #2773 (two specs flaking under parallel load) and #2871/#2877 (the Cancel label two confirmations shared) are the neighbours, nothing on the tooltips or the snapshot button's wait; upstream's own `ui-tests` on Blacksmith runners reports no flaky test
