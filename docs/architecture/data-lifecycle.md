# Task, Session, and Event Data Lifecycle

This document describes how kagent stores task, session, and event data, the default storage configuration, and recommendations for production deployments.

## Overview

kagent persists all task, session, and event data in a relational database managed by GORM. By default it uses SQLite backed by a memory-based `emptyDir` volume. PostgreSQL is also supported for production or high-availability deployments.

## Data Model

kagent stores the following entities:

| Entity | Table | Description |
|--------|-------|-------------|
| **Session** | `session` | A conversation context between a user and an agent |
| **Task** | `task` | An A2A task (agent invocation) within a session |
| **Event** | `event` | A message or event within a session |
| **Agent** | `agent` | Agent configuration |
| **Push Notification** | `push_notification` | Push notification config for a task |
| **Feedback** | `feedback` | User feedback on agent responses |
| **Tool** | `tool` | Tool metadata from MCP servers |
| **ToolServer** | `toolserver` | Registered MCP tool servers |
| **LangGraph Checkpoint** | `lg_checkpoint` | LangGraph agent state checkpoints |
| **LangGraph Checkpoint Write** | `lg_checkpoint_write` | Individual write operations for checkpoints |
| **CrewAI Agent Memory** | `crewai_agent_memory` | Long-term memory for CrewAI agents |
| **CrewAI Flow State** | `crewai_flow_state` | Flow execution state for CrewAI agents |

All tables include `created_at`, `updated_at`, and `deleted_at` (soft-delete) timestamp columns managed by GORM.

### How Data Is Created

1. **Sessions** are created when a user starts a new conversation with an agent via the API (`POST /api/sessions`).
2. **Tasks** are created when an A2A task is submitted within a session. The full task payload is serialized as JSON in the `data` column.
3. **Events** are created for each message exchanged during a session (user messages, agent responses, tool calls). The `data` column stores the serialized `protocol.Message`.
4. **Checkpoints** and **memory** records are created by the agent framework (LangGraph or CrewAI) during task execution to persist intermediate state.

### Data Relationships

```text
Agent (1) ──── (*) Session (1) ──── (*) Task
                    │                     │
                    └──── (*) Event       └──── (*) PushNotification
```

> **Note:** There are no foreign key cascade constraints between sessions, tasks, and events. Deleting a session does **not** automatically delete its associated tasks or events. The only cascade constraint is on `Feedback.MessageID` (`OnDelete:CASCADE`).

## Default Storage Configuration

### Kubernetes (Helm Chart)

By default, the Helm chart provisions SQLite on a **memory-backed `emptyDir`** volume:

```yaml
# helm/kagent/values.yaml
database:
  type: sqlite
  sqlite:
    databaseName: kagent.db
```

The deployment template mounts this as:

```yaml
volumes:
  - name: sqlite-volume
    emptyDir:
      sizeLimit: 500Mi
      medium: Memory
```

- **Mount path:** `/sqlite-volume/`
- **Database file:** `/sqlite-volume/kagent.db`
- **Size limit:** 500Mi (from RAM)
- **Persistence:** Data is **lost** when the pod restarts

This configuration is suitable for quick demos and local experimentation but **not for production use**.

### Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_TYPE` | `sqlite` | Database backend (`sqlite` or `postgres`) |
| `SQLITE_DATABASE_PATH` | `/sqlite-volume/kagent.db` (Helm) or `./kagent.db` (binary) | Path to SQLite file |
| `POSTGRES_DATABASE_URL` | `postgres://postgres:kagent@pgsql-postgresql.kagent.svc.cluster.local:5432/postgres` | PostgreSQL connection URL |

### CLI Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--database-type` | `sqlite` | Database backend |
| `--sqlite-database-path` | `./kagent.db` | SQLite file path |
| `--postgres-database-url` | (see above) | PostgreSQL connection URL |

## Data Retention

### Current Behavior

**kagent has no built-in data retention, cleanup, or garbage collection mechanisms.** All task, session, event, checkpoint, and memory data grows indefinitely unless explicitly managed by the operator.

Available manual deletion operations:

| Operation | API Endpoint | What It Deletes |
|-----------|-------------|-----------------|
| Delete session | `DELETE /api/sessions/{session_id}` | Session record only (soft delete) |
| Delete task | `DELETE /api/tasks/{task_id}` | Task record only (soft delete) |

