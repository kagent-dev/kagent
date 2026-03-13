---
status: Implemented
created: 2026-03-12
updated: 2026-03-13
authors:
  - Jeremy Alvis
  - Claude (Design Partner)
---
# Drop SQLite — Postgres-Only Database

## Overview

Kagent currently supports two database backends: SQLite (the default) and PostgreSQL. This doc covers removing SQLite entirely so that PostgreSQL is the only supported backend, for both production deployments and local Kind clusters.

This is in preparation for replacing GORM with explicit SQL migrations and sqlc. The two efforts are independent and can be sequenced in either order, but removing SQLite first simplifies the ORM migration by eliminating the need for per-dialect SQL files and a second sqlc configuration.

## Goals and Non-Goals

### Goals

- PostgreSQL is the only supported database backend
- Local Kind cluster development (`make helm-install`) deploys a bundled PostgreSQL instance automatically — no manual prerequisites
- Unit tests use testcontainers-go to spin up a real Postgres instance (no in-memory SQLite)
- `--database-vector-enabled` flag is retained (default `true`) so external PostgreSQL users without pgvector can opt out of memory features without a hard crash at startup
- SQLite Go dependencies (`glebarez/sqlite`, `turso.tech/tursogo`) are removed from `go.mod`
- CI E2E matrix simplified — `database: [sqlite, postgres]` matrix removed entirely (single-value matrices serve no purpose)

## Current State (Before This Change)

### What uses SQLite today

| Location | What it does |
|----------|--------------|
| `go/core/internal/database/manager.go` | Switches between SQLite and Postgres based on `--database-type` flag; SQLite path uses Turso driver, sets `MaxOpenConns(1)`, manual vector table DDL using `F32_BLOB(768)` |
| `go/core/internal/database/client.go` | `SearchAgentMemory` branches on `db.Name() == "sqlite"` to use `vector_distance_cos(embedding, vector32(?))` instead of pgvector `<=>` operator; `deleteAgentMemoryByQuery` uses a Turso-specific two-step delete (Pluck IDs + `DELETE WHERE id IN ?`) to avoid a libSQL multi-index scan bug; `SearchCrewAIMemoryByTask` uses `JSON_EXTRACT` (MySQL syntax) in both the WHERE clause and ORDER BY — this is a pre-existing bug that is currently masked because the Postgres E2E tests don't exercise this path |
| `go/core/internal/database/client_test.go` | All unit tests use in-memory SQLite (`:memory:`) via two helpers: `setupTestDB` (line 205) and `setupVectorTestDB` (line 319) |
| `go/core/pkg/app/app.go` | Registers `--database-type`, `--sqlite-database-path`, `--database-vector-enabled` flags; default is `sqlite` |
| `helm/kagent/values.yaml` | `database.type: sqlite` is the default; SQLite path has a `vectorEnabled` flag |
| `helm/kagent/templates/controller-deployment.yaml` | Conditionally creates an `emptyDir` volume (500Mi memory) and mounts it at `/sqlite-volume` when `database.type == "sqlite"`; sets `XDG_CACHE_HOME=/sqlite-volume/.cache` |
| `helm/kagent/templates/controller-configmap.yaml` | Conditionally sets `SQLITE_DATABASE_PATH` and `DATABASE_VECTOR_ENABLED` env vars |
| `.github/workflows/ci.yaml` | E2E test matrix: `database: [sqlite, postgres]` |
| `go/go.mod` | `github.com/glebarez/sqlite v1.11.0`, `turso.tech/database/tursogo v0.5.0-pre.13` |
| `contrib/addons/postgres.yaml` | Optional dev-only addon; `make kagent-addon-install` installs it alongside Grafana and Prometheus |

### Inconsistent Postgres default URLs

Three places defined a default Postgres URL and none of them agreed:

