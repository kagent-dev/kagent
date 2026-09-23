# Telemetry contract reference

<!-- Generated from telemetry/registry by `make semconv-generate`. Do not edit. -->

This page lists what the [registry](../../telemetry/registry) defines.
[Telemetry](telemetry.md) explains how kagent uses it.

## Attributes kagent owns

| Attribute | Type | Values | Description |
| --- | --- | --- | --- |
| `a2a.method` | enum | `SendMessage`, `SendStreamingMessage` | The A2A method that started the invocation. |
| `a2a.task.id` | string |  | The A2A task that an invocation executes. The GenAI conventions have no task identity, so it stays in the A2A namespace. Never on a resource or a metric. |
| `a2a.task.state` | enum | `TASK_STATE_SUBMITTED`, `TASK_STATE_WORKING`, `TASK_STATE_COMPLETED`, `TASK_STATE_FAILED`, `TASK_STATE_CANCELED`, `TASK_STATE_INPUT_REQUIRED`, `TASK_STATE_REJECTED`, `TASK_STATE_AUTH_REQUIRED` | The A2A task state that execution reported. The values are the A2A v1 wire names. An absent state does not mean success. |
| `kagent.capture.input_truncated` | boolean |  | Whether the captured input messages were shortened to the capture budget. |
| `kagent.capture.output_truncated` | boolean |  | Whether the captured output messages were shortened to the capture budget. |
| `kagent.invocation.disposition` | enum | `canceled`, `abandoned`, `interrupted` | How a segment stopped, when the task state does not say it. |
| `kagent.invocation.relationship` | enum | `resume_origin` | Why a segment links to another span. A link attribute. A link states a relationship. It does not reparent spans and it does not move the token usage recorded under the linked span. |
| `kagent.invocation.segment` | enum | `initial`, `resumed` | Whether an execution starts a task or continues it. A task that pauses for an approval or a question runs as several segments. Count turns by this attribute, not by invoke_agent spans. |
| `kagent.runtime` | enum | `adk-go`, `adk-python`, `claude`, `codex`, `langgraph`, `crewai`, `openai-agents`, `byo` | The runtime that produces the model and tool spans of an agent. Every runtime declares it on its resource. The name of a Harness object is not its runtime. |

## Attribute groups

### `kagent.capture`

Whether captured turn content was shortened. The content itself is `gen_ai.input.messages` and `gen_ai.output.messages` on the invoke_agent span, since a complex attribute cannot be part of a group.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `kagent.capture.input_truncated` | conditionally required: gen_ai.input.messages is present. |  |
| `kagent.capture.output_truncated` | conditionally required: gen_ai.output.messages is present. |  |

### `kagent.identity`

The compiled agent an invocation belongs to.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `gen_ai.agent.id` | required | The agent identity qualified by its namespace, `<namespace>/<template>-<harness>`. |
| `gen_ai.agent.name` | required | The compiled agent identity, `<template>-<harness>`. |
| `gen_ai.provider.name` | conditionally required: The runtime is a harness compiled against one model. | kagent also writes `ollama` and `sap.ai_core`, which the conventions do not list. |
| `gen_ai.request.model` | conditionally required: The runtime is a harness compiled against one model. | The ADK runtimes report the model on each inference span instead. |

### `kagent.outcome`

How an invocation ended.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `a2a.task.state` | conditionally required: Execution reported a task state. | The values are the A2A v1 wire names. An absent state does not mean success. |
| `error.type` | conditionally required: The invocation failed. | A bounded kagent vocabulary: `runtime_panic`, `invalid_request`, `continuation_unavailable`, `actor_unavailable`, `runtime_error`, `invalid_runtime_outcome`, `invalid_input_request`, `runtime_failure` and `transport_error`. Never a provider response, a credential or captured content. |
| `kagent.invocation.disposition` | conditionally required: The task state does not say how the segment stopped. |  |

### `kagent.request`

The A2A request an invocation serves.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `a2a.method` | required |  |
| `a2a.task.id` | conditionally required: The gateway assigned an A2A task. | The GenAI conventions have no task identity, so it stays in the A2A namespace. Never on a resource or a metric. |
| `enduser.id` | conditionally required: A trusted identity reached the runtime. | Absent when the gateway forwarded no trusted identity. Never on a resource or a metric. Hashing the value is an operator decision. |
| `gen_ai.conversation.id` | conditionally required: The gateway assigned an A2A context. | The A2A context ID. |
| `kagent.invocation.segment` | required | A task that pauses for an approval or a question runs as several segments. Count turns by this attribute, not by invoke_agent spans. |

## Spans

### `kagent.a2a.http.server`

Refines `http.server`, kind `server`. The instrumentation SERVER span of an A2A request over HTTP. kagent adds request identity and outcome to the span the HTTP instrumentation opens.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `a2a.method` | required |  |
| `a2a.task.id` | conditionally required: The gateway assigned an A2A task. | The GenAI conventions have no task identity, so it stays in the A2A namespace. Never on a resource or a metric. |
| `a2a.task.state` | conditionally required: Execution reported a task state. | The values are the A2A v1 wire names. An absent state does not mean success. |
| `enduser.id` | conditionally required: A trusted identity reached the runtime. | Absent when the gateway forwarded no trusted identity. Never on a resource or a metric. Hashing the value is an operator decision. |
| `error.type` | conditionally required: The invocation failed. | A bounded kagent vocabulary: `runtime_panic`, `invalid_request`, `continuation_unavailable`, `actor_unavailable`, `runtime_error`, `invalid_runtime_outcome`, `invalid_input_request`, `runtime_failure` and `transport_error`. Never a provider response, a credential or captured content. |
| `gen_ai.conversation.id` | conditionally required: The gateway assigned an A2A context. | The A2A context ID. |
| `kagent.invocation.disposition` | conditionally required: The task state does not say how the segment stopped. |  |
| `kagent.invocation.segment` | required | A task that pauses for an approval or a question runs as several segments. Count turns by this attribute, not by invoke_agent spans. |

