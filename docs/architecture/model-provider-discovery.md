# Model provider discovery

ModelProviderConfig defines a discovery source. Users select discovered models
when creating a ModelConfig, which agents use for invocation. OSS discovery never
automatically creates, updates or deletes ModelConfigs.

## Configuration and user flow

```yaml
apiVersion: api.kagent.dev/v1alpha3
kind: ModelProviderConfig
metadata:
  namespace: kagent
  name: team-gateway
spec:
  type: OpenAI
  endpoint: https://agw.example.com/team/v1
  # Optional; same namespace, exactly one nonempty data key:
  # secretRef:
  #   name: discovery-credentials
```

The adapter requests `/team/v1/models`, preserving the endpoint prefix.
`OpenAI` selects the discovery protocol and supports Agw's non-OpenAI and virtual
models too. Other discovery adapters are not implemented.

In **Models → Create model**, discovery is optional and defaults to manual
configuration. Without a source selected, the existing provider selector,
built-in suggestions and manual model ID entry remain available.

Selecting a ModelProviderConfig shows only that source's models. Users choose
Chat Completions, Responses or Anthropic Messages, supply separate invocation
credentials, and save a normal ModelConfig. Invocation bases are prefilled and
editable; Messages maps a discovery base ending `/team/v1` to `/team`.
Agw invocation-format metadata is still a TODO, so format selection is explicit.

## Catalog and refresh

KRT observes sources and Secrets; bounded workers fetch models outside KRT
transformations. Each API-serving replica maintains its own in-memory catalog.
API responses report local state. Discovery publishes status metadata into a KRT
collection; a KRT status collection derives desired status. Only the leader runs
the retry queue that writes it through the generated Kubernetes client.
Temporary differences between replicas are accepted. Status uses one `Ready`
condition: successful discovery is True; pending or failed discovery is False,
with the cause in `reason` (for example, `SecretUnresolved`). `modelCount` and
`lastDiscoveryTime` describe the catalog; readiness does not verify invocation.

Entries use a typed OpenAI-compatible shape with optional Agw names, attribution,
token limits, modalities, capabilities and supported parameters. IDs are preserved
exactly; absent metadata stays unknown. Internal snapshots include source identity,
revision, endpoint, state, safe errors and last-success time.

| Event | Catalog behavior |
| --- | --- |
| Startup | Pending until discovery completes. |
| Successful refresh | Atomically replace all entries, including an empty result. |
| Failure with unchanged inputs | Retain previous entries as stale; without prior success, report failed. |
| Endpoint, type or Secret observation changes | Invalidate old entries before rediscovery; discard obsolete responses. |
| Source deletion | Remove the snapshot; recreation uses a new UID. |

Refresh runs every minute with ±20% jitter (48–72 seconds), independently of
configuration changes. Defaults: four concurrent requests per replica, one per source,
15-second attempts, 30-second explicit-refresh waits, and retries backing off
from five seconds to five minutes. Explicit requests coalesce and admit new
attempts at most every ten seconds per source; caller cancellation only ends
that caller's wait.

Bodies and model-list responses are limited to 2 MiB, catalogs to 10,000 entries,
and entries to 16 KiB. Invalid, duplicate or truncated catalogs fail as a whole.
Agw requests omit `limit`, which truncates without pagination.

### Capacity benchmark

Synthetic run (2026-10-27): 100 sources × 1,000 independently decoded enriched
models, Go 1.27.3, Linux amd64, Ryzen 7 PRO 6850U, `GOMAXPROCS=4`, `GOGC=100`.
Retained catalog heap was **54.2 MiB**. Medians across three runs:

| Operation | Time | Allocated per operation |
| --- | --- | --- |
| Copy one source | 0.74 ms | 0.53 MiB |
| Copy all 100 sources | 91 ms | 52.7 MiB |
| Parse and replace all catalogs, four workers | 316 ms | 154.8 MiB |
| Parse a near-2-MiB response | 16.7 ms | 6.57 MiB |

Peak benchmark-process RSS was 311 MiB, including runtime/test overhead;
allocations are cumulative, not retained memory. These exclude networking,
informers and other controller services, so they do not establish a pod memory
limit. Full-catalog copies hold the catalog mutex; prefer per-source reads.
Reproduce with `BenchmarkCatalogScale` and `BenchmarkCatalogNearBodyLimit` in
`go/core/internal/controller/modeldiscovery/benchmark_test.go`.

## Access and integration

ModelService lists authorized sources. `ListProviderModelsResponse` returns only
`discovered_models`, `endpoint`, `state` and `error`, without a wrapper.
Legacy protobuf fields remain deprecated and are not populated. Reads use the source
namespace/name; UID, generation and freshness timestamps remain internal. The UI
clears selections when the endpoint changes, discovery becomes pending/failed, or
the selected model disappears; same-endpoint recreation alone keeps the selection. Explicit
refresh requires Update permission and is unavailable to session shares.

Discovery credentials are never exposed or reused for invocation. A supplied
Secret must resolve; invalid credentials never fall back to unauthenticated
requests. Endpoints must be absolute HTTP(S) bases without userinfo, queries or
fragments. TLS verification remains enabled; redirects are rejected.

`go/core/pkg/modeldiscovery.Reader` and `app.Options.ModelDiscovery` expose
caller-owned snapshots to in-process consumers. Subscribe before listing and
reread on coalesced notifications; freshness-only successes emit no model-change
notification. Consumers must wait for synchronization and current discovery,
and must not treat pending, failed or stale catalogs as deletion inventories.
Automatic ModelConfig synchronization is outside this implementation.
