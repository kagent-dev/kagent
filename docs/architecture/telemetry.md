# Telemetry

Kagent exports OpenTelemetry traces from agent runtimes when a user enables
them. This document describes what a runtime produces, so consumers can rely on
it without reading runtime internals.

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

Other `OTEL_*` variables remain available for per-Harness tuning through
`Harness.spec.env`, including `OTEL_RESOURCE_ATTRIBUTES`.

## The invocation span

Each A2A `SendMessage` or `SendStreamingMessage` request opens one span named
`a2a.request` in the instrumentation scope
`github.com/kagent-dev/kagent/go/adk/pkg/a2a/server`. It represents one
execution segment and is the anchor consumers should read. Model and tool spans
come from the runtime itself and are descendants of it.

| Attribute | Meaning |
| --- | --- |
| `a2a.method` | `SendMessage` or `SendStreamingMessage` |
| `kagent.harness.kind` | `claude` or `codex`. Absent for ADK agents, which are not native harnesses |
| `gen_ai.agent.name` | The compiled agent identity, `<template>-<harness>` |
| `gen_ai.conversation.id` | The A2A context ID the gateway assigned |
| `gen_ai.task.id` | The A2A task ID the gateway assigned |
| `kagent.user_id` | The authenticated user, when the gateway forwarded one |
| `a2a.task.state` | The state execution actually reported. Its absence does not mean success |
| `kagent.invocation.segment` | `initial` or `resumed` |
| `kagent.invocation.disposition` | `canceled`, `abandoned`, or `interrupted`, when the task state does not say it. See below |
| `error.type` | A safe failure category. Never a provider response, credential, or captured content |
| `kagent.input`, `kagent.output` | Bounded turn content, present only under the capture opt-in |
| `kagent.input.truncated`, `kagent.output.truncated` | Whether that content was shortened |

The runtime resource carries `service.name` and `service.namespace` from the
compiled agent identity, plus `kagent.harness.kind` and `gen_ai.agent.name`. The
adapter merges those last two into `OTEL_RESOURCE_ATTRIBUTES` for the native
child process, so its spans report the same agent while keeping its own
`service.name`. Conversation, task, and user identity never appear on a
resource, since one runtime process serves many of each.

## Completion and ownership

The transport interceptor opens the span with the identity the runtime knows
before execution begins, so a request rejected during validation still reports
which agent rejected it. Execution then takes ownership, because a2a-go runs an
executor detached from the caller and a unary response can be delivered while
the turn is still working. Completion runs exactly once.

For runtimes whose Actor may be suspended as soon as a quiescent event leaves
the process, completion exports before that event is yielded. The export is
bounded by `KAGENT_TRACE_FLUSH_TIMEOUT_MS`, three seconds by default, so an
unreachable collector costs at most that budget once per segment.

A segment records `abandoned` when the A2A event consumer stopped accepting
events before execution finished, and `interrupted` when the execution context
ended without a cancellation request. Neither is reported as cancellation, which
is recorded only when a client asked for it. The consumer a runtime can observe
is the A2A event pipe rather than the network client, so a client that merely
closes a streaming subscription leaves execution running and is not visible
here.

Cancellation completes and exports the segment before the canceled event is
published, since that event is what releases the gateway to suspend the Actor.

## Approvals and resumed turns

A turn that pauses for approval keeps the same native process. The trace context
the native runtime received belongs to the request that started it, and neither
native protocol offers a supported way to replace it when the turn resumes. Work
the native runtime does after an approval therefore stays under the originating
trace.

Kagent does not paper over this. Each execution segment gets its own
`a2a.request` span carrying the same conversation and task identity, and a
resumed segment records an OpenTelemetry link back to the segment that parked
it, with `kagent.invocation.relationship` set to `resume_origin`. A link states
a relationship. It does not reparent spans and it does not transfer ownership of
the token usage recorded under the originating segment. Consumers should expect
several segments for one task and should not assume the last one owns the work.

## Content capture

Capture is off unless a user turns it on. When it is on, a segment records
the current turn's prompt and the text that segment produced, each bounded by
the configured byte budget, preserving UTF-8 and reporting truncation. Tool
arguments, tool results, approval structures, the rest of the conversation, and
native stderr are never recorded in these attributes. An approval decision stays
structured outcome metadata rather than prompt text.

Absent `kagent.input` and `kagent.output` mean capture is disabled. Present but
empty means the segment produced no text.

These attributes come from the Go wrapper, which is only one of the producers.
Suppressing them does not establish privacy for native runtime events or log
bodies, which have their own settings.

## Rollout

The compiled runtime configuration is versioned, and a runtime refuses a version
it does not understand. Publish the controller and harness images from the same
revision and move their pins together; a controller that is ahead of its pinned
harness images compiles configurations those images reject.

## Known limitations

- Native span export completeness at process shutdown is a separate concern from
  the Go wrapper's export, and is tracked against the native runtimes.
- Ownership of native work started after an approval is ambiguous by
  construction and is expressed as a link rather than asserted as a parent.
- a2a-go dispatches execution before it reads the caller's subscription. A
  caller that disconnects inside that window completes the invocation from the
  transport side, so that request's span reports a transport error without
  conversation or task identity while execution continues untraced.
- A request rejected by a transport interceptor before execution begins has no
  invocation span. Such a request never reaches an agent.
