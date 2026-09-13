# Runtime revision cleanup

Runtime revision garbage collection runs in the controller holding the leader
lease. It discovers work at startup and every minute, independently of template
preparation. Each discovery, backlog observation, and candidate attempt has a
one-minute deadline. Sweeps are serial: a slow candidate delays later work, but a
failed candidate does not prevent other candidates from being attempted.

An eligible revision has no active desired or last-successful pair reference,
AgentInstance reference, or checkpoint reference. All checkpoint states retain
their references. After claiming a revision for deletion, GC preserves the row
and ActorTemplate identity until Substrate cleanup and database finalization
succeed. Do not manually remove these references or deletion markers to silence
an alert.

## Enable authenticated metrics

The controller metrics listener is disabled by default. The existing Helm
settings enable the dedicated metrics Service:

```yaml
controller:
  metrics:
    enabled: true
    bindAddress: ":8443"
    secureServing: true
    service:
      port: 8443
```

The chart sets `METRICS_BIND_ADDRESS` and `METRICS_SECURE`. Outside Helm, an
unset/empty bind address or `"0"` disables the listener. Secure serving defaults
to true; an invalid `METRICS_SECURE` value fails startup. An explicit false
enables unauthenticated HTTP and should only be used on an appropriately
restricted development listener.

Secure scrapes use HTTPS and a Kubernetes bearer token. The controller's
existing metrics-auth ClusterRole permits TokenReviews and SubjectAccessReviews.
Bind `<fullname>-metrics-reader` to the scraper's ServiceAccount to allow
`GET /metrics`, and configure the scraper to trust the listener certificate.
Do not disable certificate verification or authentication as an operational
workaround. No ServiceMonitor is created by this feature.

GC metrics use the registry served by the controller manager, not the public
gRPC/MCP application port. A successful HTTP response alone is not evidence
that the GC metrics are present.

## Metrics and timestamps

| Metric | Type | Meaning |
| --- | --- | --- |
| `kagent_runtime_revision_gc_pending` | Gauge | Eligible persisted revisions in the latest successful backlog observation, including unattempted candidates and incomplete deletions. |
| `kagent_runtime_revision_gc_oldest_pending_age_seconds` | Gauge | Time since the oldest candidate in that observation was first durably discovered as eligible. Zero for an observed empty backlog. |
| `kagent_runtime_revision_gc_failures_total{stage}` | Counter | Failed operations, separated by a fixed stage label. Counts attempts, not unique revisions. |
| `kagent_runtime_revision_gc_last_successful_backlog_observation_timestamp_seconds` | Gauge | Unix timestamp of the active collector's latest successful observation; zero until its first successful read. |

Backlog gauges have no application labels. Failure stages are limited to
`discovery`, `backlog_observation`, `begin_deletion`, `get_actor_template`,
`uid_check`, `delete_actor_template`, and `finalize`. No metric labels contain
revision IDs, template names, namespaces, UIDs, or error text. Prometheus can
still attach its ordinary deployment, pod, and scrape-target labels.

`cleanup_pending_since` records the first durable discovery of a continuous
eligible period. This is a lower bound on cleanup waiting time, not revision
creation time and not the exact time its last reference was released. Reference
reacquisition clears it atomically, so a later eligible period starts afresh.
Retries and controller restarts preserve it. Existing deletion claims retain
their original `deleted_at` age during migration; historical unclaimed rows
start at their first observation. The timestamp is observational metadata:
only the existing deletion claim fences new references.

GC refreshes the backlog before cleanup and after candidate outcomes. Metrics
reflect database state, not an optimistic decrement after a successful backend
call. For example, a lost response, failed finalization, or UID-protected
finalization no-op must not hide retained cleanup.

An observation checks the complete eligible set, not only its initial discovery
list. If concurrent reference changes make that set incomplete, it releases
locks and retries in a new transaction, up to three attempts within the original
deadline. Continued churn fails the observation rather than publishing a
partial or falsely empty backlog.

Age advances between sweeps using the saved timestamp. Clock skew between the
database and controller can affect age; negative ages are clamped to zero.
A failed observation retains the previous snapshot and freshness timestamp,
rather than reporting a healthy empty backlog.

Only an active collector emits backlog/freshness gauges. A standby emits no
backlog gauge, and an active collector with no successful observation emits
only freshness zero. Missing samples must not be treated as zero pending work.
After restart the new leader reconstructs backlog from PostgreSQL; counters
start again from zero. Use reset-aware `rate` or `increase`, not raw counter
differences, to monitor failures.

## Diagnose backlog versus deletion failures

Scope queries to one controller deployment and its database. Scrape individual
replicas so a load-balanced Service does not alternate active and standby
samples. Use the active target's backlog gauges rather than summing replicas.
After leadership changes, allow for Prometheus scrape staleness. Check target
availability and the observation timestamp before interpreting zero or stale
backlog.

| Signal | Investigation |
| --- | --- |
| Pending count grows, but deletion failures do not | Check workload churn, serial sweep throughput, candidate deadlines, and discovery/observation freshness. Count alone does not prove Substrate is broken. |
| `discovery` or `backlog_observation` failures increase | Investigate database availability, lock waits, sustained reference churn, and malformed durable data. A stale observation is not proof that cleanup is keeping up. |
| Pending age grows and `delete_actor_template` failures recur | Correlate controller error logs by revision and ActorTemplate identity. Repeated failures for the same revision distinguish persistently failing Substrate deletion from turnover among unrelated candidates. |
| `get_actor_template` failures recur | Check Substrate connectivity and read errors before concluding the delete operation itself is failing. |
| `begin_deletion` or `finalize` failures recur | Investigate database errors; compute may already be gone while its durable cleanup row remains. |
| `uid_check` failures recur | Investigate the identity mismatch; never bypass the UID guard to force deletion. |

For example, compare the pending count and age with
`increase(kagent_runtime_revision_gc_failures_total{stage="delete_actor_template"}[10m])`,
using your deployment's scrape labels to narrow the query. This counter cannot
identify a particular revision. The `failed to collect runtime revision` log
includes `revision`, `actor_template_atespace`, `actor_template_name`, and the
wrapped `error`.

A Substrate error such as `runsc state: exit status 128` can persist after
connectivity returns. A retained marker plus increasing age and repeated
same-revision deletion logs makes that backlog visible while unrelated cleanup
continues. Restore the backend's ability to delete the object, then let the
normal sweep retry. Successful finalization removes the candidate; count and
oldest age reflect the remaining backlog, or both become zero. Historical
failure counters do not decrease.

Parent shutdown/leadership cancellation does not increment failure counters.
A candidate's own deadline while its parent is still active does. Checkpoint
recovery and repair of stale active pair references belong to their respective
lifecycle owners, not to GC metrics.
