---
id: ui-leave-the-schedules-lifecycle-through-its-back
fork: kagent
theme: test-flakes
category: test
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: file in the UI test batch
depends_on: []
commit: 76269249b
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `test(ui): leave the schedules lifecycle through its Back button` | the lifecycle's last step loaded the list again, a load of the dev server the journey is short of, and counted zero links before the list had rendered; it goes back with the page's Back and waits for the seeded row first | to file — same branch and row as the line above |
