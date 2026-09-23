# Telemetry

Kagent exports OpenTelemetry traces from agent runtimes when a user enables
them. This document describes what a runtime produces, so consumers can rely on
it without reading runtime internals.

## Contract

The contract is a Weaver registry in [`telemetry/registry`](../../telemetry/registry).
It defines every `kagent.*` and `a2a.*` attribute, and it references each
upstream attribute kagent writes, with kagent's requirement level and notes.
Everything else is generated from it:

- [Telemetry contract reference](telemetry-contract.md), the attribute, span and
  resource tables
- `go/pkg/telemetry/conv`, the Go constants
- `kagent.core.telemetry.conv`, the Python constants
- `telemetry/resolved.yaml`, the resolved registry, so a contract change shows up
  in review

| Concept | Attributes | Where |
| --- | --- | --- |
| Operation | `gen_ai.operation.name` | A span is a GenAI operation when it carries this. kagent writes `invoke_agent` only where the runtime emits none of its own |
| Runtime | `kagent.runtime` | Resource |
| Agent | `gen_ai.agent.name`, `gen_ai.agent.id` | Resource and invoke_agent span |
| Provider and model | `gen_ai.provider.name`, `gen_ai.request.model` | Resource and invoke_agent span, for harnesses compiled against one model |
| Conversation, task, user | `gen_ai.conversation.id`, `a2a.task.id`, `enduser.id` | Request spans. Never on a resource or a metric |
| Segment | `kagent.invocation.segment`, `kagent.invocation.disposition`, link `kagent.invocation.relationship` | Request spans |
| Outcome | status, `error.type`, `a2a.task.state` | Request spans |
| Content | `gen_ai.input.messages`, `gen_ai.output.messages`, `kagent.capture.*_truncated` | invoke_agent and inference spans, under the capture opt-in |
| Usage | `gen_ai.usage.*` | Inference spans, from the runtime's own instrumentation |

