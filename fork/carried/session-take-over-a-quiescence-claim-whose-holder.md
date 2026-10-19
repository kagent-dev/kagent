---
id: session-take-over-a-quiescence-claim-whose-holder
fork: kagent
theme: durable-pause
category: bugfix
status: carried
commits:
  - 'fix(session): take over a quiescence claim whose holder stopped'
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#3010]
  exit: 'file against #3010'
depends_on: []
verified: 2026-10-19
---
# fix(session): take over a quiescence claim whose holder stopped

## Change

`fix(session): take over a quiescence claim whose holder stopped`

## Reason

the settling of the row above lived only in the memory of the API process that held the claim, and a held claim was never reassigned, so a restart, a rollout or a crash of that process while it settled, or while its Pause or Quiesce was in flight, wedged the session again (giantswarm/kagent-upstream#175). A claim now holds a lease in database time (`quiescence_claimed_until`, 30 s) that its holder renews every 10 s until the boundary is settled; a claim whose lease ran out, or that has none (claims made before the upgrade), is claimed again under a new executor id as `TakenOver` and settled from the Actor's state, never by repeating the runtime request; the stopped holder's renew, finish and release are refused. The column comes with the core migration `000002_quiescence_claim_lease.sql`, because the line's released databases already hold `000001`; upstream folds schema changes into `000001` until its release, so the upstream version of the commit puts the column there. Docs: `docs/architecture/runtime-and-lifecycle.md`

## History

to file with the row above on kagent-dev/kagent#3010
