# Persistence, Checkpoints, and Forks

## Durable interaction model

`AgentInstance` represents ephemeral compute. An A2A context durably owns its
tasks and ordered events, allowing interaction history to remain as an audit trail
after compute is removed. Normally a newly created context ID equals its
AgentInstance ID, but they are separate identities.

The core PostgreSQL records are:

| Record | Purpose |
| --- | --- |
| `runtime_revision` | Immutable compiled input and ate-api identity |
| `agent_template_harness_pair` | Pair status and latest successful revision |
| `agent_instance` | Compute identity, pinned revision, lifecycle phase, and Actor identity |
| `agent_instance_share` | Instance authorization grants |
| `a2a_context` | Durable owner of interaction history |
| `agent_instance_task` | Materialized current A2A task state |
| `agent_instance_task_event` | Append-only ordered task and message events |
| `agent_instance_checkpoint` | Named immutable snapshot/history boundary |

Identity columns use PostgreSQL's native UUID type. Other framework-specific
tables are runtime implementation details, not part of this ownership model.

```mermaid
flowchart TD
    PAIR[Harness + AgentTemplate pair] --> REV[runtime revision]
    REV --> INSTANCE[AgentInstance]
    INSTANCE -. normally same initial ID .-> CONTEXT[A2A context]
    CONTEXT --> TASK[materialized tasks]
    TASK --> EVENT[ordered task events]
    CONTEXT --> CHECKPOINT[checkpoint boundary]
    REV --> CHECKPOINT
    CHECKPOINT --> TAG[Substrate snapshot tag]
    CHECKPOINT --> FORK[forked AgentInstance]
    FORK --> NEWCTX[new A2A context]
    CONTEXT -->|bounded history copy| NEWCTX
```

## Checkpoint creation

A checkpoint names a quiescent boundary already recorded by the gateway. Creating
one does not suspend the Actor again:

1. Reserve the checkpoint in PostgreSQL.
2. Verify that the suspended Actor still holds the external snapshot URI and scope recorded on the boundary.
3. Create a Substrate `Tag`, which copies the Actor's current snapshot into independent storage, and verify the source did not change during the copy.
4. Atomically persist the Tag UID and copied snapshot URI and mark the checkpoint ready.

The `CREATING` reservation blocks task admission and lifecycle changes until the
copy completes or cleanup finishes. A lost response can reuse a completed Tag;
an incomplete copy is deleted before the failed reservation is released. A
failed database finalization leaves the reservation and completed Tag for retry.
The gateway records boundaries without retaining every turn: only explicit
checkpoints survive subsequent suspends or source deletion.

The checkpoint retains source-instance provenance, source context, prepared
revision, labels, head task, and history sequence. The source AgentInstance may be
deleted while its context and checkpoint remain.

Deletion first hides the checkpoint, then deletes its snapshot tag, then removes
the row. A checkpoint referenced by a fork cannot be deleted. Substrate deletes the Tag's copied snapshot with the Tag.

## Forking

Forking creates a new AgentInstance and A2A context. It copies task/event history
through the checkpoint sequence, deterministically remapping task and message IDs,
then creates the Actor from the checkpoint's snapshot tag. New work appends only
to the fork's context; source history and the checkpoint remain immutable. The copied head boundary
uses the retained Tag URI, allowing a fresh fork to be checkpointed before its
first turn. Fork creation verifies the Tag UID and URI as well as the Actor's
source Tag, suspended state, template, and external snapshot.

Checkpoint sharing is not implemented. Future sharing must be restricted to data
snapshots without process state.

The workflow lives in
[`go/core/internal/service/checkpoint`](../../go/core/internal/service/checkpoint).
