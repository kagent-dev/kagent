# Data Lifecycle

This document describes the current state of data storage in kagent: what data is created, where it lives, and what happens when resources are deleted.

## Where Data Lives

kagent stores data in two places:

1. **Kubernetes (etcd)** — Agent, ToolServer, RemoteMCPServer, ModelConfig, ModelProviderConfig, and Memory custom resources are stored as CRDs managed by the Kubernetes API server. These follow standard Kubernetes lifecycle: they persist until explicitly deleted via `kubectl delete` or the API, and are subject to the cluster's etcd storage.

2. **Relational database** — Runtime data (sessions, tasks, events, checkpoints, feedback) is stored in a relational database managed by [GORM](https://gorm.io/). Tables are created via `AutoMigrate` at startup (no versioned migrations).

## Database Tables

| Table | Primary Key | Description |
|-------|-------------|-------------|
| `agent` | `id` | Agent configuration synced from CRDs. Stores agent type and an optional JSON `config` blob. |
| `session` | `(id, user_id)` | A conversation context. Optionally linked to an agent via `agent_id`. |
| `task` | `id` | An A2A task within a session. The full `protocol.Task` is JSON-serialized in the `data` column. |
| `event` | `(id, user_id)` | A message or event within a session. Stores a JSON-serialized `protocol.Message`. Indexed on `session_id`. |
| `push_notification` | `id` | Push notification configuration for a task. Indexed on `task_id`. |
| `feedback` | `(id, user_id)` | User feedback on agent responses. Has an `OnDelete:CASCADE` constraint on `message_id`. |
| `tool` | `(id, server_name, group_kind)` | Tool metadata discovered from MCP servers. |
| `toolserver` | `(name, group_kind)` | Registered MCP tool servers. |
| `lg_checkpoint` | `(user_id, thread_id, checkpoint_ns, checkpoint_id)` | LangGraph agent state checkpoints. |
| `lg_checkpoint_write` | `(user_id, thread_id, checkpoint_ns, checkpoint_id, write_idx)` | Individual write operations for LangGraph checkpoints. |
| `crewai_agent_memory` | `(user_id, thread_id)` | Long-term memory for CrewAI agents. |
| `crewai_flow_state` | `(user_id, thread_id, method_name)` | Flow execution state for CrewAI agents. |

All tables include `created_at`, `updated_at`, and `deleted_at` (soft-delete) timestamp columns managed by GORM.

### Relationships

```text
Agent (1) ──── (*) Session (1) ──── (*) Task
                    │                     │
                    └──── (*) Event       └──── (*) PushNotification
```

There are no foreign key cascade constraints between sessions, tasks, and events. The only cascade constraint is `Feedback.MessageID` (`OnDelete:CASCADE`).

### Write Semantics

All `Store*` methods use GORM's `OnConflict{UpdateAll: true}` clause, giving upsert behavior — a record is created if it does not exist, or updated in place if it does.

## Deletion Behavior

- **Deleting a session** (`DELETE /api/sessions/{id}`): Sets `deleted_at` on the session row. Associated tasks and events are **not** deleted or modified.
- **Deleting a task** (`DELETE /api/tasks/{id}`): Sets `deleted_at` on the task row. Push notifications for the task are **not** deleted.
- **Deleting a message**: Sets `deleted_at` on the event row. Feedback referencing the message **is** cascade-deleted (the only cascade in the schema).
- **Deleting a Kubernetes CRD** (e.g., `kubectl delete agent my-agent`): Removes the resource from etcd. The corresponding database `agent` row and its sessions/events are **not** automatically cleaned up.

Soft-deleted rows remain in the database. They are excluded from queries by GORM's default scoping but are not physically removed.

## Conversation History

Conversation history is stored as `event` rows linked to a session via `session_id`. Each event contains a JSON-serialized `protocol.Message` (user messages, agent responses, tool calls). Events grow with each interaction and are never automatically pruned or rotated.

LangGraph checkpoints (`lg_checkpoint`, `lg_checkpoint_write`) store intermediate agent state during task execution. These also accumulate over time with no automatic cleanup.

## Default Storage Configuration

### Kubernetes (Helm)

The Helm chart defaults to SQLite on a **memory-backed `emptyDir`** volume:

```yaml
# helm/kagent/values.yaml
database:
  type: sqlite
  sqlite:
    databaseName: kagent.db
```

The controller deployment mounts:

```yaml
volumes:
  - name: sqlite-volume
    emptyDir:
      sizeLimit: 500Mi
      medium: Memory
```

The database file is `/sqlite-volume/kagent.db`. Because `medium: Memory` maps to tmpfs, all data is lost when the pod restarts or is rescheduled.

### PostgreSQL

Setting `database.type: postgres` switches to an external PostgreSQL instance. The default connection URL in the Helm chart is:

```
postgres://postgres:kagent@pgsql-postgresql.kagent.svc.cluster.local:5432/postgres
```

### Configuration Reference

| Source | Variable / Flag | Default |
|--------|----------------|---------|
| Env | `DATABASE_TYPE` | `sqlite` |
| Env | `SQLITE_DATABASE_PATH` | `/sqlite-volume/kagent.db` (Helm) or `./kagent.db` (binary) |
| Env | `POSTGRES_DATABASE_URL` | *(see Helm values)* |
| Flag | `--database-type` | `sqlite` |
| Flag | `--sqlite-database-path` | `./kagent.db` |
| Flag | `--postgres-database-url` | `postgres://postgres:kagent@db.kagent.svc.cluster.local:5432/crud` |

## Data Retention

kagent has no built-in data retention, cleanup, or garbage collection. All rows grow indefinitely until manually deleted through the API or direct database access.

## Related Files

- [`models.go`](../../go/pkg/database/models.go) — GORM struct definitions (schema source of truth)
- [`client.go`](../../go/internal/database/client.go) — Database client implementation
- [`manager.go`](../../go/internal/database/manager.go) — Database connection and `AutoMigrate`
- [`app.go`](../../go/pkg/app/app.go) — CLI flags and environment variable mapping
- [`values.yaml`](../../helm/kagent/values.yaml) — Helm chart defaults
- [`controller-deployment.yaml`](../../helm/kagent/templates/controller-deployment.yaml) — Volume and mount definitions
