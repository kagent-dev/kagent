---
id: session-end-a-resume-that-left-its-actor-suspended
fork: kagent
theme: session-lifecycle
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: []
  exit: one design issue per family on Substrate state machine
depends_on: [session-resume-again-while-substrate-has-no-free-worker]
commit: 46aed74ad
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(session): end a resume that left its Actor suspended` | a `ResumeSession` issued while ate-api's pods were replaced (a Substrate upgrade under a worker drain on a test installation) answered `Unavailable` at once, and the Session then refused `DeleteSession` with `Aborted: begin Session operation: resource conflict` (giantswarm/kagent-upstream#414, fixed in giantswarm/kagent-upstream#417). A resume onto the agent's current revision repoints the Actor after the operation's claim, and those reads and the repoint ran once while the controller's connection to the rolling ate-api answered every call `Unavailable`; the claimed operation kept its executor, and a delete is refused while claimed work may have been issued. Every ate-api call of a resume is now issued again while Substrate answers later, within the attempt, and a resume that still fails while its Actor is suspended on the template the session records ends its operation (`CancelSessionOperation`): the session stays `SUSPENDED` and takes a delete or a new resume. A resume Substrate failed for a transient cause (a worker drained under the restore: `Internal`, the Actor `CRASHED` until Substrate returns it to rest, giantswarm/substrate#310) keeps its operation; a delete that finds it held with no attempt running claims it and ends it once the Actor rests suspended or is gone, then deletes the session. The service logs the cause of a failed resume. Docs: `docs/architecture/runtime-and-lifecycle.md` | to file — builds on the carried `session-resume-again-while-substrate-has-no-free-worker` and `session-superseded-revision` changes; its `upstream/` branch is prepared stacked on theirs once the first has one |
