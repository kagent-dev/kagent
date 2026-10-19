---
id: ui-leave-the-schedules-lifecycle-through-its-back
fork: kagent
theme: test-flakes
category: test
status: carried
commits:
  - 'test(ui): leave the schedules lifecycle through its Back button'
upstream:
  outcome: to-file
  refs: []
  exit: file in the UI test batch
depends_on: []
verified: 2026-10-19
---
# test(ui): leave the schedules lifecycle through its Back button

## Change

`test(ui): leave the schedules lifecycle through its Back button`

## Reason

the lifecycle's last step loaded the list again, a load of the dev server the journey is short of, and counted zero links before the list had rendered; it goes back with the page's Back and waits for the seeded row first

## History

to file — same branch and row as the line above
