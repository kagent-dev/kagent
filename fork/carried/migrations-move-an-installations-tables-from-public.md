---
id: migrations-move-an-installations-tables-from-public
fork: kagent
theme: compat-migrations
category: feature
status: carried
commits:
  - 'feat(migrations): move an installation''s tables from public into the configured schemas'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3111]
  exit: file
depends_on: []
verified: 2026-10-19
---
# feat(migrations): move an installation's tables from public into the configured schemas

## Change

`feat(migrations): move an installation's tables from public into the configured schemas`

## Reason

upstream moved kagent's tables to a `kagent` schema and pgvector to `extensions` (kagent-dev/kagent#3111) with no migration of existing tables, so an installation upgraded onto it would start on empty tables and lose every Agent's sessions, conversations and memories. At start, before the migration prechecks, the controller and `kagent db migrate` move the tables, views, sequences and both tracking tables from `public` into `database.postgres.schema` and, with vectors enabled, pgvector into `vectorSchema`, in one transaction under the core migration lock; a database already in the configured schemas is untouched, one split between both schemas is refused (`ErrHalfMoved`), and `VerifyMigrated` refuses both with migrations skipped. No down path: the chart README says how to move the tables back before a rollback (giantswarm/kagent-upstream#456)

## History

to file — prepared in the fork: branch `upstream/schema-migration-from-public` (mirror `main` @ the pin `92dd6caa` + the patch, `ab57503b`, DCO-signed, no fork references; its message is the pull-request text), the upgrade path #3111 left out. kagent-dev/kagent searched 2026-10-13: no issue or pull request moving existing tables out of `public`
