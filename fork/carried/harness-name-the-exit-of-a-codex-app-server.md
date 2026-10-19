---
id: harness-name-the-exit-of-a-codex-app-server
fork: kagent
theme: harness-variety
category: bugfix
status: carried
commits:
  - 'fix(harness): name the exit of a Codex App Server whose stream closes'
  - 'fix(harness): read a Codex App Server''s stream to its end after it exits'
upstream:
  outcome: to-file
  refs: []
  exit: file
depends_on: []
verified: 2026-10-19
---
# fix(harness): name the exit of a Codex App Server whose stream closes

## Change

`fix(harness): name the exit of a Codex App Server whose stream closes` and `fix(harness): read a Codex App Server's stream to its end after it exits`

## Reason

A Codex App Server that exits mid-protocol failed the turn with `codex protocol stream closed while awaiting thread/start` or `… without a terminal event`, without the exit status or the stderr, because the driver reported the closed stdout before `Wait` had copied stderr (giantswarm/kagent-upstream#663). The driver now waits for the process, bounded by the interrupt grace, and the error names the exit status or signal and carries the stderr. Its stdout is a pipe the driver owns, which `Wait` cannot close under the reader, and an exit seen before the stream closes is read to the stream's end, so a turn completed just before the exit completes and a killed one is named the same way every time (giantswarm/kagent-upstream#668)

## History

to file — prepared in the fork: branch `upstream/codex-app-server-exit`