| Location | Before | After |
|----------|--------|-------|
| `helm/kagent/values.yaml` | `pgsql-postgresql.kagent.svc.cluster.local:5432/postgres` | `""` — auto-computed from bundled service (`kagent-postgresql`) |
| `go/core/pkg/app/app.go` flag default | `db.kagent.svc.cluster.local:5432/crud` | `kagent-postgresql.kagent.svc.cluster.local:5432/postgres` |
| `DEVELOPMENT.md` | `postgres.kagent.svc.cluster.local:5432/kagent` | Removed — postgres is now deployed by `make helm-install` |

## Changes

### 1. Go: Remove SQLite from manager.go

- Remove `DatabaseTypeSqlite` constant and `SqliteConfig` struct
- Remove the SQLite case from the database init switch (Turso driver open, `SetMaxOpenConns(1)`)
- Remove the SQLite vector table DDL block (the `F32_BLOB` / `idx_memory_agent_user` creation)
- The Postgres path is now the only path; direct initialization replaces the switch

### 2. Go: Remove SQLite branch and fix Postgres bugs in client.go

- Remove the `if db.Name() == "sqlite"` branch in `SearchAgentMemory` — the pgvector `<=>` cosine similarity path is the only path
- Simplify `deleteAgentMemoryByQuery` — remove the two-step Pluck + `DELETE WHERE id IN ?` workaround. The workaround was specific to a Turso multi-index scan bug and is unnecessary on Postgres
- Fix `SearchCrewAIMemoryByTask` — replace `JSON_EXTRACT(memory_data, '$.task_description')` and `JSON_EXTRACT(memory_data, '$.score')` with the Postgres JSONB operator equivalent (`memory_data->>'task_description'`, `memory_data->>'score'`). `JSON_EXTRACT` is MySQL syntax and errors on Postgres. This is a pre-existing bug masked by the SQLite E2E tests.

### 3. Go: Update app.go flags and manager.go config structs

