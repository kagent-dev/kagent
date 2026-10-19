---
id: migrations-add-the-push-notification-tables
fork: kagent
theme: compat-migrations
category: compat
status: carried
commits:
  - 'fix(migrations): add the push notification tables to a database on the earlier baseline'
upstream:
  outcome: fork-only
  refs: []
  exit: time-box to a line release
depends_on: []
verified: 2026-10-19
---
# fix(migrations): add the push notification tables to a database on the earlier baseline

## Change

`fix(migrations): add the push notification tables to a database on the earlier baseline`

## Reason

upstream added `session_push_registration`, `session_push_outbox`, their indexes and the unique key `session_task_event_push_source` to the baseline `core/000001_initial.sql` (kagent-dev/kagent#3036) after the line's installations had applied it, so a database migrated before the re-pin onto `main@92dd6caa` (core at version 4) had none of them and every Session operation failed with `relation "session_push_registration" does not exist`. `core/000005` creates each of them as the baseline does, `IF NOT EXISTS`, so a database the current baseline created is unchanged; its Down is empty, the baseline's drops them

## History

fork-only: upstream folds schema changes into its unreleased 1.0 baseline and recreates development databases (the baseline's header); the line has installations on the earlier baseline
