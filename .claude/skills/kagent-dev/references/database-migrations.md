# Database Migrations Guide

kagent uses [golang-migrate](https://github.com/golang-migrate/migrate) with embedded SQL files and [sqlc](https://sqlc.dev/) for type-safe query generation. Migrations run **in-app at startup** — the controller applies them before accepting traffic.

## Structure

```
go/core/pkg/migrations/
├── migrations.go          # Embeds the FS (go:embed); exports FS for downstream consumers
├── runner.go              # RunUp / RunDown / RunDownAll / RunVersion / RunForce
├── core/                  # Core schema (tracked in schema_migrations table)
│   ├── 000001_initial.up.sql / .down.sql
│   ├── 000002_add_session_source.up.sql / .down.sql
│   └── ...
└── vector/                # pgvector schema (tracked in vector_schema_migrations table)
    ├── 000001_vector_support.up.sql / .down.sql
    └── ...

go/core/internal/database/
├── queries/               # Hand-written SQL queries (source of truth)
│   ├── sessions.sql
│   ├── memory.sql
│   └── ...
├── gen/                   # sqlc-generated Go code — DO NOT edit manually
│   ├── db.go
│   ├── models.go
│   └── *.sql.go
└── sqlc.yaml              # sqlc configuration
```

Migrations manage two independent tracks — `core` and `vector` — and roll back both if either fails. The `--database-vector-enabled` flag (default `true`) controls whether the vector track runs.

## sqlc Workflow

When you add or change a SQL query:

1. Edit (or add) a `.sql` file under `go/core/internal/database/queries/`
2. Regenerate:
   ```bash
   cd go/core/internal/database && sqlc generate
   ```
3. Commit both the query file and the updated `gen/` files together.

A CI check (`.github/workflows/sqlc-generate-check.yaml`) fails the PR if `gen/` is out of sync with the queries. Never edit `gen/` by hand.

**sqlc annotations used:**
- `:one` — returns a single row
- `:many` — returns a slice
- `:exec` — returns only error (use for INSERT/UPDATE/DELETE that don't need the result)

## Writing Migrations

### Version compatibility policy

kagent supports **n-1 minor version** compatibility. Users must not skip minor versions when upgrading. This gives us a defined window for schema cleanup:

- **Version N**: stop using the old column/table in application code; the schema still contains it (backward compatible with N-1)
- **Version N+1**: drop the old column/table (or N+2 for additional safety if rollback risk is high)

Never migrate data and remove the old structure in the same migration — if the migration fails mid-way, rollback is much harder. Always separate the two steps across versions.

### Backward-compatible schema changes (expand/contract)

During a rolling deploy, old pods (running the previous code version) will be reading and writing a schema that has already been upgraded by the new pod's init container. **Every migration must be backward-compatible with the n-1 minor version's code.** Locking serializes concurrent migration runs but does nothing to protect old pods still running against the new schema.

| Change | Old code behavior | Safe? |
|--------|------------------|-------|
| Add nullable column | SELECT ignores it; INSERT omits it (goes NULL) | ✅ |
| Add column with `DEFAULT x` | INSERT omits it; DB fills default | ✅ |
| Add NOT NULL column **without** default | Old INSERT missing the column → error | ❌ |
| Add index | Invisible to application code | ✅ |
| Add foreign key | Old INSERT may fail constraint | ❌ |
| Drop/rename column old code references | Old SELECT/INSERT errors | ❌ |
| Change compatible type (e.g. `int` → `bigint`) | Usually fine | ⚠️ |

**Expand/contract pattern for destructive changes:**
1. **Version N (Expand)**: add the new column/table (nullable or with default); old code still works
2. **Version N (Deploy)**: ship new code that reads from the new structure, writes to both
3. **Version N+1 (Contract)**: drop the old column/table in a follow-on migration

Never drop a column or rename a column in the same release as the code change that stops using it.

### Naming

Files must follow `NNNNNN_description.up.sql` / `NNNNNN_description.down.sql` with zero-padded 6-digit sequence numbers.

### Down migrations

Every `.up.sql` must have a corresponding `.down.sql` that exactly reverses it. Down migrations are used for rollbacks and by automatic rollback on migration failure. They must be **idempotent** — the two-track rollback logic (roll back core if vector fails) may call them more than once in failure scenarios.

## Multi-Instance Safety

### How the advisory lock works

The migration runner acquires a PostgreSQL **session-level** advisory lock (`pg_advisory_lock`) before running.

### Rolling deploy concurrency

If multiple pods start simultaneously (e.g., rolling deploy with replicas > 1):
1. One controller acquires the advisory lock and runs migrations.
2. Others block on `pg_advisory_lock`.
3. When the winner finishes and its connection closes, the next waiter acquires the lock, calls `Up()`, gets `ErrNoChange`, and exits immediately.

This is safe. The only risk is if the winning controller crashes mid-migration (see Dirty State below).

### Dirty state recovery

If the controller crashes mid-migration, the migration runner records the version as `dirty = true` in the tracking table. The next startup detects dirty state and calls `rollbackToVersion`, which:
1. Calls `mg.Force(version - 1)` to clear the dirty flag.
2. Runs the down migration to restore the previous clean state.
3. Re-runs the failed up migration.

**Requirement**: down migrations must be idempotent and correctly reverse their up migration. A missing or broken down migration requires manual recovery using `RunForce`.

### Rollout strategy

For additive, backward-compatible migrations a rolling update is safe:

1. New pod starts → migration runner applies pending migrations (advisory lock serializes concurrent runs)
2. New pod passes readiness probe → old pod terminates
3. Backward-compatible schema means old pods continue operating during the window

For a migration that is **not** backward-compatible, restructure it using expand/contract.