**app.go:**
- Remove `--database-type` flag and `DATABASE_TYPE` env var
- Remove `--sqlite-database-path` flag and `SQLITE_DATABASE_PATH` env var
- Retain `--database-vector-enabled` flag (default `true`) — see [decision note below](#decision-retain---database-vector-enabled)
- Fix the `--postgres-database-url` default to match the bundled Helm postgres service

**manager.go:**
- Retain `PostgresConfig.VectorEnabled` field (same rationale)
- The `if VectorEnabled` guard controls `CREATE EXTENSION IF NOT EXISTS vector`, memory table migration, and HNSW index creation

### 4. Go: Replace in-memory SQLite unit tests with testcontainers-go

- Add `github.com/testcontainers/testcontainers-go` and `testcontainers-go/modules/postgres` to `go/core/go.mod`
- Create `go/core/internal/dbtest/dbtest.go` — shared helper that starts a `pgvector/pgvector:pg18-trixie` container with `testcontainers.WithReuseByName("kagent-dbtest-postgres")` and a `wait.ForLog` readiness strategy (waits for `"database system is ready to accept connections"` twice)
- `TestMain` in `database/` package starts the shared container once; `setupTestDB` calls `Reset(true)` between tests instead of creating a new manager per test — avoids "connection reset by peer" from rapid pool cycling
- `testing.Short()` skips database tests when run without Docker; `flag.Parse()` called before `testing.Short()` in `TestMain` (required by the testing package)

### 5. Go: Remove SQLite dependencies

Remove from `go/go.mod`:
- `github.com/glebarez/sqlite v1.11.0`
- `github.com/glebarez/go-sqlite v1.21.2` (indirect)
- `turso.tech/database/tursogo v0.5.0-pre.13`

### 6. Helm: Remove SQLite config, add bundled PostgreSQL

**`helm/kagent/values.yaml`:**
- Remove `database.type` field and `database.sqlite` section
- `database.postgres.url` defaults to `""` — leave empty to use the bundled postgres, set to use external
- Retain `database.postgres.urlFile` — takes precedence over `url` when set
- Retain `database.postgres.vectorEnabled: true`
- Add `database.postgres.bundled` sub-key for bundled instance config (image, storage, database, user, password)

**`helm/kagent/templates/controller-deployment.yaml`:**
- Remove the `{{- if eq .Values.database.type "sqlite" }}` conditional volume block (emptyDir, 500Mi)
- Remove the volumeMount block (`/sqlite-volume`) and `XDG_CACHE_HOME` env var

**`helm/kagent/templates/controller-configmap.yaml`:**
- Remove `DATABASE_TYPE` and `SQLITE_DATABASE_PATH` entries
- `POSTGRES_DATABASE_URL_FILE` set when `urlFile` is non-empty; otherwise `POSTGRES_DATABASE_URL` uses `include "kagent.postgresqlUrl"` helper

**`helm/kagent/templates/_helpers.tpl`:**
- Remove `kagent.validateController` helper (blocked renders when `replicas > 1 AND database.type == "sqlite"`)
- Add `kagent.postgresqlServiceName` and `kagent.postgresqlUrl` helpers — `postgresqlUrl` returns `database.postgres.url` when set, otherwise auto-computes from bundled config

**`helm/kagent/templates/postgresql.yaml`** (new):
- Templated version of the former `contrib/addons/postgres.yaml`
- Renders when `database.postgres.url` and `database.postgres.urlFile` are both empty
- Deploys ConfigMap, PVC, Deployment, and Service for `pgvector/pgvector:pg18-trixie`

**`helm/kagent/tests/controller-deployment_test.yaml`:**
- Remove test case "should fail when replicas > 1 and database type is sqlite"
- Remove SQLite volume/mount test cases

### 7. CI: Remove E2E matrix

Remove `strategy.matrix` from `test-e2e` entirely. A `database: [postgres]` single-value matrix runs the job once with no variation — it is equivalent to no matrix. Remove the Postgres service container block as well; postgres is deployed inside the Kind cluster by `make helm-install`.

### 8. Local Kind setup: Postgres bundled in Helm chart

**Decision: built-in chart templates** (`helm/kagent/templates/postgresql.yaml`).

Options considered:

| Option | Decision |
|--------|----------|
| Built-in chart templates (chosen) | Move `contrib/addons/postgres.yaml` into `helm/kagent/templates/`. Renders when `database.postgres.url` and `urlFile` are both empty — the same condition users already knew: leave `url` empty for local dev, set it for production. No external deps, no separate install step. Works with Argo CD, Flux, and any Helm-based deploy, not just the Makefile. |
| Bitnami postgresql subchart | More feature-rich (backups, HA, etc.) but heavy for a dev dependency and ties the chart to an external chart release cycle. Overkill for a bundled dev/default postgres. |
| Separate kagent-postgres chart | Mirrors the kagent-crds pattern. Cleaner separation but adds install complexity (`helm install kagent-postgres` then `helm install kagent`). |
| Keep in contrib, Makefile applies it | What existed before. Works for local dev but breaks pure Helm installs -- i.e. anyone not using the Makefile. |

Production users set `database.postgres.url` (or `database.postgres.urlFile` for Secret-mounted credentials) — the same configuration they already provided. The bundled postgres simply does not deploy.

Delete `contrib/addons/postgres.yaml`. Remove postgres from `make kagent-addon-install`.

## Decision: Retain `--database-vector-enabled`

The original design called for removing this flag (pgvector always-on). Retain it for the following reason:

Without the flag, any external PostgreSQL instance without the pgvector extension installed causes the controller to crash at startup with `failed to create vector extension`. This is a hard prerequisite that external users may not anticipate.

With the flag (default `true`), users running external Postgres without pgvector can set `database.postgres.vectorEnabled: false` (or `DATABASE_VECTOR_ENABLED=false`) to skip extension creation and run without memory/vector support. This is a softer failure mode and a better operator experience.

The bundled postgres uses `pgvector/pgvector:pg18-trixie` and always has the extension available, so the default `true` is correct for the common case.
