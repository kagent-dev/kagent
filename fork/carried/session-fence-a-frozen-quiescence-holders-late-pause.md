---
id: session-fence-a-frozen-quiescence-holders-late-pause
fork: kagent
theme: durable-pause
category: bugfix
status: carried
commits:
  - 'fix(session): fence a frozen quiescence holder''s late Pause or Suspend'
upstream:
  outcome: fork-only
  refs: [giantswarm/substrate#182]
  exit: Substrate fencing token upstream (T4 design issue)
depends_on: [giantswarm/substrate:pause-suspend-resume-take-optional-fencingtoken]
verified: 2026-10-19
---
# fix(session): fence a frozen quiescence holder's late Pause or Suspend

## Change

`fix(session): fence a frozen quiescence holder's late Pause or Suspend`

## Reason

the claim's records fence a holder that stopped, not one frozen past its lease (a GC pause, a node stall, a slow network) while its Pause or Suspend is in flight: the successor settles a RUNNING Actor as not quiesced and releases the claim, and the late request then pauses or suspends an Actor the session believes is running (giantswarm/kagent-upstream#181). Every Pause, Suspend and fencing Resume a holder sends now carries a Substrate `FencingToken` (its executor id and a generation from the `quiescence_fencing_generation` sequence, which grows with every request); before releasing a claim on a RUNNING Actor the holder sends a fenced Resume with a new generation, which records the token without touching the runtime, so Substrate refuses a late request with `FailedPrecondition`; a holder whose own request is refused that way was superseded and leaves the claim to its successor. The token rides `substrate.WithFencingToken` on the request's context, so the lifecycle and gateway calls without one are unchanged. Needs a Substrate that takes the token (the line's `1.5.0`, giantswarm/substrate#182; an older Substrate refuses the field as unknown, `InvalidArgument` naming protobuf tag 10001, and the next row's fallback then sends the calls unfenced). The sequence comes with the core migration `000004_quiescence_fencing_generation.sql`; the upstream version puts it into `000001`. Docs: `docs/architecture/runtime-and-lifecycle.md`

## History

none upstream; written upstream-ready, filed with the row above once upstream Substrate takes the token (giantswarm/substrate#182's row)
