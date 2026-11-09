# Creating an Agent

This document explains which component is responsible for each part of
preparing a newly applied Agent, and why the components interact the way they
do. Use it to decide where a change belongs. The RPCs and queries behind each
arrow are in the code linked at the end.

![Creating an Agent](images/agent-creation.svg)

## Phases

Numbers on the arrows match these phases.

1. **Apply.** The user applies the Agent and the resources it references.
2. **Compile.** Informers notice the change. The translator turns the Agent and
   its inputs into an immutable revision, identified by a digest.
3. **Record intent.** The reconciler stores the desired revision and the
   Agent's egress grants, then applies the grants to any running Session
   actors. A new Agent has none.
4. **Create the runtime template.** The reconciler asks Substrate for an
   ActorTemplate, then records which template belongs to the revision together
   with its configuration.
5. **Bootstrap and snapshot.** Substrate starts a golden actor. It fetches its
   configuration from BootstrapService, saves it, and Substrate snapshots it.
6. **Ready.** The reconciler sees the snapshot is ready, promotes the revision,
   and reports `Ready=True`. New Sessions now start from this revision.

## Responsibilities

| Component | Responsible for | Not responsible for |
| --- | --- | --- |
| Kubernetes API | What the user wants, and the status reported back | Runtime state |
| Translator | Turning an Agent into a revision, using the Harness's compiler | Any I/O, or applying what it produces |
| Reconciler | Making the revision exist in PostgreSQL and Substrate, retrying, and reporting status. It is the only writer of an Agent's grants. | Compiling, and Session lifecycle |
| BootstrapService | Giving a golden actor its configuration, and only its own | Sessions, or any user authority |
| ate-api | Templates, actors, and their egress policies | Knowing what an Agent is |
| egress gateway | Enforcing egress and adding the actor's identity to its requests | Deciding what is allowed |
| Golden actor | Saving its configuration and becoming ready to be snapshotted | Serving users |

## What kagent stores

kagent PostgreSQL, not Kubernetes, is what Sessions are created from. Only
kagent reads it.

| Record | Holds | Why |
| --- | --- | --- |
| Agent definition (`agent_definition`) | One row per Agent UID: the desired revision, the revision new Sessions use, and egress grants | Grants and promotion depend on what the Agent wants now, not on any one revision. A recreated Agent has a new UID and inherits no grants. |
| Agent revision (`agent_revision`) | The Agent Card and configuration bundle for one revision | Agent-specific data for a revision. The bundle is immutable, so every actor started from the revision gets the same configuration. |
| Runtime revision (`runtime_revision`) | The Substrate template, egress pinned at compile time, and golden retry state | Links kagent's revision to Substrate's template so BootstrapService can check a caller. Shared with sandboxes. |
| Sessions (`runtime_instance`, `session`) | Which revision each actor runs | Egress updates reach every running actor, and a revision cannot be deleted while it is in use. |

They are associated as follows:

- An Agent definition points to two Agent revisions: the desired one and the
  one new Sessions use. The second only changes when the desired revision
  becomes ready.
- An Agent revision extends exactly one runtime revision.
- Each Session is pinned to one revision, so editing the Agent never changes a
  running Session.

## Why it works this way

**Compilation has no I/O.** The same inputs always give the same digest, so
retries, restarts, and replicas reach the same template. Compilers can be
tested without a cluster.

**Intent is recorded before compute exists.** A revision is promoted only if
it is still the desired one, so a late result from a replaced preparation can
never become the revision new Sessions use.

**Egress grants are separate from the revision.** Editing an EgressPolicy
updates running actors without a new revision or snapshot, and an unrelated
compile error cannot block a revocation.

**Configuration is fetched, not embedded.** A template environment value is
limited to 32,768 characters; a bundle can be 256 KiB. The template carries
only a small pointer to the bundle.

**The bundle is bound to the template.** BootstrapService serves a bundle only
to an actor started from its template. Substrate assigns the template's UID on
creation, so the binding is recorded afterwards and the golden actor retries
until it exists.

**The golden actor holds no credential.** The egress gateway adds its identity
to each request, and it can reach only the control plane.

**Sessions start from a golden snapshot.** The configuration is in the
snapshot, so new Sessions, forks, and restarts never call BootstrapService.

**A failed revision does not affect new Sessions.** They keep using the last
successful revision until a new one is ready.

## Where changes belong

- A new Agent field that changes runtime behavior: the translator or a harness
  compiler. It changes the digest.
- A new harness: a compiler registered for its runtime type.
- State that must survive a controller restart: a store method the reconciler
  calls.
- A new Substrate call: the Substrate client, called from the reconciler.
- Configuration a runtime needs at startup: the bootstrap bundle, not the
  template environment.

## Failures and recovery

### Retryable errors

Any PostgreSQL or Substrate call can fail. The reconciler's queue owns the
retry: the Agent reports `Ready=False` with reason `RuntimePreparationFailed`
and is requeued with a backoff from 1s to 30s and no attempt limit. While its
configuration is not yet bound, the golden actor retries the fetch itself.

### Golden build failures

If the golden snapshot fails, the reconciler deletes the template and creates
it again after 15s, 1m, 4m, and then every 10m, with up to 20% jitter. The
Agent reports `Ready=False` with reason `ActorTemplateRetrying`. The schedule is
stored with the runtime revision, so it survives controller restarts.

### Errors that need a change

These are not retried until the Agent or its inputs change:

- Compilation fails with `RevisionInvalid`, `WorkerPoolNotFound`, or
  `ActorTemplateInvalid`. This also clears `desiredRevision`.
- Substrate rejects the template (`ActorTemplateRejected`).
- An existing immutable template differs (`ActorTemplateConflict`).

Grant errors set `EgressPoliciesApplied=False` but never block `Ready`. See
[Configuration and compilation](configuration-and-compilation.md#prepared-revision-pipeline)
and [Egress policies](egress-policies.md#preparation-and-lifecycle) for details.

## Code

- Translator: [`translator/compiler.go`](../../go/core/internal/translator/compiler.go)
- Reconciler: [`controller/reconciler.go`](../../go/core/internal/controller/reconciler.go)
  and [`controller/egress.go`](../../go/core/internal/controller/egress.go)
- BootstrapService: [`service/bootstrap/service.go`](../../go/core/internal/service/bootstrap/service.go)
- Substrate client: [`substrate/actor_template.go`](../../go/core/internal/substrate/actor_template.go)
- Store: [`database/runtime_revisions.go`](../../go/core/internal/database/runtime_revisions.go)
