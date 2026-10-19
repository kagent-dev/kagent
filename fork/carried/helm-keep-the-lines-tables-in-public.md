---
id: helm-keep-the-lines-tables-in-public
fork: kagent
theme: compat-migrations
category: compat
status: carried
commits:
  - 'fix(helm): keep an installation''s tables and pgvector in the public schema'
  - 'fix(helm): default the table and pgvector schemas to upstream''s'
upstream:
  outcome: drops-at-repin
  refs: []
  exit: remove the pair
depends_on: []
verified: 2026-10-19
---
# fix(helm): keep an installation's tables and pgvector in the public schema

## Change

`fix(helm): keep an installation's tables and pgvector in the public schema`, `fix(helm): default the table and pgvector schemas to upstream's`

## Reason

upstream moved kagent's tables to a `kagent` schema and pgvector to `extensions` with no migration of existing tables (clean installs only); every installation of the line held its tables in `public` and its pgvector there, so the chart's defaults stayed `public` and an upgrade kept its sessions. Retired by `feat(migrations): move an installation's tables from public into the configured schemas`: the controller now moves them at its start, and the second commit returns `database.postgres.schema` and `vectorSchema` to upstream's `kagent` and `extensions` and `kagent install` to upstream's values

## History

dropped — the two commits cancel out; the next re-pin leaves both out and deletes this file (both still on the branch after the 2026-10-12 re-pin onto `92dd6caa`: `05358271`, `77f63506`)
