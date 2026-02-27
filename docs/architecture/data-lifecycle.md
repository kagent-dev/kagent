# Task, Session, and Event Data Lifecycle

This document describes how kagent currently stores task, session, and event data.

## Data Model

kagent persists data in a relational database managed by [GORM](https://gorm.io/). The schema is defined in Go structs and tables are created via GORM's `AutoMigrate` at startup (no versioned migrations).

### Tables

| Table | Primary Key | Description |
|-------|-------------|-------------|
| `agent` | `id` | Agent configuration. Stores agent type and an optional JSON `config` blob. |
| `session` | `(id, user_id)` | A conversation context. Optionally linked to an agent via `agent_id`. |
| `task` | `id` | An A2A task within a session. The full `protocol.Task` is JSON-serialized in the `data` column. |
| `event` | `(id, user_id)` | A message or event within a session. The `data` column stores a JSON-serialized `protocol.Message`. Indexed on `session_id`. |
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

There are no foreign key cascade constraints between sessions, tasks, and events. Deleting a session does not automatically delete its tasks or events. The only cascade constraint is `Feedback.MessageID` (`OnDelete:CASCADE`).

### Write Semantics

All `Store*` methods use GORM's `OnConflict{UpdateAll: true}` clause, giving upsert behavior — a record is created if it does not exist, or updated in place if it does.

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

volumeMounts:
  - name: sqlite-volume
    mountPath: /sqlite-volume
```

The database file is `/sqlite-volume/kagent.db`. Because `medium: Memory` maps to tmpfs, **all data is lost when the pod restarts or is rescheduled.**

### PostgreSQL

Setting `database.type: postgres` switches to an external PostgreSQL instance. The default connection URL in the Helm chart is:

```
postgres://postgres:kagent@pgsql-postgresql.kagent.svc.cluster.local:5432/postgres
```

### Configuration

| Source | Variable / Flag | Default |
|--------|----------------|---------|
| Env | `DATABASE_TYPE` | `sqlite` |
| Env | `SQLITE_DATABASE_PATH` | `/sqlite-volume/kagent.db` (Helm) or `./kagent.db` (binary) |
| Env | `POSTGRES_DATABASE_URL` | *(see Helm values)* |
| Flag | `--database-type` | `sqlite` |
| Flag | `--sqlite-database-path` | `./kagent.db` |
| Flag | `--postgres-database-url` | `postgres://postgres:kagent@db.kagent.svc.cluster.local:5432/crud` |

## Data Retention

kagent has no built-in data retention, cleanup, or garbage collection. All rows grow indefinitely.

The API exposes soft-delete endpoints:

| Endpoint | Effect |
|----------|--------|
| `DELETE /api/sessions/{id}` | Sets `deleted_at` on the session row (does not cascade) |
| `DELETE /api/tasks/{id}` | Sets `deleted_at` on the task row |

Soft-deleted rows remain on disk; they are excluded from queries but not removed.

## Related Files

- [`models.go`](../../go/pkg/database/models.go) — GORM struct definitions (schema source of truth)
- [`client.go`](../../go/internal/database/client.go) — Database client implementation
- [`manager.go`](../../go/internal/database/manager.go) — Database connection and `AutoMigrate`
- [`app.go`](../../go/pkg/app/app.go) — CLI flags and environment variable mapping
- [`values.yaml`](../../helm/kagent/values.yaml) — Helm chart defaults
- [`controller-deployment.yaml`](../../helm/kagent/templates/controller-deployment.yaml) — Volume and mount definitions
