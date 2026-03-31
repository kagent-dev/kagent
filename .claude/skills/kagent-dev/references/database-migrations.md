# Database Migrations Guide

kagent uses [golang-migrate](https://github.com/golang-migrate/migrate) with embedded SQL files. Migrations run as a Kubernetes **init container** (`kagent-migrate`) before the controller starts.

## Structure

```
go/core/pkg/migrations/
├── migrations.go          # Embeds the FS (go:embed)
├── core/                  # Core schema (tracked in schema_migrations table)
│   ├── 000001_initial.up.sql / .down.sql
│   ├── 000002_add_session_source.up.sql / .down.sql
│   └── ...
└── vector/                # pgvector schema (tracked in vector_schema_migrations table)
    ├── 000001_vector_support.up.sql / .down.sql
    └── ...
```

The `kagent-migrate` binary (in `go/core/cmd/migrate/`) runs `up` by default. It manages two independent tracks — `core` and `vector` — and rolls back both if either fails.

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

Every `.up.sql` must have a corresponding `.down.sql` that exactly reverses it. Down migrations are used by the `kagent-migrate down --steps N --track core` command for rollbacks, and by automatic rollback on migration failure. They must be **idempotent** — the two-track rollback logic (roll back core if vector fails) may call them more than once in failure scenarios.

## Multi-Instance Safety

### How the advisory lock works

golang-migrate acquires a PostgreSQL **session-level** advisory lock (`pg_advisory_lock`) before running.

### Init container concurrency

If multiple pods start simultaneously (e.g., rolling deploy with replicas > 1):
1. One init container acquires the advisory lock and runs migrations.
2. Others block on `pg_advisory_lock`.
3. When the winner finishes and its connection closes, the next waiter acquires the lock, calls `Up()`, gets `ErrNoChange`, and exits immediately.

This is safe. The only risk is if the winning init container crashes mid-migration (see Dirty State below).

### Dirty state recovery

If `kagent-migrate` crashes mid-migration (OOMKill, pod eviction), golang-migrate records the version as `dirty = true` in the tracking table. The next run (after the advisory lock releases) will detect dirty state and call `rollbackToVersion`, which:
1. Calls `mg.Force(version - 1)` to clear the dirty flag.
2. Runs the down migration to restore the previous clean state.
3. Re-runs the failed up migration.

**Requirement**: down migrations must be idempotent and correctly reverse their up migration. A missing or broken down migration requires manual recovery — see the `force` subcommand below.


### Rollout strategy

For additive, backward-compatible migrations a rolling update is safe:

1. New pod starts → `kagent-migrate up` runs (advisory lock serializes concurrent runs)
2. New pod passes readiness probe → old pod terminates
3. Backward-compatible schema means old pods continue operating during the window

For a migration that is **not** backward-compatible, restructure it using expand/contract.

## Running Migrations Locally

```bash
# Apply all pending migrations
POSTGRES_DATABASE_URL="postgres://..." kagent-migrate up

# Check current version on each track
POSTGRES_DATABASE_URL="..." kagent-migrate version

# Roll back 1 step on core track
POSTGRES_DATABASE_URL="..." kagent-migrate down --steps 1 --track core

# With vector support
KAGENT_DATABASE_VECTOR_ENABLED=true POSTGRES_DATABASE_URL="..." kagent-migrate up
```

### Manual dirty-state recovery

If a migration was partially applied (dirty state), use `force` to reset to the last clean version before running `down`:

```bash
# Force the tracking table to a specific version (clears dirty flag)
POSTGRES_DATABASE_URL="..." kagent-migrate force <version> --track core
# Then re-run up, or roll back:
POSTGRES_DATABASE_URL="..." kagent-migrate down --steps 1 --track core
```

In a Kubernetes deployment, the init container runs automatically on every pod start.
