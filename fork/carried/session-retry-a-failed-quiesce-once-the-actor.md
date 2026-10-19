---
id: session-retry-a-failed-quiesce-once-the-actor
fork: kagent
theme: durable-pause
category: bugfix
status: carried
commits:
  - 'fix(session): retry a failed quiesce once the Actor runs again'
upstream:
  outcome: to-file
  refs: []
  exit: with the T4 design issue
depends_on: [session-fence-a-frozen-quiescence-holders-late-pause]
verified: 2026-10-19
---
# fix(session): retry a failed quiesce once the Actor runs again

## Change

`fix(session): retry a failed quiesce once the Actor runs again`

## Reason

the settle of a failed quiesce (the row above it) released the claim without a snapshot as soon as the Actor was `RUNNING` again, so a first `SuspendActor` that outlived its deadline and was abandoned by Substrate (the first suspend after an atelet restart, giantswarm/substrate#162) left the turn without a snapshot for good, and nothing on the session said why (giantswarm/kagent-upstream#213). A `RUNNING` Actor now gets the pause or suspend once more, fenced with a newer token, at the settle backoff; the claim is released without a snapshot only when the retry fails too or the Actor is crashed or gone, and the release records why on the session as the output-only `Session.last_quiescence_failure` (task, reason `SuspendFailed`/`PauseFailed`/`RuntimeUnavailable`, message, time), shown by `GetSession`, `ListSessions` and the CLI's session details and cleared by the next snapshot; a successor that took over a frozen holder's claim pauses or suspends a running Actor itself. Stored in the session's protobuf, no migration. Docs: `docs/architecture/runtime-and-lifecycle.md`

## History

prepared, not opened: branch `upstream/session-retry-failed-quiesce` (the settle row's upstream branch + the commit without the fencing token)
