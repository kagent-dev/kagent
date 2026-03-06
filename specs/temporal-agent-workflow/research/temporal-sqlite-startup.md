# Temporal Server SQLite Startup Fix

## Problem

The original Helm template for `temporal-server-deployment.yaml` used env vars (`DB=sqlite`, `SQLITE_DB_PATH`) to configure the `temporalio/auto-setup` image for SQLite mode. This does not work -- the `auto-setup` image only supports PostgreSQL/MySQL via env vars, not SQLite.

## Root Cause

The `temporalio/auto-setup` Docker image runs the full Temporal server with auto-schema setup. Its entrypoint script recognizes `DB=postgres12` or `DB=mysql8` and configures the corresponding database driver. There is no `DB=sqlite` support in the auto-setup entrypoint.

SQLite is only supported by the **Temporal CLI dev server** (`temporal server start-dev`), which is a lightweight single-process server intended for development.

## Solution

**SQLite mode (dev):** Override `command` and `args` to run the Temporal CLI dev server directly:

```yaml
command: ["temporal"]
args:
  - "server"
  - "start-dev"
  - "--headless"        # no built-in UI (separate UI container)
  - "--ip"
  - "0.0.0.0"          # bind all interfaces (required in K8s)
  - "--port"
  - "7233"
  - "--db-filename"
  - "/temporal-data/temporal.db"
  - "--namespace"
  - "kagent"            # auto-create namespace on startup
```

Key flags:
- `--headless` -- disables built-in UI server (we deploy Temporal UI separately)
- `--ip 0.0.0.0` -- required for K8s service routing (default is `127.0.0.1`)
- `--db-filename` -- persists to emptyDir volume (data lost on pod restart, acceptable for dev)
- `--namespace` -- pre-creates the Temporal namespace, avoiding manual `tctl` setup

No `env:` block needed for SQLite mode -- all config via CLI args.

**PostgreSQL mode (prod):** Uses `temporalio/auto-setup` with corrected env vars:

| Env Var | Correct Value | Previous (Wrong) |
|---------|--------------|-------------------|
| `DB` | `postgres12` | `postgres` |
| `DB_PORT` | `5432` | `POSTGRES_PORT` |
| `DBNAME` | `temporal` | `POSTGRES_DB` |
| `POSTGRES_SEEDS` | host | (correct) |
| `POSTGRES_USER` | user | (correct) |
| `POSTGRES_PWD` | password | (correct) |

The `env:` block is conditionally rendered only for PostgreSQL mode.

## Validated

Tested on Kind cluster -- Temporal server starts successfully in SQLite mode, gRPC port 7233 becomes ready, namespace `kagent` auto-created.
