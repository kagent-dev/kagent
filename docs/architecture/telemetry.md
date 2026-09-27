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
| Go ADK | `adk-go` | `otelgrpc` or `otelhttp` SERVER span and kagent's `a2a.request` | The ADK's own | The ADK's own | `gcp.vertex.agent.*`, written by the ADK itself |
| Claude Code, Codex | `claude`, `codex` | `otelgrpc` or `otelhttp` SERVER span | kagent's wrapper, `invoke_agent <agent>` | The native runtime's own | None |
| Python ADK | absent | FastAPI SERVER span | The ADK's own | The ADK's `generate_content`, plus OpenLLMetry client spans | `kagent.user_id`, `gen_ai.task.id` |
| LangGraph | absent | FastAPI SERVER span | None | OpenLLMetry | `kagent.user_id`, `gen_ai.task.id` |
| CrewAI | absent | FastAPI SERVER span | OpenLLMetry CrewAI | OpenLLMetry | `kagent.user_id`, `gen_ai.task.id` |
| OpenAI Agents | absent | FastAPI SERVER span | OpenLLMetry OpenAI Agents | OpenLLMetry OpenAI Agents | None |

## Enabling export

Configuration uses the OpenTelemetry SDK variables only. The chart renders them
into the controller from `otel.*`, the controller exports with them, and it
compiles the same settings into every runtime revision.

| Chart value | Variable |
| --- | --- |
| `otel.traces.enabled`, `otel.metrics.enabled`, `otel.logs.enabled` | `OTEL_TRACES_EXPORTER`, `OTEL_METRICS_EXPORTER`, `OTEL_LOGS_EXPORTER`, `otlp` or `none`. `OTEL_SDK_DISABLED=true` when all are off and the controller `/metrics` is off |
| `otel.exporter.otlp.endpoint`, `.protocol`, `.timeout` | `OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL` (`grpc` or `http/protobuf`), `OTEL_EXPORTER_OTLP_TIMEOUT` in milliseconds |
| `otel.<signal>.endpoint`, `.protocol` | `OTEL_EXPORTER_OTLP_<SIGNAL>_ENDPOINT` and `_PROTOCOL`, a full URL used as given |
| `otel.capture.messageContent` | `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT`, `SPAN_ONLY` or `NO_CONTENT` |
| `otel.capture.maxBytes` | `KAGENT_OTEL_MAX_CAPTURE_BYTES`, bytes kept per captured prompt and response. 16 KiB by default, 64 KiB at most |
| `otel.capture.rawApiBodies` | `KAGENT_OTEL_CAPTURE_RAW_API_BODIES`, native raw provider body logging in Claude |
| `otel.resourceAttributes` | `KAGENT_OTEL_RESOURCE_ATTRIBUTES`, added to the controller and every runtime |

The chart has no sampler setting. Runtimes stay parent-based AlwaysOn and
sampling belongs in the collector. An invalid value is reported as a warning and
turns its signal off, so an observability setting cannot invalidate an
AgentTemplate.

Each runtime receives `OTEL_SERVICE_NAME=<agent>` and an
`OTEL_RESOURCE_ATTRIBUTES` that carries `service.namespace`, `gen_ai.agent.name`,
`gen_ai.agent.id`, the provider and model for a harness, the operator's
attributes, and `service.version`, the short revision id added when the
ActorTemplate is built. A `Harness.spec.env` `OTEL_RESOURCE_ATTRIBUTES` is kept,
with the agent identity winning. The controller reports itself as
`kagent-controller` with `service.session.id` and `k8s.*` from the downward API.

kagent runtimes apply three defaults when the environment leaves them unset:
`OTEL_PROPAGATORS=tracecontext`, so a caller's baggage never reaches tools or
model providers, `OTEL_EXPORTER_OTLP_COMPRESSION=gzip`, and base-2 exponential
histograms. The Go runtimes set them in `go/pkg/telemetry`, which also hands
them to the Claude and Codex processes. The Python runtimes set them in
`kagent.core`. A BYO image gets them compiled in, together with the rest of the
kagent telemetry, only while kagent telemetry is on, and its own `Harness.spec.env`
values win, so an image that exports to its own backend keeps doing so. The Python ADK also defaults
`ADK_TELEMETRY_SCHEMA_VERSION_OPT_IN=2`, `OTEL_SEMCONV_STABILITY_OPT_IN=gen_ai_latest_experimental`,
and `ADK_CAPTURE_MESSAGE_CONTENT_IN_SPANS` from the capture setting.

