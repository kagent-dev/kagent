# Runtime revision cleanup

Runtime revision garbage collection runs in the controller holding the leader
lease. It discovers work at startup and every minute, independently of template
preparation. Each discovery and candidate attempt has a
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

## Metrics

| Metric | Type | Meaning |
| --- | --- | --- |
| `kagent_runtime_revision_gc_pending` | Gauge | Eligible persisted revisions in the latest successful discovery, including unattempted candidates and incomplete deletions. `NaN` while inactive or before the first successful discovery. |
| `kagent_runtime_revision_gc_failures_total{stage}` | Counter | Failed operations, separated by a fixed stage label. Counts attempts, not unique revisions. |

The backlog gauge has no application labels. Failure stages are limited to
`discovery` and `collection`. No metric labels contain
revision IDs, template names, namespaces, UIDs, or error text. Prometheus can
still attach its ordinary deployment, pod, and scrape-target labels.

GC publishes the count from the discovery that starts a sweep, then refreshes
once at the end, including for an empty sweep. It does not query the backlog
after each candidate. Newly eligible revisions found at the end wait for the
next sweep. Scrapes read in-memory collectors without database or network I/O.
Metrics reflect database state, not an optimistic decrement after a successful backend
call. For example, a lost response, failed finalization, or UID-protected
finalization no-op must not hide retained cleanup.

A failed discovery retains the last successful count and increments the
`discovery` failure counter. An initial failure stops the sweep; an end-of-sweep
failure leaves the starting count visible even if cleanup succeeded. Query,
scan, or decoding errors never publish partial results.

Only the leader runs discovery and cleanup. A standby, a stopped collector, or
a new process without successful discovery exposes `NaN` (unknown), not zero.
Do not convert unknown or missing samples to zero pending work. A successful
empty discovery publishes zero. After restart the new leader reconstructs count
from PostgreSQL; counters start again from zero. Use reset-aware `rate` or
`increase`, not raw counter differences, to monitor failures.

This version does not measure pending age or expose a last-discovery timestamp.
Counts can be stale between sweeps, during a slow candidate, or after failed
discovery. A healthy scrape is not proof that discovery succeeded recently.

## Diagnose backlog versus deletion failures

Scope queries to one controller deployment and its database. Scrape individual
replicas so a load-balanced Service does not alternate active and standby
samples. Use the active target's backlog gauge rather than summing replicas.
After leadership changes, allow for Prometheus scrape staleness. Check target
availability, leadership, discovery failures, and controller logs before
interpreting zero or stale backlog.

| Signal | Investigation |
| --- | --- |
| Pending count grows, but collection failures do not | Check workload churn, serial sweep throughput, candidate deadlines, and discovery failures. Count alone does not prove Substrate is broken. |
| `discovery` failures increase | Investigate database availability and malformed durable data. A retained count is not proof that cleanup is keeping up. |
| Pending count stays positive and `collection` failures recur | Correlate controller error logs by revision and ActorTemplate identity. Repeated deletion errors for the same revision distinguish persistently failing Substrate deletion from turnover among unrelated candidates. |
| Collection logs report claim or finalization errors | Investigate database errors; compute may already be gone while its durable cleanup row remains. |
| Collection logs report UID mismatches | Investigate the identity mismatch; never bypass the UID guard to force deletion. |

For example, compare the pending count with
`increase(kagent_runtime_revision_gc_failures_total{stage="collection"}[10m])`,
using your deployment's scrape labels to narrow the query. This counter cannot
identify a particular revision or distinguish a database failure from a
Substrate failure. The `failed to collect runtime revision` log
includes `revision`, `actor_template_atespace`, `actor_template_name`, and the
wrapped `error`.

A Substrate error such as `runsc state: exit status 128` can persist after
connectivity returns. A retained marker plus positive pending count and repeated
same-revision deletion logs makes that backlog visible while unrelated cleanup
continues. Restore the backend's ability to delete the object, then let the
normal sweep retry. Successful finalization removes the candidate; the next
successful discovery reflects the remaining backlog, or zero. Historical
failure counters do not decrease.

Parent shutdown/leadership cancellation does not increment failure counters.
A candidate's own deadline while its parent is still active does. Checkpoint
recovery and repair of stale active pair references belong to their respective
lifecycle owners, not to GC metrics.
