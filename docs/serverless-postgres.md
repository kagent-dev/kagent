# Serverless PostgreSQL

To let PostgreSQL become idle between bursts of kagent activity, configure both
background polling and connection-pool idle cleanup. A small pool alone does not
help if a worker queries the database every second. A long polling interval alone
does not promptly close idle pooled connections.

## Background intervals

Set these controller environment variables through Helm's `controller.env`.
Values use Go durations, such as `30s`, `5m`, or `1h`. They must be positive;
malformed, zero, and negative values fail controller startup. Unset or empty
values use the defaults below. Changes require restarting the controller.

| Variable | Default | Effect of increasing it |
| --- | --- | --- |
| `KAGENT_SESSION_QUIESCENCE_POLL_INTERVAL` | `1s` | Delays pausing or suspending settled sessions. Subsequent turns can remain blocked until the runtime boundary is completed. |
| `KAGENT_SESSION_EXPIRATION_POLL_INTERVAL` | `1m` | Delays deletion of sessions eligible under `KAGENT_SESSION_IDLE_TTL`. |
| `KAGENT_SANDBOX_EXPIRATION_POLL_INTERVAL` | `1s` | Delays deleting expired sandboxes, including cleanup of incomplete creation. |
| `KAGENT_SCHEDULED_RUN_POLL_INTERVAL` | `1s` | Delays reserving cron firings. Occurrences more than 30 seconds late are skipped. |
| `KAGENT_SCHEDULED_RUN_EXECUTION_POLL_INTERVAL` | `1s` | Delays dispatch, status updates, deadline enforcement, and execution cleanup, including manually triggered runs. |
| `KAGENT_RUNTIME_REVISION_GC_INTERVAL` | `1m` | Retains unreferenced runtime revisions and their external resources longer. |
| `KAGENT_MCP_TOOL_REFRESH_INTERVAL` | `5m` | Delays refreshing MCPServer and RemoteMCPServer tool catalogs. Each refresh writes to PostgreSQL. |
| `KAGENT_MCP_READINESS_POLL_INTERVAL` | `10s` | Delays checking unready MCPServers and updating their database catalogs. |

These settings preserve background processing. The existing
`KAGENT_SESSION_IDLE_TTL=0` option still disables session expiration specifically.
All other workers continue running independently of that setting.

Agent and sandbox-template preparation checks remain fixed at one second. They
scan cached state and only enqueue reconciliation for pending work; once
preparation and cleanup are settled, these ticks do not query PostgreSQL.
Slowing them would delay availability of newly prepared runtimes without helping
the database become idle in that settled state. MCP readiness checks differ:
each check of an unready MCPServer writes its database catalog, so that interval
is configurable.

## Connection pool

Set `database.postgres.pool.minConns: 0` and choose a short positive
`database.postgres.pool.maxConnIdleTime`, such as `30s`. These Helm values map to
`DB_MIN_CONNS` and `DB_MAX_CONN_IDLE_TIME`. The chart also supports `maxConns` and
`maxConnLifetime`, mapped to `DB_MAX_CONNS` and `DB_MAX_CONN_LIFETIME`.

Unset settings leave pgx pool configuration unchanged. pgx defaults to zero
minimum connections and a 30-minute idle timeout. Use the Helm pool settings
above to change these defaults.

pgx removes idle connections during pool cleanup, whose default interval is one
minute. Allow time for both the idle timeout and cleanup. With minimum
connection counts at zero, this maintenance does not issue periodic SQL pings
or reopen an empty pool. New requests acquire or reopen connections as needed.

## Helm example

This example allows long idle gaps for a deployment that can tolerate delayed
background work and does not rely on timely scheduled runs. Choose intervals
according to the customer's latency needs and the provider's autosuspend timeout;
`20m` is an illustrative value. Keep cron polling comfortably below the fixed
30-second lateness window when relying on schedules: a longer interval can skip
firings entirely. Execution polling must also leave enough time within run
deadlines to dispatch and complete work.

```yaml
database:
  postgres:
    bundled:
      enabled: false
    # Supply the connection URL through your deployment's secret configuration.
    pool:
      minConns: 0
      maxConnIdleTime: "30s"

controller:
  env:
    - name: KAGENT_SESSION_QUIESCENCE_POLL_INTERVAL
      value: "20m"
    - name: KAGENT_SESSION_EXPIRATION_POLL_INTERVAL
      value: "20m"
    - name: KAGENT_SANDBOX_EXPIRATION_POLL_INTERVAL
      value: "20m"
    - name: KAGENT_SCHEDULED_RUN_POLL_INTERVAL
      value: "20m"
    - name: KAGENT_SCHEDULED_RUN_EXECUTION_POLL_INTERVAL
      value: "20m"
    - name: KAGENT_RUNTIME_REVISION_GC_INTERVAL
      value: "20m"
    - name: KAGENT_MCP_TOOL_REFRESH_INTERVAL
      value: "20m"
    - name: KAGENT_MCP_READINESS_POLL_INTERVAL
      value: "20m"
```

## Scope of idle behavior

Workers still perform their initial sweeps at startup. Kubernetes events,
controller error retries, API requests, active interactions, and outstanding
work can all cause database traffic between sweeps. Request-scoped interaction
polling ends with the request; these settings control periodic background work.
Session quiescence drains available work before waiting again, and session
expiration drains full pages before waiting.

Workers on different controller replicas have independent timers. Their combined
activity, other database clients, and the provider's autosuspend policy determine
whether PostgreSQL actually suspends. Validate idle connection counts and query
activity in the deployed environment. kagent's health endpoints do not query
PostgreSQL.
