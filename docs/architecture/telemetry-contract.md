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
| `kagent.gc.stage` | enum | `discovery`, `collection` | The stage of a runtime revision garbage collection attempt. |
| `kagent.invocation.disposition` | enum | `canceled`, `abandoned`, `interrupted` | How a segment stopped, when the task state does not say it. |
| `kagent.invocation.relationship` | enum | `resume_origin` | Why a segment links to another span. A link attribute. A link states a relationship. It does not reparent spans and it does not move the token usage recorded under the linked span. |
| `kagent.invocation.segment` | enum | `initial`, `resumed` | Whether an execution starts a task or continues it. A task that pauses for an approval or a question runs as several segments. Count turns by this attribute, not by invoke_agent spans. |
| `kagent.runtime` | enum | `adk-go`, `adk-python`, `claude`, `codex`, `langgraph`, `crewai`, `openai-agents`, `byo` | The runtime that produces the model and tool spans of an agent. Every runtime declares it on its resource. The name of a Harness object is not its runtime. |

## Metrics

### `gen_ai.invoke_agent.duration`

Refines `gen_ai.invoke_agent.duration` as `kagent.invoke_agent.duration`. Instrument: histogram. Unit: `s`. The duration of one execution segment of a kagent agent. Recorded with the invoke_agent span kagent opens, and equal to its duration. A runtime that emits its own invoke_agent span reports its own metric, if any.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `error.type` | conditionally required: The invocation failed. | The same bounded kagent vocabulary as on the span. |
| `gen_ai.agent.name` | required | The compiled agent identity, `<agent>`. |
| `gen_ai.request.model` | conditionally required: The runtime is a harness compiled against one model. | This attribute SHOULD be populated if and only if the instrumented library allows to set only a single model per agent. It SHOULD NOT be populated for agents that support multiple models or dynamic selection. |

### `kagent.runtime_revision.gc.duration`

Instrument: histogram. Unit: `s`. Duration of a runtime revision garbage collection attempt. Records each discovery and each collection attempt, including the database claim, Substrate read and deletion, and database finalization. A no-op claim is a successful attempt. Parent cancellation is excluded; an operation deadline while the parent remains active is recorded as a failure. Explicit bucket boundaries in seconds are 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, and 60.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `error.type` | conditionally required: The attempt failed. | The gRPC status code name for Substrate errors, or `_OTHER` for other failures. Absent on success. Never raw error text or revision, template, UID, or namespace identity. |
| `kagent.gc.stage` | required |  |

### `kagent.runtime_revision.gc.pending`

Instrument: gauge. Unit: `{revision}`. Number of cleanup-eligible runtime revisions in the last successful discovery. An observable integer gauge over a cached count, without collection-time database or network access. Absent before successful discovery and while inactive; zero means a successful empty discovery. Discovery errors retain the previous count while garbage collection is active.

| Attribute | Requirement | Note |
| --- | --- | --- |

## Attribute groups

### `kagent.baggage.outbound`

Tool identity propagated to an outbound request as W3C baggage.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `gen_ai.tool.call.id` | required |  |
| `gen_ai.tool.name` | required |  |

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
| `gen_ai.agent.id` | required | The agent identity qualified by its namespace, `<namespace>/<agent>`. |
| `gen_ai.agent.name` | required | The compiled agent identity, `<agent>`. |
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

### `kagent.resource`

