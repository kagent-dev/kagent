---
id: ui-one-test-per-way-out-of
fork: kagent
theme: test-flakes
category: test
status: carried
commits:
  - 'test(ui): one test per way out of a parked question'
upstream:
  outcome: to-file
  refs: []
  exit: file in the UI test batch
depends_on: []
verified: 2026-10-19
---
# test(ui): one test per way out of a parked question

## Change

`test(ui): one test per way out of a parked question`

## Reason

the fork's `ui-tests` failed every attempt of a re-pin candidate's Firefox run on `chat/questions.spec.ts` (giantswarm/kagent-upstream#507): upstream holds its three journeys — a parked question answered with its choices, a second one given up, a one-field question answered with the keyboard — in one twelve-step test on one sixty-second budget, and on the hosted runner the first eight steps took fifty-five seconds, so the keyboard journey timed out at its first step. Each starts from its own page load and park, so each is its own test on its own budget (about fifteen, twelve and six seconds on Firefox); this replaces the doubled budget `fix(ui): the chat journeys no longer race two tooltips and the reply` gave the single test, which a re-pin's conflict resolution can drop

## History

to file — prepared in the fork: branch `upstream/ui-questions-one-test-per-way-out` (mirror `main` + the patch, DCO-signed, no fork references; its message is the pull-request text)