### `kagent.a2a.rpc.server`

Refines `rpc.call.server`, kind `server`. The instrumentation SERVER span of an A2A request over gRPC. kagent adds request identity and outcome to the span the gRPC instrumentation opens.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `a2a.method` | required |  |
| `a2a.task.id` | conditionally required: The gateway assigned an A2A task. | The GenAI conventions have no task identity, so it stays in the A2A namespace. Never on a resource or a metric. |
| `a2a.task.state` | conditionally required: Execution reported a task state. | The values are the A2A v1 wire names. An absent state does not mean success. |
| `enduser.id` | conditionally required: A trusted identity reached the runtime. | Absent when the gateway forwarded no trusted identity. Never on a resource or a metric. Hashing the value is an operator decision. |
| `error.type` | conditionally required: The invocation failed. | A bounded kagent vocabulary: `runtime_panic`, `invalid_request`, `continuation_unavailable`, `actor_unavailable`, `runtime_error`, `invalid_runtime_outcome`, `invalid_input_request`, `runtime_failure` and `transport_error`. Never a provider response, a credential or captured content. |
| `gen_ai.conversation.id` | conditionally required: The gateway assigned an A2A context. | The A2A context ID. |
| `kagent.invocation.disposition` | conditionally required: The task state does not say how the segment stopped. |  |
| `kagent.invocation.segment` | required | A task that pauses for an approval or a question runs as several segments. Count turns by this attribute, not by invoke_agent spans. |

### `kagent.invoke_agent.internal`

Refines `gen_ai.invoke_agent.internal`, kind `internal`. One execution segment of a kagent agent. Named `invoke_agent {gen_ai.agent.name}`. kagent opens it only for a runtime that emits no invoke_agent span of its own. A resumed segment links to the segment that started the native turn, with `kagent.invocation.relationship` set to `resume_origin`.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `a2a.method` | required |  |
| `a2a.task.id` | conditionally required: The gateway assigned an A2A task. | The GenAI conventions have no task identity, so it stays in the A2A namespace. Never on a resource or a metric. |
| `a2a.task.state` | conditionally required: Execution reported a task state. | The values are the A2A v1 wire names. An absent state does not mean success. |
| `enduser.id` | conditionally required: A trusted identity reached the runtime. | Absent when the gateway forwarded no trusted identity. Never on a resource or a metric. Hashing the value is an operator decision. |
| `error.type` | conditionally required: The invocation failed. | A bounded kagent vocabulary: `runtime_panic`, `invalid_request`, `continuation_unavailable`, `actor_unavailable`, `runtime_error`, `invalid_runtime_outcome`, `invalid_input_request`, `runtime_failure` and `transport_error`. Never a provider response, a credential or captured content. |
| `gen_ai.agent.id` | required | The agent identity qualified by its namespace, `<namespace>/<template>-<harness>`. |
| `gen_ai.agent.name` | required | The compiled agent identity, `<template>-<harness>`. |
| `gen_ai.conversation.id` | conditionally required: The gateway assigned an A2A context. | The A2A context ID. |
| `gen_ai.input.messages` | opt in | Bounded turn content, recorded only under `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT=SPAN_ONLY`. |
| `gen_ai.operation.name` | required | Always `invoke_agent`. |
| `gen_ai.output.messages` | opt in | Bounded turn content, recorded only under `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT=SPAN_ONLY`. |
| `gen_ai.provider.name` | conditionally required: The runtime is a harness compiled against one model. | kagent also writes `ollama` and `sap.ai_core`, which the conventions do not list. |
| `gen_ai.request.model` | conditionally required: The runtime is a harness compiled against one model. | The ADK runtimes report the model on each inference span instead. |
| `kagent.capture.input_truncated` | conditionally required: gen_ai.input.messages is present. |  |
| `kagent.capture.output_truncated` | conditionally required: gen_ai.output.messages is present. |  |
| `kagent.invocation.disposition` | conditionally required: The task state does not say how the segment stopped. |  |
| `kagent.invocation.segment` | required | A task that pauses for an approval or a question runs as several segments. Count turns by this attribute, not by invoke_agent spans. |

## Resource

### `kagent.agent`

An agent runtime compiled by kagent. Conversation, task and user identity never appear here, since one runtime process serves many of each.

| Attribute | Role | Requirement | Note |
| --- | --- | --- | --- |
| `gen_ai.agent.id` | identity | required | The agent identity qualified by its namespace, `<namespace>/<template>-<harness>`. |
| `gen_ai.agent.name` | description | required | The compiled agent identity, `<template>-<harness>`. |
| `gen_ai.provider.name` | description | conditionally required: The runtime is a harness compiled against one model. | The provider the agent is compiled against. |
| `gen_ai.request.model` | description | conditionally required: The runtime is a harness compiled against one model. | The model the agent is compiled against. |
| `kagent.runtime` | description | required | Every runtime declares it on its resource. The name of a Harness object is not its runtime. |