The resource of a kagent runtime. The `kagent.main_agent` entity and the service identity the controller compiles. Live-check compares a resource with this group, since a resource is never compared with an entity.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `gen_ai.main_agent.id` | required | For main agents, this SHOULD be the provider-assigned stable identifier of the agent resource such as [AWS Bedrock agent ARN](https://docs.aws.amazon.com/bedrock/latest/APIReference/API_agent_Agent.html) or [GCP Agent Registry identifier](https://docs.cloud.google.com/agent-registry/concepts#agent-identifier). Instrumentation MUST NOT generate `gen_ai.main_agent.id` from an in-memory or process-local identifier. If no stable identifier is available, the agent entity is not emitted, and instrumentation continues to report `gen_ai.agent.name` at the span level when relevant. |
| `gen_ai.main_agent.name` | required | In Agent-to-Agent (A2A) communication, this maps to the `name` property declared in the Agent Card. |
| `gen_ai.provider.name` | conditionally required: The runtime is a harness compiled against one model. | Semantic conventions for individual GenAI operations SHOULD clarify which kinds of providers (e.g. inference, embeddings, retrieval, memory, hosted agent providers) apply when it is not clear from context. The attribute SHOULD be set based on the instrumentation's best knowledge and may differ from the actual upstream provider. For example, a client SDK may be configured against a proxy or hosting platform that transparently relays requests to a different provider. The `gen_ai.provider.name` attribute acts as a discriminator that identifies the GenAI telemetry format flavor specific to that provider within GenAI semantic conventions. It SHOULD be set consistently with provider-specific attributes and signals. For example, GenAI spans, metrics, and events related to AWS Bedrock should have the `gen_ai.provider.name` set to `aws.bedrock` and include applicable `aws.bedrock.*` attributes and are not expected to include `openai.*` attributes. |
| `gen_ai.request.model` | conditionally required: The runtime is a harness compiled against one model. | The ADK runtimes report the model on each inference span instead. |
| `kagent.runtime` | required | Every runtime declares it on its resource. The name of a Harness object is not its runtime. |
| `service.name` | required | The compiled agent identity, `<agent>`. |
| `service.namespace` | required | The agent namespace. |
| `service.version` | required | The short revision id. |

## Spans

### `kagent.a2a.request`

Kind `internal`. One execution segment of a runtime that emits its own invoke_agent span. The Go ADK, the Python ADK and OpenAI Agents describe the agent invocation themselves, so kagent opens this span instead of an invoke_agent span. It ends and exports before a quiescent event leaves the process, because the gateway may suspend the Actor on that event while the SERVER span is still open. It carries no `gen_ai.operation.name`.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `a2a.method` | required |  |
| `a2a.task.id` | conditionally required: The gateway assigned an A2A task. | The GenAI conventions have no task identity, so it stays in the A2A namespace. Never on a resource or a metric. |
| `a2a.task.state` | conditionally required: Execution reported a task state. | The values are the A2A v1 wire names. An absent state does not mean success. |
| `enduser.id` | conditionally required: A trusted identity reached the runtime. | Absent when the gateway forwarded no trusted identity. Never on a resource or a metric. Hashing the value is an operator decision. |
| `error.type` | conditionally required: The invocation failed. | A bounded kagent vocabulary: `runtime_panic`, `invalid_request`, `continuation_unavailable`, `actor_unavailable`, `runtime_error`, `invalid_runtime_outcome`, `invalid_input_request`, `runtime_failure` and `transport_error`. Never a provider response, a credential or captured content. |
| `gen_ai.agent.id` | required | The agent identity qualified by its namespace, `<namespace>/<agent>`. |
| `gen_ai.agent.name` | required | The compiled agent identity, `<agent>`. |
| `gen_ai.conversation.id` | conditionally required: The gateway assigned an A2A context. | The A2A context ID. |
| `gen_ai.provider.name` | conditionally required: The runtime is a harness compiled against one model. | kagent also writes `ollama` and `sap.ai_core`, which the conventions do not list. |
| `gen_ai.request.model` | conditionally required: The runtime is a harness compiled against one model. | The ADK runtimes report the model on each inference span instead. |
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
| `gen_ai.agent.id` | required | The agent identity qualified by its namespace, `<namespace>/<agent>`. |
| `gen_ai.agent.name` | required | The compiled agent identity, `<agent>`. |
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

### `kagent.main_agent`

Refines the `gen_ai.main_agent` entity. An agent runtime compiled by kagent. kagent sets `gen_ai.main_agent.id` to `<namespace>/<agent>` and `gen_ai.main_agent.name` to `<agent>`, both stable across revisions. Conversation, task and user identity never appear here, since one runtime process serves many of each.

| Attribute | Role | Requirement | Note |
| --- | --- | --- | --- |
| `gen_ai.main_agent.id` | identity | required | For main agents, this SHOULD be the provider-assigned stable identifier of the agent resource such as [AWS Bedrock agent ARN](https://docs.aws.amazon.com/bedrock/latest/APIReference/API_agent_Agent.html) or [GCP Agent Registry identifier](https://docs.cloud.google.com/agent-registry/concepts#agent-identifier). Instrumentation MUST NOT generate `gen_ai.main_agent.id` from an in-memory or process-local identifier. If no stable identifier is available, the agent entity is not emitted, and instrumentation continues to report `gen_ai.agent.name` at the span level when relevant. |
| `gen_ai.main_agent.description` | description | recommended | In Agent-to-Agent (A2A) communication, this maps to the `description` property declared in the Agent Card. |
| `gen_ai.main_agent.name` | description | required | The compiled agent identity, `<agent>`. |
| `gen_ai.provider.name` | description | conditionally required: The runtime is a harness compiled against one model. | The provider the agent is compiled against. |
| `gen_ai.request.model` | description | conditionally required: The runtime is a harness compiled against one model. | The model the agent is compiled against. |
| `kagent.runtime` | description | required | Every runtime declares it on its resource. The name of a Harness object is not its runtime. |


## Imported signals

The registry imports these upstream signals, so live-check compares the runtimes' own instrumentation with them.

Spans: `gen_ai.client.inference`, `gen_ai.execute_tool.internal`, `gen_ai.invoke_agent.internal`, `gen_ai.invoke_workflow.internal`, `http.client`, `http.server`, `rpc.call.client`, `rpc.call.server`.

Metrics: `gen_ai.client.inference.duration`, `gen_ai.client.inference.operation.input_tokens`, `gen_ai.client.inference.operation.output_tokens`, `gen_ai.client.inference.time_per_output_chunk`, `gen_ai.client.inference.time_to_first_chunk`, `gen_ai.client.inference.usage.cache_read.input_tokens`, `gen_ai.client.inference.usage.cache_write.input_tokens`, `gen_ai.client.inference.usage.input_tokens`, `gen_ai.client.inference.usage.output_tokens`, `gen_ai.client.inference.usage.reasoning.output_tokens`, `gen_ai.execute_tool.duration`, `gen_ai.invoke_agent.duration`, `gen_ai.invoke_workflow.duration`, `http.client.active_requests`, `http.client.connection.duration`, `http.client.open_connections`, `http.client.request.body.size`, `http.client.request.duration`, `http.client.response.body.size`, `http.server.active_requests`, `http.server.request.body.size`, `http.server.request.duration`, `http.server.response.body.size`, `rpc.client.call.duration`, `rpc.client.duration`, `rpc.client.request.size`, `rpc.client.requests_per_rpc`, `rpc.client.response.size`, `rpc.client.responses_per_rpc`, `rpc.server.call.duration`, `rpc.server.duration`, `rpc.server.request.size`, `rpc.server.requests_per_rpc`, `rpc.server.response.size`, `rpc.server.responses_per_rpc`.