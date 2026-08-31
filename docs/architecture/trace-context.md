# Caller Context in Traces

Agent spans describe *what the agent did*, but they say nothing about *who asked
for it*. Kagent can promote a configurable allowlist of caller-supplied values —
an opaque user identifier, a conversation thread, a ticket ID — onto every span
of a request, so traces can be filtered and grouped by the caller in Langfuse,
Jaeger, Grafana Tempo, or any other OTLP backend.

The feature is **off by default**. It turns on when an operator sets an
allowlist.

---

## Configuration

| Setting | Default | Description |
|---|---|---|
| Helm `otel.tracing.contextKeys` | `[]` | List of context keys or `{from, to, hash}` mappings to promote |
| Env `KAGENT_TRACE_CONTEXT_KEYS` | `""` | Comma-separated keys, or a JSON array of the same mappings |
| Helm `otel.tracing.contextHashKeySecret` | unset | Secret providing `KAGENT_TRACE_CONTEXT_HASH_KEY` for `hash: hmac-sha256` |

```yaml
otel:
  tracing:
    enabled: true
    contextKeys:
      - {from: sub, to: user.id}
      - thread_id
      - channel
```

Prefer an opaque identifier such as an OIDC `sub` for `user.id`. Do not put
names or email addresses on spans; see [Sensitive values](#sensitive-values).

The controller forwards `KAGENT_TRACE_CONTEXT_KEYS` to every agent it creates.
Both the Go and the Python runtime read it, so behaviour is identical whichever
one an agent runs.

Adding a new traced value is a configuration change, not a code change: append
the key and redeploy.

---

## Where values come from

Two sources feed the allowlist, in increasing order of precedence:

| Source | Set by | Survives hops |
|---|---|---|
| W3C [Baggage](https://www.w3.org/TR/baggage/) (`baggage` header) | Any client or proxy on the request path | Yes — automatically |
| A2A `message.metadata` | The A2A caller, per message | No — one hop only |

**Baggage is the primary mechanism.** It is the vendor-neutral OTel answer to
this problem and it needs no kagent-specific knowledge from the caller: the
controller, both runtimes, and every instrumented HTTP client already run a
composite `tracecontext + baggage` propagator, so a value set once at the edge
reaches the agent, its sub-agents, its tools, and its model calls without any
further plumbing.

**A2A `message.metadata` is the complement** for callers that cannot set a
header — for example, a bot that speaks A2A over an SDK that exposes message
metadata but not transport headers. It is scoped to a single message, and
because it is the more specific source, it overrides baggage for the same key
when the sanitised metadata value is non-empty. A blank metadata entry cannot
wipe a baggage value.

A key absent from both sources is simply not emitted.

---

## Where values land

Each mapping is read from `from` (defaulting to the entry itself) and written
as span attribute `to` (defaulting to `from`) after the prefix rules below:

```
baggage: sub=opaque-subject          →   user.id                 = "opaque-subject"
metadata: {"thread_id": "1717171.42"} →   kagent.context.thread_id = "1717171.42"
metadata: {"channel": "C0AB1"}        →   kagent.context.channel   = "C0AB1"
```

| Destination name | Emitted as |
|---|---|
| `user.id`, `user.hash`, `enduser.id`, `session.id` | Unprefixed (OpenTelemetry semantic conventions) |
| Anything else | `kagent.context.<name>` |

Invented names such as `user.asdasd` or `kagent.user_id` are prefixed. Promoted
values fill the request-scoped bag only when the key is not already set, so
caller context cannot override `kagent.user_id`, `kagent.app_name`, or
`gen_ai.*` attributes the runtime stamped.

The attributes are merged into the **request-scoped attribute bag**, not set on
a single span. The `KagentAttributesSpanProcessor` (Python) and
`kagentAttributesSpanProcessor` (Go) stamp that bag onto every span started
during the request, so tool calls, sub-agent delegations, MCP calls, and model
calls all carry the same values.

This is deliberate rather than incidental: Langfuse v4 and comparable backends
resolve trace-level filters against the attributes present on each span, so
stamping only the root span would leave most views unfilterable.

---

## Sensitive values

[OpenTelemetry recommends](https://opentelemetry.io/docs/security/handling-sensitive-data/)
against putting email addresses or names on telemetry at all. An OIDC `sub` is
already an opaque identifier and is what `user.id` should use.

If a stable identifier must be derived from a value that itself should not
appear on a span, hash it with HMAC-SHA256 onto the registry attribute
`user.hash`:

```yaml
contextKeys:
  - {from: sub, to: user.id}
  - {from: email, to: user.hash, hash: hmac-sha256}
  - thread_id
```

`hash: hmac-sha256` requires `KAGENT_TRACE_CONTEXT_HASH_KEY` (Helm:
`otel.tracing.contextHashKeySecret`). If the key is missing, the hashed
attribute is skipped — the original value is never written onto the span.

Hashing at promotion time only affects the span. Baggage travels on HTTP
headers, so a value placed in baggage is still visible to every downstream hop
that receives those headers, including model providers and HTTP MCP servers.
Do not put sensitive values in baggage; hash or replace them at the edge
before the request enters the cluster.

---

## Safety properties

Caller-supplied context is untrusted input, so promotion is constrained on every
axis:

| Risk | Control |
|---|---|
| Attribute explosion / cardinality | Only allowlisted keys are read; the allowlist itself is capped at 32 entries |
| Oversized spans | Values are truncated to 256 characters, keys to 64 |
| Log or trace injection | Control characters are stripped from values |
| Shadowing semantic conventions | Custom keys are namespaced under `kagent.context.`; only `user.id`, `user.hash`, `enduser.id`, and `session.id` pass through unprefixed. Caller context fills missing keys only. |
| Leaking secrets into a trace backend | Nothing is promoted unless an operator names the key; hashed entries are omitted when the HMAC key is unset |
| A tenant widening the allowlist | The allowlist is cluster-wide operator configuration; an entry of the same name in a `Harness` environment is dropped rather than inherited |

Non-scalar metadata (objects, arrays) is skipped: it is unbounded in size and
meaningless as an attribute value.

Choose allowlist keys deliberately. Anything named here is visible to everyone
with access to the trace backend, and callers control the values.

---

## Renaming attributes for a backend

Some backends expect names other than the ones kagent emits. Rather than making
the attribute namespace configurable, do the rename in the OTel Collector that
already sits between kagent and the backend:

```yaml
processors:
  transform:
    trace_statements:
      - set(span.attributes["session.id"], span.attributes["kagent.context.thread_id"])
        where span.attributes["kagent.context.thread_id"] != nil
```

---

## Implementation

| Component | Path |
|---|---|
| Go ADK | `go/adk/pkg/telemetry/context_attributes.go` |
| Python core | `python/packages/kagent-core/src/kagent/core/tracing/_context_attributes.py` |
| Python ADK | `python/packages/kagent-adk/src/kagent/adk/_agent_executor.py` |
| Python LangGraph | `python/packages/kagent-langgraph/src/kagent/langgraph/_executor.py` |
| Python CrewAI | `python/packages/kagent-crewai/src/kagent/crewai/_executor.py` |
| Python OpenAI | `python/packages/kagent-openai/src/kagent/openai/_agent_executor.py` |
| Controller forwarding | `go/core/v2/translator/kagent/compiler.go` |
| Helm | `helm/kagent/templates/controller-configmap.yaml` |

Both implementations share the same allowlist parsing, precedence, limits, and
sanitisation rules so the two runtimes cannot drift.
