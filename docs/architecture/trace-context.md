# Caller Context in Traces

Agent spans describe *what the agent did*, but they say nothing about *who asked
for it*. Kagent can promote a configurable allowlist of caller-supplied values —
the signed-in user's email, a Slack thread, a support ticket ID — onto every
span of a request, so traces can be filtered and grouped by the caller in
Langfuse, Jaeger, Grafana Tempo, or any other OTLP backend.

The feature is **off by default**. It turns on when an operator sets an
allowlist.

---

## Configuration

| Setting | Default | Description |
|---|---|---|
| Helm `otel.tracing.contextKeys` | `[]` | List of context keys to promote |
| Env `KAGENT_TRACE_CONTEXT_KEYS` | `""` | Comma-separated form of the same list |

```yaml
otel:
  tracing:
    enabled: true
    contextKeys:
      - user.email
      - user.name
      - thread_id
      - channel
```

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
because it is the more specific source, it overrides baggage for the same key.

A key absent from both sources is simply not emitted.

---

## Where values land

Every promoted value becomes a span attribute named `kagent.context.<key>`:

```
baggage: user.email=ada@example.com   →   kagent.context.user.email = "ada@example.com"
metadata: {"thread_id": "1717171.42"} →   kagent.context.thread_id  = "1717171.42"
```

The attributes are merged into the **request-scoped attribute bag**, not set on
a single span. The `KagentAttributesSpanProcessor` (Python) and
`kagentAttributesSpanProcessor` (Go) stamp that bag onto every span started
during the request, so tool calls, sub-agent delegations, MCP calls, and model
calls all carry the same values.

This is deliberate rather than incidental: Langfuse v4 and comparable backends
resolve trace-level filters against the attributes present on each span, so
stamping only the root span would leave most views unfilterable.

---

## Safety properties

Caller-supplied context is untrusted input, so promotion is constrained on every
axis:

| Risk | Control |
|---|---|
| Attribute explosion / cardinality | Only allowlisted keys are read; the allowlist itself is capped at 32 entries |
| Oversized spans | Values are truncated to 256 characters, keys to 64 |
| Log or trace injection | Control characters are stripped from values |
| Shadowing semantic conventions | Every attribute is namespaced under `kagent.context.`, so `service.name` and friends cannot be overwritten even if an operator allowlists them |
| Leaking secrets into a trace backend | Nothing is promoted unless an operator names the key; raw values are never logged |
| A tenant widening the allowlist | The allowlist is cluster-wide operator configuration; an entry of the same name in a `Harness` environment is dropped rather than inherited |

Non-scalar metadata (objects, arrays) is skipped: it is unbounded in size and
meaningless as an attribute value.

Choose allowlist keys deliberately. Anything named here is visible to everyone
with access to the trace backend, and callers control the values.

---

## Renaming attributes for a backend

Some backends expect specific attribute names — Langfuse, for instance, maps
`user.id` and `session.id` onto its own trace fields. Rather than making the
attribute namespace configurable, do the rename in the OTel Collector that
already sits between kagent and the backend:

```yaml
processors:
  transform:
    trace_statements:
      - set(span.attributes["user.id"], span.attributes["kagent.context.user.email"])
        where span.attributes["kagent.context.user.email"] != nil
```

---

## Implementation

| Component | Path |
|---|---|
| Go ADK | `go/adk/pkg/telemetry/context_attributes.go` |
| Python | `python/packages/kagent-core/src/kagent/core/tracing/_context_attributes.py` |
| Controller forwarding | `go/core/v2/translator/kagent/compiler.go` |
| Helm | `helm/kagent/templates/controller-configmap.yaml` |

Both implementations share the same allowlist parsing, precedence, limits, and
sanitisation rules so the two runtimes cannot drift.