> **Important:** GORM soft-deletes set `deleted_at` but do not remove rows from the database. The data still occupies storage. Soft-deleted records are excluded from queries but remain on disk.

### What Grows Fastest

In typical usage, the **events table** grows the fastest because every message in every conversation creates a new row with a full JSON payload. The **LangGraph checkpoint** tables can also grow significantly since each agent execution step may create checkpoint records.

### Safe-to-Prune Data

The following categories of data can generally be pruned without affecting active operations:

- **Soft-deleted records** (`deleted_at IS NOT NULL`) — already excluded from queries
- **Old events** — historical conversation messages no longer needed for active sessions
- **Old checkpoints** (`lg_checkpoint`, `lg_checkpoint_write`) — only the most recent checkpoint is needed for resuming an agent; older checkpoints can be removed
- **Completed tasks** — tasks in a terminal state that are no longer referenced

> **Caution:** Always verify that sessions are not actively in use before pruning their associated events. Deleting events from an active session will cause data loss.

## Production Recommendations

### 1. Switch to PostgreSQL

For any deployment beyond local experimentation, migrate to PostgreSQL:

```yaml
# values.yaml
database:
  type: postgres
  postgres:
    url: postgres://user:password@your-postgres-host:5432/kagent
```

Benefits:

- Persistent storage that survives pod restarts
- Better concurrency and query performance
- Standard backup and replication tools (`pg_dump`, WAL archiving, etc.)
- Ability to run multiple controller replicas (SQLite only supports `replicas: 1`)

### 2. Switch SQLite to Disk-Backed Storage

If you must use SQLite, at minimum switch from memory-backed to disk-backed storage:

**Option A — Disk-backed emptyDir** (data still lost on pod deletion, but survives container restarts and does not consume RAM):

```yaml
volumes:
  - name: sqlite-volume
    emptyDir:
      sizeLimit: 10Gi
```

**Option B — PersistentVolumeClaim** (data persists across pod restarts):

```yaml
controller:
  volumes:
    - name: sqlite-volume
      persistentVolumeClaim:
        claimName: kagent-sqlite-pvc
  volumeMounts:
    - name: sqlite-volume
      mountPath: /sqlite-volume
```

### 3. Implement External Retention Policies

Since kagent does not include built-in retention, operators should implement their own. Example SQL for PostgreSQL:

```sql
-- Delete soft-deleted records older than 30 days
DELETE FROM event WHERE deleted_at IS NOT NULL AND deleted_at < NOW() - INTERVAL '30 days';
DELETE FROM task WHERE deleted_at IS NOT NULL AND deleted_at < NOW() - INTERVAL '30 days';
DELETE FROM session WHERE deleted_at IS NOT NULL AND deleted_at < NOW() - INTERVAL '30 days';

-- Prune events older than 90 days (adjust to your needs)
DELETE FROM event WHERE created_at < NOW() - INTERVAL '90 days';

-- Prune old checkpoints
DELETE FROM lg_checkpoint WHERE created_at < NOW() - INTERVAL '30 days';
DELETE FROM lg_checkpoint_write WHERE created_at < NOW() - INTERVAL '30 days';
```

For SQLite deployments where direct SQL access may be limited in Kubernetes, use the REST API to periodically list and delete old sessions.

### 4. Monitor Storage Usage

- Monitor the SQLite database file size or PostgreSQL table sizes
- Set alerts when storage exceeds thresholds (e.g., 80% of the `emptyDir` size limit)
- The default 500Mi memory-backed `emptyDir` can fill up quickly under normal usage

### 5. Back Up Your Data

- **PostgreSQL:** Use standard tools (`pg_dump`, WAL archiving, managed database backups)
- **SQLite with PVC:** Schedule periodic copies of the database file
- **SQLite with emptyDir:** Data cannot be reliably backed up (it is ephemeral)

## Related Files

- [models.go](../../go/pkg/database/models.go) — Database models (schema definitions)
- [client.go](../../go/internal/database/client.go) — Database client implementation
- [manager.go](../../go/internal/database/manager.go) — Database connection and initialization
- [service.go](../../go/internal/database/service.go) — Database service helpers
- [app.go](../../go/pkg/app/app.go) — Application configuration (database flags and env vars)
- [values.yaml](../../helm/kagent/values.yaml) — Helm chart default values
- [controller-deployment.yaml](../../helm/kagent/templates/controller-deployment.yaml) — Controller deployment template
