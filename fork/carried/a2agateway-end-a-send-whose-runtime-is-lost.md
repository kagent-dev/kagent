---
id: a2agateway-end-a-send-whose-runtime-is-lost
fork: kagent
theme: session-lifecycle
category: bugfix
status: carried
commits:
  - 'fix(a2agateway): end a send whose runtime is lost, and delete a paused Actor as it is'
upstream:
  outcome: drops-at-repin
  refs: [giantswarm/kagent-upstream#43, kagent-dev/kagent#2447, kagent-dev/kagent#2939]
  exit: drop the commit; survived re-pins 10-05, 10-08, 10-12
depends_on: []
verified: 2026-10-19
---
# fix(a2agateway): end a send whose runtime is lost, and delete a paused Actor as it is

## Change

`fix(a2agateway): end a send whose runtime is lost, and delete a paused Actor as it is`

## Reason

a session kagent parks on `input-required`/`auth-required` is paused, not suspended: its only checkpoint lives on the worker's node, and when that node goes (a spot interruption, a consolidation, a node roll) nothing notices until the person answers — the resume fails after Substrate's parking budget with the text of the last refusal, kagent's gateway records it as one more failed dispatch, every retry repeats the wait, and the session cannot be deleted because `Delete` suspends first and suspending a paused Actor dials the atelet on the node that is gone (seen on a production installation: a session paused for input whose node was interrupted hours later). The gateway now asks the lifecycle workflow after every failed runtime stream whether the runtime is lost — the Actor `CRASHED` (what Substrate makes of an Actor it cannot bring back: a restore out of time, a worker gone, and with the Substrate line's companion patch a pause checkpoint whose node is gone) or gone — and if it is, fails the task with the cause under the message prefix `runtime lost: ` (`go/api/a2a` `RuntimeLostMessagePrefix`), moves the instance to `FAILED` with `failure.reason` `RuntimeLost` and the same message, and refuses later sends with that message without dialing the runtime, instead of asking the runtime whether it took the task (a second parking budget). The transcript stays readable; `Delete` deletes a `PAUSED` or `CRASHED` Actor as it is (`any_state` for the paused one) and suspends only a live Actor first. A runtime whose state Substrate cannot report is not presumed lost. Docs: `docs/architecture/runtime-and-lifecycle.md` "Lost runtimes"

## History

superseded structurally by kagent-dev/kagent#2939 (the gateway task run this patches is gone): the patch drops at the re-pin (still carried as `c2394209` after the 2026-10-12 re-pin onto `92dd6caa`), the requirement is rebuilt on the Session model in the re-pin candidate and needs an upstream issue (kagent-dev/kagent#2447 is the adjacent report). Was prepared in the fork: branch `upstream/runtime-lost` (`3e5b77ef`, on the mirror `main` plus `upstream/a2agateway-dispatch-failure` and `upstream/a2agateway-detached-dispatch`, which it stacks on); carried as `e821fcdc` (giantswarm/kagent-upstream#43), released in `v0.11.0-gs.17` (the Substrate line carries the companion fail-fast and crash reason; the pause TTL is a separate patch)
