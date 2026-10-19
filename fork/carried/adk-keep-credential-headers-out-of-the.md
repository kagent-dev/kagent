---
id: adk-keep-credential-headers-out-of-the
fork: kagent
theme: credential-injection
category: bugfix
status: carried
commits:
  - 'fix(adk): keep credential headers out of the persisted session state'
upstream:
  outcome: to-file
  refs: []
  exit: file the branch
depends_on: []
verified: 2026-10-19
---
# fix(adk): keep credential headers out of the persisted session state

## Change

`fix(adk): keep credential headers out of the persisted session state`

## Reason

the Python ADK handed a turn its request headers as persisted session state, so an actor-local SQLite session store kept every caller's `Authorization` header at rest in the session row and in each turn's event, and with it in the actor's durable directory and its snapshots. Credential-bearing headers now reach the turn through ADK `temp:` state only, and `LocalSessionService` removes the ones a store written before holds, once, at its first use. The Go ADK keeps request headers in the call context and persists none

## History

to file — prepared in the fork: branch `upstream/adk-credential-headers-not-persisted`