Defaults live in the runtimes because a Substrate Actor holds at most 32
environment variables, and the ActorTemplate itself uses nine. The controller
renders only what differs by installation, and
`TestCompiledTelemetryFitsTheActorEnvironmentBudget` pins the worst case.

The capture variable is rendered whether or not the controller exports, so a
runtime that reaches a collector through settings the controller did not render
still follows it. It is controller-owned: the Claude and Codex compilers reject
a `Harness.spec.env` entry with that name, and the kagent compiler replaces one.
The harness runtimes carry the same decision in their compiled configuration.
Other `OTEL_*` variables, such as `OTEL_BSP_*`, remain available for per-Harness
tuning through `Harness.spec.env`. `OTEL_EXPORTER_OTLP_HEADERS` is not
forwarded, because an Actor environment holds no secrets. Export to an
in-cluster collector that adds them.

## Metrics and logs

kagent defines no metrics of its own. It exports otelgrpc's
`rpc.server.call.duration` and `rpc.client.call.duration` and otelhttp's
`http.*.request.duration`, in seconds, over OTLP. The controller also serves
them on `/metrics` when `controller.metrics.enabled` is on, so ingest one path
per backend. `rpc_server_call_duration_seconds` replaces the removed
`kagent_grpc_server_*` metrics.

Go logs are single-line JSON. Records inside a span carry `trace_id`, `span_id`
and `trace_flags`.

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
which only the request span carries, whichever runtime produced the trace.

`a2a.request` stays separate from the SERVER span because it can end and export
before a quiescent event leaves the process. The gateway may suspend the Actor
on that event while the SERVER span is still open.

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

The Go ADK stamps `gen_ai.conversation.id`, `a2a.task.id` and a trusted
`enduser.id` on the spans beneath the request span. The Python runtimes still
use `kagent.user_id` and `gen_ai.task.id`.

## Completion and ownership

The transport interceptor opens the span with the identity the runtime knows
before execution begins, so a request rejected during validation still reports
which agent rejected it. For native harnesses, the task-store wrapper takes
ownership and records task identity before the initial save can block, because
a2a-go runs an executor detached from the caller and a unary response can be
delivered while the turn is still working. The wrapper finishes the span if
execution never starts; otherwise the native executor completes it. The ADK
executor does not take ownership: its
request span is a transport span, completed when the response it describes is
delivered, and the ADK's own `invoke_agent` describes the turn. Completion runs
exactly once.

An Actor becomes eligible for automatic suspension after native cleanup and
task settlement, so completion exports spans and metrics before yielding the
quiescent event. The task-store wrapper flushes again before settlement to
export the final save, and after the settlement attempt to export that RPC.
The latter is best effort because suspension can race the RPC response.
Flush failures are logged and do not prevent settlement.
On a terminal event the gateway drains the runtime stream,
for up to two seconds, before closing its observation connection. Suspension
runs independently in the lifecycle workflow. The export
waits at most three seconds, so an unreachable collector costs at most that
once per segment: after a failed flush, later flushes in the same request are
skipped. It does nothing when traces are off.

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
published. Native cleanup and task settlement must still finish before the
lifecycle workflow can suspend the Actor.

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
  [Who emits what](#who-emits-what) that are outside the contract. The Python
  ones are removed when the Python runtimes move to the contract.
- The Go ADK writes tool arguments and results
  (`gcp.vertex.agent.tool_call_args`, `gcp.vertex.agent.tool_response`)
  whatever the capture setting, fixed upstream by
  [google/adk-go#1634](https://github.com/google/adk-go/issues/1634). Until
  then, drop them in a Collector if needed.
- A turn that pauses for input still ends its gateway CLIENT span as canceled.
- The Python runtimes declare no `kagent.runtime` and open no `invoke_agent`
  span, and the Python ADK counts model usage twice.
- Nothing yet compares emitted telemetry with the registry. The registry checks
  names, the Go and Python code uses the generated constants, and a live check
  against end-to-end telemetry is planned.
- Runtimes on Substrate set no `service.session.id`. An Actor can be restored
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
