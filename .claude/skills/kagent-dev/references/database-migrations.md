# Database Migrations Guide

kagent uses [golang-migrate](https://github.com/golang-migrate/migrate) with embedded SQL files and [sqlc](https://sqlc.dev/) for type-safe query generation. Migrations run **in-app at startup** — the controller applies them before accepting traffic.

## Structure

```
go/core/pkg/migrations/
├── migrations.go          # Embeds the FS (go:embed); exports FS for downstream consumers
├── runner.go              # RunUp (applies pending migrations at startup)
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

Migration files are append-only and immutable once merged (see [One Linear History](#one-linear-history)).

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

Because the hand-written queries are the source of truth for what code reads, sqlc's generated output makes "no current code reads this column" greppable — the check behind a contraction sign-off (see [Backward compatibility and contraction](#backward-compatibility-and-contraction)).

## Writing Migrations

### Backward compatibility and contraction

During a rolling deploy — and after a version rollback — old pods read and write a schema a newer release has already migrated. **The default for every migration is backward-compatible: nothing a prior release's code reads or writes may stop working.** "Additive-only" is the usual shorthand, but it is imprecise — some additive DDL still breaks old code. State the rule by *effect*, not by DDL shape.

| Change | Old code behavior | Safe? |
|--------|------------------|-------|
| Add nullable column | SELECT ignores it; INSERT omits it (goes NULL) | ✅ |
| Add column with `DEFAULT x` | INSERT omits it; DB fills default | ✅ |
| Add index | Invisible to application code | ✅ |
| Widen a compatible type (e.g. `int` → `bigint`) | Usually fine | ⚠️ |
| Add NOT NULL column **without** default | Old INSERT missing the column → error | ❌ |
| New constraint on a shipped table (FK / `UNIQUE` / `CHECK`) | Old writer violates it → error | ❌ |
| Narrow a column type | Existing/old-code value may no longer fit | ❌ |
| Drop or rename a column/table old code uses | Old SELECT/INSERT errors | ❌ |
| Rewrite stored rows into a new format | Old reader can't parse the new format | ❌ |

This is exactly what makes a rollback safe: when an operator redeploys the previous release against a database the newer release already migrated, the old code's queries still run because no contraction has shipped (see [Rollback and ahead-schema tolerance](#rollback-and-ahead-schema-tolerance)).

The last ❌ row is easy to miss: a migration — **or an out-of-band tool** — that rewrites existing rows breaks old readers the same way a `DROP COLUMN` does, with no DDL for static analysis to catch. A data rewrite is a contraction regardless of its SQL.

**Contractions are not banned — they are windowed.** Anything in the ❌ rows is a *contraction*. Forever-backward-compatible is not tenable (dead weight accumulates without bound), so a breaking change is split across minor versions such that no supported rollback target ever lands on code that needs the removed structure:

1. **Minor `X.Y` (expand):** add the new column/table (nullable or with default). Old code still works.
2. **Minor `X.Y` (deploy):** ship the code that uses the new structure.
3. **Minor `X.(Y+1)` (contract):** drop the old column/table — safe because the furthest supported rollback from `X.(Y+1)` lands on minor `X.Y`, which already uses the new structure.

The **rollback window** is how far back a rollback is supported: **one minor back**. From `Major.Minor.Patch`, an operator may roll back to an earlier release in the current minor, or to the previous minor — the previous minor is the furthest-back supported target. A contraction is therefore safe to merge only once its replacement shipped at or before the previous minor, so no supported rollback can land on code that predates it.

**Destructive changes must be declared, not silent.** An intentional contraction is allowed only with explicit reviewer sign-off confirming (1) the replacement shipped in the prior release and (2) no current code still reads the old structure — sqlc makes that second point checkable for Postgres, since the generated queries are greppable. Pre-rule contractions already in history are grandfathered; the rule binds going forward.

> **Enforcement.** *Target — not yet enforced*: a static check extending `cross_track_test.go` will block undeclared destructive DDL at merge (see [Static Analysis Enforcement](#static-analysis-enforcement)).

### Schema-agnostic SQL

**Migration SQL must not name a schema.** The schema a migration lands in is chosen by the *connection* (its `search_path` / `current_schema`), not the file, so the same migration files apply into whatever schema the connection selects.

Forbidden in any migration file:

- `CREATE SCHEMA` / `DROP SCHEMA`
- schema-qualified DDL (`CREATE TABLE foo.bar`, `ALTER TABLE foo.bar ...`)
- `SET search_path`
- `ALTER ... SET SCHEMA`

```sql
-- ❌ pins every install to one hard-coded schema
CREATE TABLE myschema.eval_set (...);

-- ✅ lands in whatever schema the connection selects
CREATE TABLE IF NOT EXISTS eval_set (...);
```

Schema is a deploy-time choice, fixed by the connection rather than the migration file. A hard-coded schema breaks any deployment that runs the track in a different schema (e.g. a connection that sets `?search_path=<schema>`). The core and vector migrations comply today (verified by inspection until the lint lands).

> **Enforcement.** A static lint test rejecting the forbidden patterns across all migration files (*Target — not yet enforced*; see [Static Analysis Enforcement](#static-analysis-enforcement)).

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

A down file that never runs is a down file you cannot trust. There are no up-only migrations — a working down has shipped with every migration since the golang-migrate adoption. Exercising every migration up → down → up against the real migration set, to prove the reversal rather than assume it, is a *Target — not yet enforced* (see [Upgrade and rollback testing](#upgrade-and-rollback-testing)).

## One Linear History

Migrations form a single, append-only sequence. Two rules keep it that way.

**Immutable from merge.** A migration file is never edited once it merges to `main` — not merely once it is released. The next build picks it up as soon as it merges, so editing it would diverge the schema state of any database that already applied it. A bug in a migration is fixed by a **new** compensating migration, never by editing the original in place.

**Sequence numbers are claimed at merge.** The 6-digit number is allocated when the migration lands on `main`. A feature branch carrying a draft migration **renumbers** before merging if `main` has moved on, so the sequence never forks or collides.

A backward-compatible migration may ship in the **same PR** as the code that uses it — the migration is additive, so old code tolerates the new schema during the rollout.

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

If the controller crashes mid-migration, golang-migrate leaves the tracking table marked `dirty = true`. On the next startup `Up` refuses to run against a dirty database, so the runner clears the flag: `mg.Force(version - 1)` resets the tracking table to the last clean version. The process then exits with the error, and the failed migration is re-applied on a **subsequent** startup once the database is clean — so recovery from a transient failure spans restarts rather than completing in a single run.

**Requirement**: down migrations must be idempotent and correctly reverse their up migration. A missing or broken down migration requires manual recovery.

### Rollout strategy

For backward-compatible migrations a rolling update is safe:

1. New pod starts → migration runner applies pending migrations (advisory lock serializes concurrent runs)
2. New pod passes readiness probe → old pod terminates
3. Backward-compatible schema means old pods continue operating during the window

For a migration that is **not** backward-compatible, restructure it using the expand-then-contract pattern (add new column/table in version N, ship code that uses it, drop the old column in version N+1).

## Rollback and ahead-schema tolerance

Two distinct events both get called "rollback."

**A migration fails mid-upgrade.** The runner reverts the in-flight migration automatically and the process exits, leaving the database at the last clean version (see [Dirty state recovery](#dirty-state-recovery)). This has always worked.

**A version rollback after a successful upgrade.** A regression turns up and the operator redeploys the previous release against a database the newer release already migrated forward. The runner **tolerates a database ahead of it** — it sees a higher-than-known version, accepts it, and starts.

Tolerating an ahead database is only safe because of the [backward-compatibility window](#backward-compatibility-and-contraction): inside the window no contraction has shipped, so the old code's queries run against the newer schema by construction. The schema simply stays expanded until the operator re-upgrades. The server does **no** version arithmetic at startup — staying within the supported rollback window (one minor back) is the operator's responsibility, not a runtime check.

Down migrations are off this routine path. They are still authored and still run — by the failure-revert above, and for a deliberate schema reversal (run from the newer release, which ships the down files) — but a routine in-window version rollback touches no down file.

## Static Analysis Enforcement

The policies above are enforced by static analysis tests in `go/core/pkg/migrations/cross_track_test.go`. These run against the embedded SQL files — no database required.

| Test | What it enforces |
|------|-----------------|
| `TestNoCrossTrackDDL` | No track may `ALTER TABLE` or `CREATE INDEX ON` a table owned by another track |
| `TestMigrationGuards` | Up migrations must use `IF NOT EXISTS` on all `CREATE`/`ADD COLUMN`; down migrations must use `IF EXISTS` on all `DROP` statements |
| Contraction guard *(target)* | Blocks undeclared destructive DDL — `DROP`/`RENAME` of shipped objects, type narrowing, new constraints on shipped tables (see [Backward compatibility and contraction](#backward-compatibility-and-contraction)) |
| Schema-agnostic lint *(target)* | Rejects `CREATE SCHEMA`, schema-qualified DDL, `SET search_path`, and `ALTER ... SET SCHEMA` (see [Schema-agnostic SQL](#schema-agnostic-sql)) |

**Adding a new track**: add the track directory name to the `tracks` slice in each test so the new track is covered by the same checks.

These tests catch policy violations at PR time without needing a running database. They complement the integration tests in `runner_test.go`, which verify the runner's rollback and concurrency behavior against a real Postgres instance.

## Upgrade and rollback testing

Static analysis covers file *content*; round-trip tests cover *behavior* against a real Postgres. Beyond `runner_test.go` (rollback and concurrency), release-to-release coverage makes the rollback promise real.

**Previous-minor round-trip** (*Target — not yet enforced*). Seed a database at the previous minor's latest release with representative data, apply migrations up to `HEAD`, and assert the schema matches a clean `HEAD` install and the data survives; then reverse to the previous minor and assert the schema matches a clean previous-minor install and the data survives. This exercises every changed down file rather than only reviewing it.

**Query-level backward compatibility.** A static check — `scripts/check-query-contraction.sh`, run by the `query-contraction-check` CI job — compiles a previous release's sqlc queries against the `HEAD` schema and fails if a migration dropped, renamed, or retyped a column or table an older query still reads. It catches column/table/type-shape contraction with no database, against two prior versions: the latest release reachable from `HEAD` and the previous stable line's latest patch (the `release/vX.Y.x` tip, via `scripts/prev-stable-version.sh`). The fuller property — running the previous minor's whole database *test suite* against a `HEAD`-migrated schema, which also covers semantic breaks a query still compiles against — remains a *Target — not yet enforced*.

## Downstream Extension Model

The migration layer is designed for downstream consumers to extend with their own migrations. The extension points are:

1. **SQL files as the contract.** The migration files in `go/core/pkg/migrations/core/` and `vector/` are the stable interface. Downstream consumers sync these files into their own repos and build their own migration runners. Don't move or reorganize migration file paths without considering downstream impact.

2. **`MigrationRunner` DI callback.** Downstream consumers pass a custom `MigrationRunner` to `app.Start` to take full ownership of the migration process — running the core and vector migrations alongside their own in whatever order they need. The signature `func(ctx context.Context, url string, vectorEnabled bool) error` is stable.

3. **Vector track stays separate.** The vector track is conditionally applied and has its own tracking table. Downstream extensions should not modify vector-owned tables (enforced by `TestNoCrossTrackDDL`).

### What this means for development

- **Migration immutability is cross-repo.** [Immutability](#one-linear-history) binds from the moment a migration merges to `main`, not from release: downstream consumers may have synced a merged file before it ships. Modifying it breaks their tracking-table state.
