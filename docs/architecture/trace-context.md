# Caller Context in Traces

Agent spans describe *what the agent did*, but they say nothing about *who asked
for it*. Kagent can copy a configurable allowlist of caller-supplied values —
an opaque user identifier, a conversation thread, a ticket ID — onto every span
of a request, so traces can be filtered and grouped by the caller in Langfuse,
Jaeger, Grafana Tempo, or any other OTLP backend.

The feature is **off by default**. It turns on when an operator sets an
allowlist.

---

## Configuration

| Setting | Default | Description |
|---|---|---|
| Helm `otel.tracing.contextKeys` | `[]` | List of baggage keys, or `{from, to}` mappings, to copy onto spans |
| Env `KAGENT_TRACE_CONTEXT_KEYS` | `""` | Comma-separated keys, or a JSON array of the same mappings |

```yaml
otel:
  tracing:
    enabled: true
    contextKeys:
      - {from: sub, to: user.id}
      - thread_id
      - gen_ai.conversation.id
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

There is one path onto spans: **W3C [Baggage](https://www.w3.org/TR/baggage/)**.
Both runtimes install the upstream baggage-to-span-attribute processor
(`baggagecopy` in Go, `BaggageSpanProcessor` in Python) with a predicate that
copies only allowlisted destination keys.

A2A `message.metadata` is promoted *into baggage* at the executor boundary when
the destination key is not already set. That is what lets callers that cannot
set a transport header still reach every span — including sub-agent hops —
without a second merge path or a request-scoped attribute bag.

| Source | Set by | How it reaches spans |
|---|---|---|
| W3C baggage (`baggage` header) | Any client or proxy on the request path | Copied by the baggage span processor |
| A2A `message.metadata` | The A2A caller, per message | Written into baggage at the executor if the destination key is absent |

Empty or non-scalar metadata does not wipe baggage. `{from, to}` remaps a
source key onto the destination baggage member; the processor then copies
`to`. Destination names are used as-is, so `user.id` and
`gen_ai.conversation.id` are reachable without a hardcoded unprefixed set.

A key absent from both sources is simply not emitted.

---

## Where values land

```
baggage: user.id=opaque-subject      →   user.id                  = "opaque-subject"
metadata: {"sub": "opaque-subject"}  →   user.id                  = "opaque-subject"   # with {from: sub, to: user.id}
metadata: {"thread_id": "1717171.42"} →  thread_id                = "1717171.42"
```

The request-scoped kagent attribute bag still carries runtime values
(`kagent.user_id`, `gen_ai.conversation.id`, …). That processor runs after
baggagecopy, so caller baggage cannot override attributes the runtime already
stamped.

This is deliberate: Langfuse v4 and comparable backends resolve trace-level
filters against the attributes present on each span, so stamping only the
root span would leave most views unfilterable.

---

## Sensitive values

[OpenTelemetry recommends](https://opentelemetry.io/docs/security/handling-sensitive-data/)
against putting email addresses or names on telemetry at all. An OIDC `sub` is
already an opaque identifier and is what `user.id` should use.

Hashing and renaming that must happen before a value is visible on hops belong
in the authenticating gateway or the OTel Collector. Baggage travels on HTTP
headers, so a value placed there is visible to every downstream hop that
receives those headers, including model providers and HTTP MCP servers.

Value length is the ordinary OTel limit
(`OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT`), not a kagent-specific cap.

---

## Safety properties

Caller-supplied context is untrusted input, so promotion is constrained:

| Risk | Control |
|---|---|
| Attribute explosion / cardinality | Only allowlisted keys are copied; the allowlist is capped at 32 entries |
| Oversized spans | `OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT` |
| Shadowing runtime attributes | The kagent span processor runs after baggagecopy and wins on conflict |
| Leaking secrets into a trace backend | Nothing is copied unless an operator names the key |
| A tenant widening the allowlist | The allowlist is cluster-wide operator configuration; an entry of the same name in a `Harness` environment is dropped rather than inherited |

Non-scalar metadata (objects, arrays) is skipped: it is unbounded in size and
meaningless as a baggage value.

Choose allowlist keys deliberately. Anything named here is visible to everyone
with access to the trace backend, and callers control the values.

---

## Renaming attributes for a backend

`{from, to}` is enough when the operator wants a baggage key remapped at the
executor. Further renames belong in the OTel Collector that already sits
between kagent and the backend:

```yaml
processors:
  transform:
    trace_statements:
      - set(span.attributes["session.id"], span.attributes["thread_id"])
        where span.attributes["thread_id"] != nil
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
| Controller forwarding | `go/core/internal/translator/kagent/compiler.go` |
| Helm | `helm/kagent/templates/controller-configmap.yaml` |
