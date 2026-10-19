---
id: controller-write-the-harness-ready-condition
fork: kagent
theme: golden-boot
category: bugfix
status: carried
upstream:
  outcome: to-file
  refs: [kagent-dev/kagent#2764, kagent-dev/kagent#2876]
  exit: 're-file against #2764'
depends_on: []
commit: a2f201d48
verified: 2026-10-19
---
| Carried commit (subject) | Why the platform needs it | Upstream |
|---|---|---|
| `fix(controller): write the Harness Ready condition from its golden boots` and `fix(controller): keep a Harness Ready until a golden boot fails` | nothing wrote `Harness.status`, so `kubectl get harness` showed an empty READY column and `Harness.ready` of the gRPC API was always false although the Harness served (giantswarm/kagent-upstream#210). The controller now derives `Ready` from the Harness's WorkerPool and the golden boots of the Agents that reference it, on its own status queue: `True` (`Booted`) once one Agent's golden boot succeeded, since every revision boots the Harness's workload; `False` with the failed boot's reason and the Agent named (`ActorTemplateFailed`, `ActorTemplateRetrying`, `RuntimePreparationFailed`) while no Agent booted, or `WorkerPoolNotFound`; before any golden boot finished `True` (`WorkerPoolResolved`), since Helm's and Flux's readiness wait reads any other `Ready` as a rollout in progress and a Harness without Agents would hold every release of its chart. The status write keeps `status.capabilities` | kagent-dev/kagent#2764 (open) reports the gap; kagent-dev/kagent#2876, which set `Ready` from the WorkerPool alone so a Harness whose image never boots read ready, was closed unmerged by the stale bot on 2026-10-12. To file — prepared in the fork as one commit: branch `upstream/harness-ready-condition` |
