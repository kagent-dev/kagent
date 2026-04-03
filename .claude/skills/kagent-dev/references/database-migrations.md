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

### Backward-compatible schema changes

During a rolling deploy, old pods will be reading and writing a schema that has already been upgraded. **Every migration must be backward-compatible with the previous version's code.**

| Change | Old code behavior | Safe? |
|--------|------------------|-------|
| Add nullable column | SELECT ignores it; INSERT omits it (goes NULL) | ✅ |
| Add column with `DEFAULT x` | INSERT omits it; DB fills default | ✅ |
| Add NOT NULL column **without** default | Old INSERT missing the column → error | ❌ |
| Add index | Invisible to application code | ✅ |
| Add foreign key | Old INSERT may fail constraint | ❌ |
| Drop/rename column old code references | Old SELECT/INSERT errors | ❌ |
| Change compatible type (e.g. `int` → `bigint`) | Usually fine | ⚠️ |

**Expand-then-contract pattern for schema changes:**
1. **Version N (Expand)**: add the new column/table (nullable or with default); old code still works
2. **Version N (Deploy)**: ship new code that uses the new structure
3. **Version N+1 (Contract)**: drop the old column/table once version N is fully deployed and no pods run version N-1

### Idempotency and cross-track safety

All DDL statements must use `IF EXISTS` / `IF NOT EXISTS` guards:

```sql
-- Up
CREATE TABLE IF NOT EXISTS foo (...);
ALTER TABLE foo ADD COLUMN IF NOT EXISTS bar TEXT;

-- Down
DROP TABLE IF EXISTS foo;
ALTER TABLE foo DROP COLUMN IF EXISTS bar;
```

Guards provide defense-in-depth for crash recovery and dirty-state cleanup, where a partially-applied migration may be re-run or rolled back.

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

For backward-compatible migrations a rolling update is safe:

1. New pod starts → migration runner applies pending migrations (advisory lock serializes concurrent runs)
2. New pod passes readiness probe → old pod terminates
3. Backward-compatible schema means old pods continue operating during the window

For a migration that is **not** backward-compatible, restructure it using the expand-then-contract pattern (add new column/table in version N, ship code that uses it, drop the old column in version N+1).

## Static Analysis Enforcement

The policies above are enforced by static analysis tests in `go/core/pkg/migrations/cross_track_test.go`. These run against the embedded SQL files — no database required.

| Test | What it enforces |
|------|-----------------|
| `TestNoCrossTrackDDL` | No track may `ALTER TABLE` or `CREATE INDEX ON` a table owned by another track |
| `TestMigrationGuards` | Up migrations must use `IF NOT EXISTS` on all `CREATE`/`ADD COLUMN`; down migrations must use `IF EXISTS` on all `DROP` statements |

**Adding a new track**: add the track directory name to the `tracks` slice in each test so the new track is covered by the same checks.

These tests catch policy violations at PR time without needing a running database. They complement the integration tests in `runner_test.go`, which verify the runner's rollback and concurrency behavior against a real Postgres instance.

## Downstream Extension Model

The migration layer is designed for downstream consumers to extend with their own migrations alongside OSS. The extension points are:

1. **SQL files as the contract.** The migration files in `go/core/pkg/migrations/core/` and `vector/` are the stable interface. Downstream consumers sync these files into their own repos and build their own migration runners. Don't move or reorganize migration file paths without considering downstream impact.

2. **`MigrationRunner` DI callback.** Downstream consumers pass a custom `MigrationRunner` to `app.Start` to take full ownership of the migration process — running OSS migrations alongside their own in whatever order they need. The signature `func(ctx context.Context, url string, vectorEnabled bool) error` is stable.

3. **Vector track stays separate.** The vector track is conditionally applied and has its own tracking table. Downstream extensions should not modify vector-owned tables (enforced by `TestNoCrossTrackDDL`).

### What this means for OSS development

- **Migration immutability is cross-repo.** Once a migration file is merged and tagged, downstream consumers may have synced it. Modifying it breaks their tracking table state.
- **The `MigrationRunner` DI signature is stable.** Changes to this type are breaking for downstream consumers.
