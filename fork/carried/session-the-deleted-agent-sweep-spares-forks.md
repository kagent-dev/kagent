---
id: session-the-deleted-agent-sweep-spares-forks
fork: kagent
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(session): the deleted-Agent sweep spares forks and running turns'
upstream:
  outcome: to-file
  refs: []
  exit: with the sweep row
depends_on: [session-delete-the-sessions-of-a-deleted-agent]
verified: 2026-10-19
---
# fix(session): the deleted-Agent sweep spares forks and running turns

## Change

`fix(session): the deleted-Agent sweep spares forks and running turns`

## Reason

a checkpoint retains runnable inputs after its source and its Agent are deleted, and a fork of it is the only session that can be created once the Agent is gone, yet the deleted-Agent sweep (`fix(session): delete the sessions of a deleted Agent`) listed the fork as a session of a deleted Agent and deleted it, also while its first turn ran: the send's runtime stream broke with `Internal` and `TestRuntimeRevisionLifecycle` flaked on the sweep's tick (giantswarm/kagent-upstream#241). A fork pins its checkpoint until it is deleted, so the sweep leaves a session holding a checkpoint pin alone, in the listing and at admission; the runtime revision GC collects the revision once the last fork and the checkpoint are gone. Admission defers a session whose task still runs (`FailedPrecondition`), so the sweep never cuts a turn off. CI runs the e2e suite with a one-second expiration poll interval. Docs: `docs/architecture/runtime-and-lifecycle.md` "Deleted Agents"

## History

to file — folded into the deleted-Agent sweep's prepared upstream branch `upstream/session-deleted-agent-sweep` (the sweep is not upstream yet)
