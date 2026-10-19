---
id: migrations-refuse-to-start-on-a-1-x
fork: kagent
theme: compat-migrations
category: compat
status: carried
commits:
  - 'fix(migrations): refuse to start on a 1.x database instead of migrating it'
upstream:
  outcome: fork-only
  refs: []
  exit: time-box to a line release
depends_on: []
verified: 2026-10-19
---
# fix(migrations): refuse to start on a 1.x database instead of migrating it

## Change

`fix(migrations): refuse to start on a 1.x database instead of migrating it`

## Reason

every 1.x release recorded its baseline as core version 1, the version this tree's baseline carries too, so Goose took a 1.x database for current and the controller started on the `agent_instance` tables. The core source checks for that table before any migration or verification and fails startup with the remedy (`migrations.ErrLegacyBaseline`): a fresh database, the old one kept for retention. The line does not migrate the 1.x schema; each installation's cut-over starts on a fresh database on its existing CNPG cluster (decision of 2026-09-29: conversations, shares, scheduled-run executions and Slack thread bindings are not carried over)

## History

fork-only: upstream has no released baseline to protect