The registry describes the contract the runtimes are moving to. The next section
says what each runtime emits today, and [Blind spots](#blind-spots) lists where
the two still differ.

## Who emits what

| Runtime | `kagent.runtime` | Request span | `invoke_agent` | Inference spans | Keys outside the contract |
| --- | --- | --- | --- | --- | --- |
| Go ADK | `adk-go` | `otelhttp` SERVER span and kagent's `a2a.request` | The ADK's own | The ADK's own, plus kagent's `invocation` span in the `gcp.vertex.agent` scope | `kagent.user_id`, `gen_ai.task.id`, `kagent.app_name`, `a2a.message.metadata.*`, `gcp.vertex.agent.*` |
| Claude Code, Codex | `claude`, `codex` | `otelhttp` SERVER span | kagent's wrapper, `invoke_agent <agent>` | The native runtime's own | None |
| Python ADK | absent | FastAPI SERVER span | The ADK's own | The ADK's `generate_content`, plus OpenLLMetry client spans | `kagent.user_id`, `gen_ai.task.id` |
| LangGraph | absent | FastAPI SERVER span | None | OpenLLMetry | `kagent.user_id`, `gen_ai.task.id` |
| CrewAI | absent | FastAPI SERVER span | OpenLLMetry CrewAI | OpenLLMetry | `kagent.user_id`, `gen_ai.task.id` |
| OpenAI Agents | absent | FastAPI SERVER span | OpenLLMetry OpenAI Agents | OpenLLMetry OpenAI Agents | None |

## Enabling export

The controller resolves telemetry from its own process environment and compiles
the result into each runtime revision.

| Variable | Effect |
| --- | --- |
| `OTEL_TRACING_ENABLED` | Enables trace export for compiled runtimes |
| `OTEL_LOGGING_ENABLED` | Enables log export for compiled runtimes |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | Destination, with the usual signal-specific overrides |
| `OTEL_EXPORTER_OTLP_PROTOCOL` | `grpc` or `http/protobuf`, with signal-specific overrides |
| `KAGENT_OTEL_CAPTURE_SENSITIVE_CONTENT` | Enables bounded prompt and response capture. Off by default |
| `KAGENT_OTEL_CAPTURE_RAW_API_BODIES` | Enables native raw provider body logging. Off by default |
| `KAGENT_OTEL_MAX_CAPTURE_BYTES` | Bytes retained per captured prompt and per captured response. Defaults to 16 KiB, ceiling 64 KiB |

An unusable capture budget is reported as a compilation warning and replaced by
the default, so an observability setting cannot invalidate an AgentTemplate.

The capture decision reaches every runtime as the standard GenAI instrumentation
variable, `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT`, rendered
`SPAN_ONLY` when capture is on and `false` when it is off. It is rendered
whether or not the controller exports traces, so a runtime that reaches a
collector through settings the controller did not render still follows it. The
ADK runtimes read the variable as a mode and treat a plain `true` as log records
only, so the span form is what puts `gen_ai.input.messages` and
`gen_ai.output.messages` on their model spans; the ADK Go runtime's older
`gcp.vertex.agent.llm_request` and `llm_response` payload attributes follow the
same value. The harness runtimes carry the same decision in their compiled
configuration. The variable is controller-owned: the Claude and Codex compilers
reject a `Harness.spec.env` entry with that name, and the kagent compiler
replaces one with the controller's value. One setting decides whether prompts
enter traces, and no runtime can be talked into recording them by a
user-supplied variable.

Other `OTEL_*` variables remain available for per-Harness tuning through
`Harness.spec.env`, including `OTEL_RESOURCE_ATTRIBUTES`.

## Conventions and versioning

The registry depends on the core semantic conventions at v1.44.0 and on the
GenAI conventions, which live in their own repository and have no release yet,
pinned by commit in `telemetry/registry/manifest.yaml`. The `gen_ai.*` names in
the generated constants come from that pin. Core names such as `service.*`,
`enduser.id` and `error.type` come from the newest core Go package,
`semconv/v1.43.0`, which is also the version the OpenTelemetry SDK uses, and
every tracer kagent creates declares its schema URL. Only the `kagent.*` and
`a2a.*` names are kagent's own. All of them are `development` until the v1.x
contract is declared stable.

## The invocation span

Each A2A `SendMessage` or `SendStreamingMessage` request opens one span in the
instrumentation scope `github.com/kagent-dev/kagent/go/adk/pkg/a2a/server`. It
represents one execution segment, and what it is called depends on whether
anything beneath it describes the agent invocation.

For a native harness, Claude Code or Codex, nothing does: the native runtime
emits model and tool spans but no agent invocation. The request span is
therefore the GenAI conventions' `invoke_agent` operation, named
`invoke_agent <gen_ai.agent.name>`, and it is the anchor consumers should read.

For the ADK runtime the ADK emits `invoke_agent` spans of its own, carrying
`gen_ai.agent.name` and `gen_ai.conversation.id`: one for the root agent and
one for each sub-agent it transfers to within the turn. The root one is the
invocation. The request span stays a transport span named `a2a.request` with
the same identity attributes and no `gen_ai.operation.name`, so the wrapper
never adds an invocation of its own to what the ADK reports. A consumer that
counts turns rather than agent invocations counts `kagent.invocation.segment`,
which only the request span carries, whichever runtime produced the trace. The
ADK also keeps an `invocation` span of its own beneath the request span.

The attributes, their values and their requirement levels are in the
[contract reference](telemetry-contract.md). Today the request span also carries
`kagent.runtime`, which the contract keeps on the resource only.

The provider and model on this span stand in for what a native runtime does not
report. Codex records token usage on a span that names no model, so without the
compiled model a consumer cannot attribute that usage without walking the trace.
The conventions allow the model on an agent span when the agent is bound to one
model, which a compiled kagent agent is.

The runtime resource carries `service.name` and `service.namespace` from the
compiled agent identity, plus the same `kagent.runtime`, `gen_ai.agent.name`,
`gen_ai.agent.id`, `gen_ai.provider.name` and `gen_ai.request.model`. The
harness adapters merge those, with `service.namespace`, into
`OTEL_RESOURCE_ATTRIBUTES` for the native child process, so its spans report the
same agent, model and namespace while keeping its own `service.name`. Conversation, task, and user identity never appear on a
resource, since one runtime process serves many of each.

The ADK runtime additionally stamps `kagent.user_id`, `gen_ai.task.id`,
`gen_ai.conversation.id` and `kagent.app_name` on the spans beneath the
invocation, as it always has; the Python runtimes stamp the first three. Those
descendant keys move to the names above together with a Python invocation span,
so the two runtimes change in one step rather than diverging further.

## Completion and ownership

The transport interceptor opens the span with the identity the runtime knows
before execution begins, so a request rejected during validation still reports
which agent rejected it. The harness executor then takes ownership, because
a2a-go runs an executor detached from the caller and a unary response can be
delivered while the turn is still working. The ADK executor does not: its
request span is a transport span, completed when the response it describes is
delivered, and the ADK's own `invoke_agent` describes the turn. Completion runs
exactly once.

For runtimes whose Actor may be suspended as soon as a quiescent event leaves
the process, completion exports before that event is yielded. The export is
bounded by `KAGENT_TRACE_FLUSH_TIMEOUT_MS`, three seconds by default, so an
unreachable collector costs at most that budget once per segment.

A segment records `abandoned` when the A2A event consumer stopped accepting
events before execution finished, and `interrupted` when the execution context
ended without a cancellation request. Neither is reported as cancellation, which
is recorded only when a client asked for it. A harness segment observes the A2A
event pipe rather than the network client, so a client that closes a streaming
subscription leaves that execution running and is not visible to it. A request
span the transport still owns is completed as `abandoned` when the request
context ends without a quiescent event, which is how a2a-go surfaces a caller
that stopped waiting; otherwise the span would never end and never export.

A failure that never publishes a task event, such as a rejected request, is
recorded with its `error.type` and exported before the error leaves the
process. A panic in a harness runner is recorded as `error.type=runtime_panic`
without the panic value and then propagates.

Cancellation completes and exports the segment before the canceled event is
published, since that event is what releases the gateway to suspend the Actor.

## Approvals and resumed turns

A turn that pauses for approval keeps the same native process. The trace context
the native runtime received belongs to the request that started it, and neither
native protocol offers a supported way to replace it when the turn resumes. Work
the native runtime does after an approval therefore stays under the originating
trace.

Kagent does not paper over this. Each execution segment gets its own request
span carrying the same conversation and task identity, and a resumed segment
records an OpenTelemetry link back to the segment that started the native turn,
however many times the turn has paused since, with
`kagent.invocation.relationship` set to `resume_origin`. A link states
a relationship. It does not reparent spans and it does not transfer ownership of
the token usage recorded under the originating segment. Consumers should expect
several segments for one task and should not assume the last one owns the work.

## Content capture

Capture is off unless a user turns it on. When it is on, a segment records
the current turn's prompt as `gen_ai.input.messages` and the text that segment
produced as `gen_ai.output.messages`, each a JSON array holding one message with
one text part, in the shape the conventions define for those attributes. Each
output message carries the `finish_reason` the conventions require: `stop` for
a completed segment, `tool_call` for a segment parked for an approval or a
question, since both harnesses park only at a tool call, `error` for a failure,
and the disposition name for a canceled, abandoned or interrupted segment. The
text is bounded by the configured byte budget, preserving UTF-8 and reporting
truncation; the structure around it is not counted. Tool arguments, tool
results, approval structures, the rest of the conversation, and native stderr
are never recorded in these attributes.

Absent output messages mean capture is disabled; a message with empty text
means the segment produced none. A resumed segment records no input messages,
because its input is a structured approval or answer rather than prompt text.

These attributes come from the Go wrapper, which is only one of the producers.
Suppressing them does not establish privacy for native runtime events or log
bodies, which have their own settings.

## Rollout

The controller hands each Claude Code and Codex Actor its compiled
configuration as JSON, and that JSON carries a version number. The harness
binary in the runtime image accepts only the version it was built with, and
rejects anything else at startup with `unsupported config version N (want M)`.
The telemetry section described above is one such change, since adding it moved
the Claude version from 4 to 5 and the Codex version from 2 to 3.

That makes upgrade order matter. A Harness selects its runtime image by digest
in `spec.workload.image`. If the controller is upgraded to a build that includes
a new version while a Harness still points at a harness image built before it,
every new revision the controller compiles for that Harness carries the new
version, the older binary refuses it, and the Actor exits before it can serve a
request. The controller cannot catch this when it compiles, because it has no
way to ask the pinned image which version it understands.

Publish the controller image and the harness images from the same commit, and
when one digest moves, move the others in the same change.

## Changing the contract

Edit the registry, then regenerate and commit the result:

```bash
make semconv-check      # Weaver validation, shared and kagent policies
make semconv-generate   # constants, reference, resolved snapshot
make semconv-verify     # both, plus the policy tests and a drift check (CI)
```

The kagent policies in `telemetry/policies` enforce that kagent defines
attributes only in the `kagent.` and `a2a.` namespaces, that request identity
never reaches a resource or a metric, that every kagent span references
`error.type`, and that no metric name carries a unit or `_total` suffix. Each
policy has a fixture under `telemetry/policies/testdata` that it must reject.
Weaver runs from the version pinned in `telemetry/versions.env`, through a local
binary of exactly that version or the pinned image.

A rename or removal shows up as a diff in `telemetry/resolved.yaml` and the
generated files. Once an attribute is declared stable, the check also runs the
upstream backwards-compatibility policy against the last release.

## Lint exceptions

- `a2a.*` is not a registered OpenTelemetry namespace. kagent owns it until the
  conventions define A2A attributes.
- `gen_ai.provider.name` takes `ollama` and `sap.ai_core`, which the conventions
  do not list. The attribute is an open enum.
- `error.type` uses a bounded kagent vocabulary, listed in the reference.

## Blind spots

- The runtimes still emit the keys listed under
  [Who emits what](#who-emits-what) that are outside the contract. They are
  removed when the Go and Python runtimes move to the contract.
- The Python runtimes declare no `kagent.runtime` and open no `invoke_agent`
  span, and the Python ADK counts model usage twice.
- Nothing yet compares emitted telemetry with the registry. The registry checks
  names, the Go and Python code uses the generated constants, and a live check
  against end-to-end telemetry is planned.
- Runtimes on Substrate set no `service.instance.id`. An Actor can be restored
  from a snapshot, so an identity generated in the process would be wrong or
  shared.
- Native span export completeness at process shutdown is a separate concern from
  the Go wrapper's export, and is tracked against the native runtimes.
- Ownership of native work started after an approval is ambiguous by
  construction and is expressed as a link rather than asserted as a parent.
- a2a-go dispatches execution before it reads the caller's subscription. A
  caller that disconnects inside that window completes the invocation from the
  transport side, so that request's span is recorded as abandoned without
  conversation or task identity while execution continues untraced.
- A request rejected by a transport interceptor before execution begins has no
  invocation span. Such a request never reaches an agent.
