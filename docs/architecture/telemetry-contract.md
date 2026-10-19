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
| `kagent.capture.input_truncated` | boolean |  | Whether the captured input messages or tool arguments were shortened to the capture budget. |
| `kagent.capture.output_truncated` | boolean |  | Whether the captured output messages or tool result were shortened to the capture budget. |
| `kagent.gc.stage` | enum | `discovery`, `collection` | The stage of a runtime revision garbage collection attempt. |
| `kagent.genai.producer` | enum | `runtime` | The component that writes the GenAI model and tool spans of an agent. Set on the resource of a native harness process. With the value `runtime`, the kagent harness runtime writes the model and tool spans of this process. Its native spans repeat the same calls, so the reference collector removes their GenAI operation, model, finish reason and token usage attributes. Each call is then counted once. A standalone Claude request keeps them, since the runtime writes no span for it. Absent when the image writes its own native GenAI spans. |
| `kagent.inference.request_id` | string |  | The provider's request ID of a model call. Lets a backend join the call with the provider's logs. Claude only. A call canceled early can lack it. |
| `kagent.inference.retry.count` | int |  | How many times the harness retried a model call after a transient error. Set only when the call was retried. The chat span covers all attempts. Codex counts its reported retries, not the HTTP retries inside each one. Over a WebSocket, Codex does not report its first retry. Claude also counts a new stream that replaces one cut off mid-response. |
| `kagent.inference.usage_partial` | boolean |  | Whether the call ended without final usage, so its output tokens are a lower bound. Set only when true. Input tokens, when present, stay exact. Calls cut short and Claude subagent calls can set it. |
| `kagent.invocation.disposition` | enum | `canceled`, `abandoned`, `interrupted` | How a segment stopped, when the task state does not say it. |
| `kagent.invocation.relationship` | enum | `resume_origin` | Why a segment links to another span. A link attribute. A link states a relationship. It does not reparent spans and it does not move the token usage recorded under the linked span. |
| `kagent.invocation.segment` | enum | `initial`, `resumed` | Whether an execution starts a task or continues it. A task that pauses for an approval or a question runs as several segments. Count turns by this attribute, not by invoke_agent spans. |
| `kagent.mcp.server.name` | string |  | The name of the MCP server that serves a tool call. The value is the server name in the runtime's MCP configuration. A `.` in a RemoteMCPServer name shows as `_`. The `gen_ai.tool.name` attribute holds the tool name without the server, so the same tool groups together in all runtimes. |
| `kagent.runtime` | enum | `adk-go`, `adk-python`, `claude`, `codex`, `langgraph`, `crewai`, `openai-agents`, `byo` | The framework or harness that runs an agent. Every runtime declares it on its resource. The name of a Harness object is not its runtime. |

## Metrics

### `gen_ai.client.inference.duration`

Refines `gen_ai.client.inference.duration` as `kagent.client.inference.duration`. Instrument: histogram. Unit: `s`. The duration of one model call of a harness turn. Recorded with the chat span, and equal to its duration. Not recorded for a zero-length call, such as a Codex call from a thread not known yet. Codex reports no call times, so its durations are estimates, as on the chat span.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `error.type` | conditionally required: The call failed. | The same bounded kagent vocabulary as on the chat span. |
| `gen_ai.operation.name` | required | Always `chat`. |
| `gen_ai.provider.name` | conditionally required: The runtime is a harness compiled against one model. | Semantic conventions for individual GenAI operations SHOULD clarify which kinds of providers (e.g. inference, embeddings, retrieval, memory, hosted agent providers) apply when it is not clear from context. The attribute SHOULD be set based on the instrumentation's best knowledge and may differ from the actual upstream provider. For example, a client SDK may be configured against a proxy or hosting platform that transparently relays requests to a different provider. The `gen_ai.provider.name` attribute acts as a discriminator that identifies the GenAI telemetry format flavor specific to that provider within GenAI semantic conventions. It SHOULD be set consistently with provider-specific attributes and signals. For example, GenAI spans, metrics, and events related to AWS Bedrock should have the `gen_ai.provider.name` set to `aws.bedrock` and include applicable `aws.bedrock.*` attributes and are not expected to include `openai.*` attributes. |
| `gen_ai.request.model` | conditionally required: The runtime is a harness compiled against one model. | The name of the GenAI model a request is being made to. If the model is supplied by a vendor, then the value must be the exact name of the model requested. If the model is a fine-tuned custom model, the value should have a more specific name than the base model that's been fine-tuned. |
| `gen_ai.response.model` | recommended |  |
| `server.address` | recommended | When observed from the client side, and when communicating through an intermediary, `server.address` SHOULD represent the server address behind any intermediaries, for example proxies, if it's available. |
| `server.port` | conditionally required: If `server.address` is set. | When observed from the client side, and when communicating through an intermediary, `server.port` SHOULD represent the server port behind any intermediaries, for example proxies, if it's available. |

### `gen_ai.client.inference.operation.input_tokens`

Refines `gen_ai.client.inference.operation.input_tokens` as `kagent.client.inference.operation.input_tokens`. Instrument: histogram. Unit: `{token}`. The input tokens of one model call of a harness turn. Recorded with each chat span that reports usage. The input count of a partial call is exact, so it is recorded too.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `gen_ai.operation.name` | required | Always `chat`. |
| `gen_ai.provider.name` | conditionally required: The runtime is a harness compiled against one model. | Semantic conventions for individual GenAI operations SHOULD clarify which kinds of providers (e.g. inference, embeddings, retrieval, memory, hosted agent providers) apply when it is not clear from context. The attribute SHOULD be set based on the instrumentation's best knowledge and may differ from the actual upstream provider. For example, a client SDK may be configured against a proxy or hosting platform that transparently relays requests to a different provider. The `gen_ai.provider.name` attribute acts as a discriminator that identifies the GenAI telemetry format flavor specific to that provider within GenAI semantic conventions. It SHOULD be set consistently with provider-specific attributes and signals. For example, GenAI spans, metrics, and events related to AWS Bedrock should have the `gen_ai.provider.name` set to `aws.bedrock` and include applicable `aws.bedrock.*` attributes and are not expected to include `openai.*` attributes. |
| `gen_ai.request.model` | conditionally required: The runtime is a harness compiled against one model. | The name of the GenAI model a request is being made to. If the model is supplied by a vendor, then the value must be the exact name of the model requested. If the model is a fine-tuned custom model, the value should have a more specific name than the base model that's been fine-tuned. |
| `gen_ai.response.model` | recommended |  |
| `server.address` | recommended | When observed from the client side, and when communicating through an intermediary, `server.address` SHOULD represent the server address behind any intermediaries, for example proxies, if it's available. |
| `server.port` | conditionally required: If `server.address` is set. | When observed from the client side, and when communicating through an intermediary, `server.port` SHOULD represent the server port behind any intermediaries, for example proxies, if it's available. |

### `gen_ai.client.inference.operation.output_tokens`

Refines `gen_ai.client.inference.operation.output_tokens` as `kagent.client.inference.operation.output_tokens`. Instrument: histogram. Unit: `{token}`. The output tokens of one model call of a harness turn. Recorded with each chat span that reports usage. Not recorded for a partial call, since its output count is a lower bound.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `gen_ai.operation.name` | required | Always `chat`. |
| `gen_ai.provider.name` | conditionally required: The runtime is a harness compiled against one model. | Semantic conventions for individual GenAI operations SHOULD clarify which kinds of providers (e.g. inference, embeddings, retrieval, memory, hosted agent providers) apply when it is not clear from context. The attribute SHOULD be set based on the instrumentation's best knowledge and may differ from the actual upstream provider. For example, a client SDK may be configured against a proxy or hosting platform that transparently relays requests to a different provider. The `gen_ai.provider.name` attribute acts as a discriminator that identifies the GenAI telemetry format flavor specific to that provider within GenAI semantic conventions. It SHOULD be set consistently with provider-specific attributes and signals. For example, GenAI spans, metrics, and events related to AWS Bedrock should have the `gen_ai.provider.name` set to `aws.bedrock` and include applicable `aws.bedrock.*` attributes and are not expected to include `openai.*` attributes. |
| `gen_ai.request.model` | conditionally required: The runtime is a harness compiled against one model. | The name of the GenAI model a request is being made to. If the model is supplied by a vendor, then the value must be the exact name of the model requested. If the model is a fine-tuned custom model, the value should have a more specific name than the base model that's been fine-tuned. |
| `gen_ai.response.model` | recommended |  |
| `server.address` | recommended | When observed from the client side, and when communicating through an intermediary, `server.address` SHOULD represent the server address behind any intermediaries, for example proxies, if it's available. |
| `server.port` | conditionally required: If `server.address` is set. | When observed from the client side, and when communicating through an intermediary, `server.port` SHOULD represent the server port behind any intermediaries, for example proxies, if it's available. |

### `gen_ai.client.inference.usage.cache_read.input_tokens`

Refines `gen_ai.client.inference.usage.cache_read.input_tokens` as `kagent.client.inference.usage.cache_read.input_tokens`. Instrument: counter. Unit: `{token}`. The cache read input tokens of the model calls of harness turns. Added with each chat span, with the same count. Not added when the count is zero. A partial call adds its counts, which are a lower bound. At the end of a Claude turn, the runtime also adds the tokens that Claude's own total shows but no chat span has, such as most of a subagent's output.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `gen_ai.operation.name` | required | Always `chat`. |
| `gen_ai.provider.name` | conditionally required: The runtime is a harness compiled against one model. | Semantic conventions for individual GenAI operations SHOULD clarify which kinds of providers (e.g. inference, embeddings, retrieval, memory, hosted agent providers) apply when it is not clear from context. The attribute SHOULD be set based on the instrumentation's best knowledge and may differ from the actual upstream provider. For example, a client SDK may be configured against a proxy or hosting platform that transparently relays requests to a different provider. The `gen_ai.provider.name` attribute acts as a discriminator that identifies the GenAI telemetry format flavor specific to that provider within GenAI semantic conventions. It SHOULD be set consistently with provider-specific attributes and signals. For example, GenAI spans, metrics, and events related to AWS Bedrock should have the `gen_ai.provider.name` set to `aws.bedrock` and include applicable `aws.bedrock.*` attributes and are not expected to include `openai.*` attributes. |
| `gen_ai.request.model` | conditionally required: The runtime is a harness compiled against one model. | The name of the GenAI model a request is being made to. If the model is supplied by a vendor, then the value must be the exact name of the model requested. If the model is a fine-tuned custom model, the value should have a more specific name than the base model that's been fine-tuned. |
| `gen_ai.response.model` | recommended |  |
| `gen_ai.token.modality` | required | Always `unknown`, since the runtimes report no count by modality. |
| `server.address` | recommended | When observed from the client side, and when communicating through an intermediary, `server.address` SHOULD represent the server address behind any intermediaries, for example proxies, if it's available. |
| `server.port` | conditionally required: If `server.address` is set. | When observed from the client side, and when communicating through an intermediary, `server.port` SHOULD represent the server port behind any intermediaries, for example proxies, if it's available. |

### `gen_ai.client.inference.usage.cache_write.input_tokens`

Refines `gen_ai.client.inference.usage.cache_write.input_tokens` as `kagent.client.inference.usage.cache_write.input_tokens`. Instrument: counter. Unit: `{token}`. The cache write input tokens of the model calls of harness turns. Added with each chat span, with the same count. Not added when the count is zero. A partial call adds its counts, which are a lower bound. At the end of a Claude turn, the runtime also adds the tokens that Claude's own total shows but no chat span has, such as most of a subagent's output.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `gen_ai.operation.name` | required | Always `chat`. |
| `gen_ai.provider.name` | conditionally required: The runtime is a harness compiled against one model. | Semantic conventions for individual GenAI operations SHOULD clarify which kinds of providers (e.g. inference, embeddings, retrieval, memory, hosted agent providers) apply when it is not clear from context. The attribute SHOULD be set based on the instrumentation's best knowledge and may differ from the actual upstream provider. For example, a client SDK may be configured against a proxy or hosting platform that transparently relays requests to a different provider. The `gen_ai.provider.name` attribute acts as a discriminator that identifies the GenAI telemetry format flavor specific to that provider within GenAI semantic conventions. It SHOULD be set consistently with provider-specific attributes and signals. For example, GenAI spans, metrics, and events related to AWS Bedrock should have the `gen_ai.provider.name` set to `aws.bedrock` and include applicable `aws.bedrock.*` attributes and are not expected to include `openai.*` attributes. |
| `gen_ai.request.model` | conditionally required: The runtime is a harness compiled against one model. | The name of the GenAI model a request is being made to. If the model is supplied by a vendor, then the value must be the exact name of the model requested. If the model is a fine-tuned custom model, the value should have a more specific name than the base model that's been fine-tuned. |
| `gen_ai.response.model` | recommended |  |
| `gen_ai.token.modality` | required | Always `unknown`, since the runtimes report no count by modality. |
| `server.address` | recommended | When observed from the client side, and when communicating through an intermediary, `server.address` SHOULD represent the server address behind any intermediaries, for example proxies, if it's available. |
| `server.port` | conditionally required: If `server.address` is set. | When observed from the client side, and when communicating through an intermediary, `server.port` SHOULD represent the server port behind any intermediaries, for example proxies, if it's available. |

### `gen_ai.client.inference.usage.input_tokens`

Refines `gen_ai.client.inference.usage.input_tokens` as `kagent.client.inference.usage.input_tokens`. Instrument: counter. Unit: `{token}`. The input tokens of the model calls of harness turns. Added with each chat span, with the same count. Not added when the count is zero. A partial call adds its counts, which are a lower bound. At the end of a Claude turn, the runtime also adds the tokens that Claude's own total shows but no chat span has, such as most of a subagent's output.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `gen_ai.operation.name` | required | Always `chat`. |
| `gen_ai.provider.name` | conditionally required: The runtime is a harness compiled against one model. | Semantic conventions for individual GenAI operations SHOULD clarify which kinds of providers (e.g. inference, embeddings, retrieval, memory, hosted agent providers) apply when it is not clear from context. The attribute SHOULD be set based on the instrumentation's best knowledge and may differ from the actual upstream provider. For example, a client SDK may be configured against a proxy or hosting platform that transparently relays requests to a different provider. The `gen_ai.provider.name` attribute acts as a discriminator that identifies the GenAI telemetry format flavor specific to that provider within GenAI semantic conventions. It SHOULD be set consistently with provider-specific attributes and signals. For example, GenAI spans, metrics, and events related to AWS Bedrock should have the `gen_ai.provider.name` set to `aws.bedrock` and include applicable `aws.bedrock.*` attributes and are not expected to include `openai.*` attributes. |
| `gen_ai.request.model` | conditionally required: The runtime is a harness compiled against one model. | The name of the GenAI model a request is being made to. If the model is supplied by a vendor, then the value must be the exact name of the model requested. If the model is a fine-tuned custom model, the value should have a more specific name than the base model that's been fine-tuned. |
| `gen_ai.response.model` | recommended |  |
| `gen_ai.token.modality` | required | Always `unknown`, since the runtimes report no count by modality. |
| `server.address` | recommended | When observed from the client side, and when communicating through an intermediary, `server.address` SHOULD represent the server address behind any intermediaries, for example proxies, if it's available. |
| `server.port` | conditionally required: If `server.address` is set. | When observed from the client side, and when communicating through an intermediary, `server.port` SHOULD represent the server port behind any intermediaries, for example proxies, if it's available. |

### `gen_ai.client.inference.usage.output_tokens`

Refines `gen_ai.client.inference.usage.output_tokens` as `kagent.client.inference.usage.output_tokens`. Instrument: counter. Unit: `{token}`. The output tokens of the model calls of harness turns. Added with each chat span, with the same count. Not added when the count is zero. A partial call adds its counts, which are a lower bound. At the end of a Claude turn, the runtime also adds the tokens that Claude's own total shows but no chat span has, such as most of a subagent's output.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `gen_ai.operation.name` | required | Always `chat`. |
| `gen_ai.provider.name` | conditionally required: The runtime is a harness compiled against one model. | Semantic conventions for individual GenAI operations SHOULD clarify which kinds of providers (e.g. inference, embeddings, retrieval, memory, hosted agent providers) apply when it is not clear from context. The attribute SHOULD be set based on the instrumentation's best knowledge and may differ from the actual upstream provider. For example, a client SDK may be configured against a proxy or hosting platform that transparently relays requests to a different provider. The `gen_ai.provider.name` attribute acts as a discriminator that identifies the GenAI telemetry format flavor specific to that provider within GenAI semantic conventions. It SHOULD be set consistently with provider-specific attributes and signals. For example, GenAI spans, metrics, and events related to AWS Bedrock should have the `gen_ai.provider.name` set to `aws.bedrock` and include applicable `aws.bedrock.*` attributes and are not expected to include `openai.*` attributes. |
| `gen_ai.request.model` | conditionally required: The runtime is a harness compiled against one model. | The name of the GenAI model a request is being made to. If the model is supplied by a vendor, then the value must be the exact name of the model requested. If the model is a fine-tuned custom model, the value should have a more specific name than the base model that's been fine-tuned. |
| `gen_ai.response.model` | recommended |  |
| `gen_ai.token.modality` | required | Always `unknown`, since the runtimes report no count by modality. |
| `server.address` | recommended | When observed from the client side, and when communicating through an intermediary, `server.address` SHOULD represent the server address behind any intermediaries, for example proxies, if it's available. |
| `server.port` | conditionally required: If `server.address` is set. | When observed from the client side, and when communicating through an intermediary, `server.port` SHOULD represent the server port behind any intermediaries, for example proxies, if it's available. |

### `gen_ai.client.inference.usage.reasoning.output_tokens`

Refines `gen_ai.client.inference.usage.reasoning.output_tokens` as `kagent.client.inference.usage.reasoning.output_tokens`. Instrument: counter. Unit: `{token}`. The reasoning output tokens of the model calls of harness turns. Added with each chat span, with the same count. Not added when the count is zero. A partial call adds its counts, which are a lower bound. At the end of a Claude turn, the runtime also adds the tokens that Claude's own total shows but no chat span has, such as most of a subagent's output.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `gen_ai.operation.name` | required | Always `chat`. |
| `gen_ai.provider.name` | conditionally required: The runtime is a harness compiled against one model. | Semantic conventions for individual GenAI operations SHOULD clarify which kinds of providers (e.g. inference, embeddings, retrieval, memory, hosted agent providers) apply when it is not clear from context. The attribute SHOULD be set based on the instrumentation's best knowledge and may differ from the actual upstream provider. For example, a client SDK may be configured against a proxy or hosting platform that transparently relays requests to a different provider. The `gen_ai.provider.name` attribute acts as a discriminator that identifies the GenAI telemetry format flavor specific to that provider within GenAI semantic conventions. It SHOULD be set consistently with provider-specific attributes and signals. For example, GenAI spans, metrics, and events related to AWS Bedrock should have the `gen_ai.provider.name` set to `aws.bedrock` and include applicable `aws.bedrock.*` attributes and are not expected to include `openai.*` attributes. |
| `gen_ai.request.model` | conditionally required: The runtime is a harness compiled against one model. | The name of the GenAI model a request is being made to. If the model is supplied by a vendor, then the value must be the exact name of the model requested. If the model is a fine-tuned custom model, the value should have a more specific name than the base model that's been fine-tuned. |
| `gen_ai.response.model` | recommended |  |
| `gen_ai.token.modality` | required | Always `unknown`, since the runtimes report no count by modality. |
| `server.address` | recommended | When observed from the client side, and when communicating through an intermediary, `server.address` SHOULD represent the server address behind any intermediaries, for example proxies, if it's available. |
| `server.port` | conditionally required: If `server.address` is set. | When observed from the client side, and when communicating through an intermediary, `server.port` SHOULD represent the server port behind any intermediaries, for example proxies, if it's available. |

### `gen_ai.execute_tool.duration`

Refines `gen_ai.execute_tool.duration` as `kagent.execute_tool.duration`. Instrument: histogram. Unit: `s`. The duration of one tool call of a harness turn. Recorded with the execute_tool span, and equal to its duration. A declined call has no span, so it has no duration either.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `error.type` | conditionally required: The call failed or never got a result. | The same bounded kagent vocabulary as on the execute_tool span. |
| `gen_ai.agent.name` | conditionally required: The tool is the agent's own, not a subagent's. | The compiled agent identity, `<agent>`. Absent on a subagent's tool, since Claude Code and Codex report no subagent name. |
| `gen_ai.tool.name` | required | The plain MCP tool name, as on the execute_tool span. |
| `gen_ai.tool.type` | recommended: If available. | Extension: A tool executed on the agent-side to directly call external APIs, bridging the gap between the agent and real-world systems. Agent-side operations involve actions that are performed by the agent on the server or within the agent's controlled environment. Function: A tool executed on the client-side, where the agent generates parameters for a predefined function, and the client executes the logic. Client-side operations are actions taken on the user's end or within the client application. Datastore: A tool used by the agent to access and query structured or unstructured external data for retrieval-augmented tasks or knowledge updates. |

### `gen_ai.invoke_agent.duration`

Refines `gen_ai.invoke_agent.duration` as `kagent.invoke_agent.duration`. Instrument: histogram. Unit: `s`. The duration of one execution segment of a kagent agent. Recorded with the invoke_agent span kagent opens, and equal to its duration. A runtime that emits its own invoke_agent span reports its own metric, if any.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `error.type` | conditionally required: The invocation failed. | The same bounded kagent vocabulary as on the span. |
| `gen_ai.agent.name` | required | The compiled agent identity, `<agent>`. |
| `gen_ai.request.model` | conditionally required: The runtime is a harness compiled against one model. | This attribute SHOULD be populated if and only if the instrumented library allows to set only a single model per agent. It SHOULD NOT be populated for agents that support multiple models or dynamic selection. |

### `gen_ai.invoke_agent.inference_calls`

Refines `gen_ai.invoke_agent.inference_calls` as `kagent.invoke_agent.inference_calls`. Instrument: histogram. Unit: `{inference_call}`. The model calls of one execution segment of a harness agent. Recorded when the invoke_agent span kagent opens ends, if the agent ran. Counts only the agent's own model calls, so a subagent's calls are not counted. A call that names an unknown parent tool is not counted either. A Codex call whose usage arrives after a pause counts in the later segment. A call reported when a paused turn is canceled is not counted.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `gen_ai.agent.name` | required | The compiled agent identity, `<agent>`. |

### `gen_ai.invoke_agent.tool_calls`

Refines `gen_ai.invoke_agent.tool_calls` as `kagent.invoke_agent.tool_calls`. Instrument: histogram. Unit: `{tool_call}`. The tool calls of one execution segment of a harness agent. Recorded when the invoke_agent span kagent opens ends, if the agent ran. Counts only the agent's own tool calls, declined calls included, so a subagent's tools are not counted. A tool that names an unknown parent tool is not counted either.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `gen_ai.agent.name` | required | The compiled agent identity, `<agent>`. |

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
| `gen_ai.tool.name` | required | For an MCP tool, the MCP tool name without the server. |

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

### `kagent.inference`

What one model request returned.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `gen_ai.response.finish_reasons` | recommended: The runtime reported it. | Claude stop reasons map to `stop`, `tool_call`, `length` or `content_filter`. Codex calls have only `error`, set for a failed call. |
| `gen_ai.response.id` | recommended: The runtime reported it. | The Claude message ID. Codex reports none. |
| `gen_ai.response.model` | recommended: The runtime reported it. | The model the harness reported. |
| `gen_ai.usage.cache_read.input_tokens` | recommended: The runtime reported a non-zero count. | The value SHOULD be included in `gen_ai.usage.input_tokens`. |
| `gen_ai.usage.cache_write.input_tokens` | recommended: The runtime reported a non-zero count. | The value SHOULD be included in `gen_ai.usage.input_tokens`. |
| `gen_ai.usage.input_tokens` | recommended | Includes cached tokens. Absent when the runtime reported no usage. |
| `gen_ai.usage.output_tokens` | recommended | Absent when the runtime reported no usage. |
| `gen_ai.usage.reasoning.output_tokens` | recommended: The runtime reported a non-zero count. | Included in gen_ai.usage.output_tokens. |
| `kagent.inference.request_id` | recommended: The runtime reported it. | The Claude request ID. Codex reports none. |
| `kagent.inference.retry.count` | conditionally required: The harness retried the call. | Set only when the call was retried. The chat span covers all attempts. Codex counts its reported retries, not the HTTP retries inside each one. Over a WebSocket, Codex does not report its first retry. Claude also counts a new stream that replaces one cut off mid-response. |
| `kagent.inference.usage_partial` | conditionally required: The call ended without final usage. | Set only when true. Input tokens, when present, stay exact. Calls cut short and Claude subagent calls can set it. |

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

### `kagent.tool_call`

One tool call of an invocation.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `error.type` | conditionally required: The tool reported a failure or never finished. | `tool_error` when the tool reported a failure, `unfinished` when its result never arrived. Never the tool output. |
| `gen_ai.tool.call.id` | required |  |
| `gen_ai.tool.name` | required | For an MCP tool, the MCP tool name without the server. |
| `kagent.mcp.server.name` | conditionally required: The tool is an MCP tool. | The value is the server name in the runtime's MCP configuration. A `.` in a RemoteMCPServer name shows as `_`. The `gen_ai.tool.name` attribute holds the tool name without the server, so the same tool groups together in all runtimes. |

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

### `kagent.execute_tool.internal`

Refines `gen_ai.execute_tool.internal`, kind `internal`. One tool call of a harness turn. Named `execute_tool {gen_ai.tool.name}`. The span runs from the tool call to its result. A subagent tool goes under the execute_tool span of the tool call that started the subagent when that parent is known in the segment. Other tools go under the invoke_agent span. For Claude and Codex DATA continuations, non-gated calls still open at input publication are recorded as unfinished; this indicates a missing final result, not proof of remote failure. An approved deferred attempt is recorded in the resumed segment, excluding approval wait time. Claude includes replay startup; Codex includes the harness MCP caller setup and cleanup. Proven pre-execution failures suppress the span. Denial and supplying a cached result create no execution span. Previous tool parents are not restored across suspension.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `error.type` | conditionally required: The tool reported a failure or never finished. | `tool_error` when the tool reported a failure, `unfinished` when its result never arrived. Never the tool output. |
| `gen_ai.agent.name` | conditionally required: When applicable. |  |
| `gen_ai.conversation.id` | conditionally required: The gateway assigned an A2A context. | The A2A context ID. |
| `gen_ai.operation.name` | required | Always `execute_tool`. |
| `gen_ai.tool.call.arguments` | opt in | The tool input as JSON. It can hold sensitive data, so it is recorded only under `OTEL_INSTRUMENTATION_GENAI_CAPTURE_MESSAGE_CONTENT=SPAN_ONLY`. It is cut to the same byte budget as `gen_ai.input.messages`, so a cut value is not valid JSON. |
| `gen_ai.tool.call.id` | required |  |
| `gen_ai.tool.call.result` | opt in | The tool result as JSON, which can hold sensitive data, under the same rule and budget. Absent when the call failed or never finished. |
| `gen_ai.tool.name` | required | For an MCP tool, the MCP tool name without the server. |
| `kagent.capture.input_truncated` | conditionally required: gen_ai.tool.call.arguments is present. |  |
| `kagent.capture.output_truncated` | conditionally required: gen_ai.tool.call.result is present. |  |
| `kagent.mcp.server.name` | conditionally required: The tool is an MCP tool. | The value is the server name in the runtime's MCP configuration. A `.` in a RemoteMCPServer name shows as `_`. The `gen_ai.tool.name` attribute holds the tool name without the server, so the same tool groups together in all runtimes. |

### `kagent.inference.client`

Refines `gen_ai.client.inference`, kind `client`. One model request of a harness turn. Named `chat {gen_ai.request.model}`, or `chat` when the request model is unknown. A subagent model call goes under the execute_tool span of the tool call that started the subagent when that parent is known in the segment. Examples are the Claude `Agent` tool and the Codex `spawnAgent` call. For Claude and Codex DATA continuations, parent bookkeeping is local to an execution segment; it is not restored across suspension. Other model calls go under the current invoke_agent span. A subagent model call can start outside the time of its parent span. A Codex `spawnAgent` call ends when the subagent starts, so the subagent model calls start after that span ends. A call that ended with finish reason `error` has an error status, unless it was canceled. One span covers a call and all its automatic retries, and only a final failure gives it an error status. A Claude call's token counts add up every attempt that the provider started. Codex reports usage only for the attempt that succeeded, and none for a call canceled mid-response. Codex errors of the `other` and `badRequest` categories can also come from outside a model request, and they still give a failed call. Codex reports no call times, so a Codex call starts at the previous call's usage report or final error. It ends when its first tool starts or the turn pauses for input, or else at its own usage report or final error. The first call of a Codex subagent starts when its `spawnAgent` call first names its thread.

| Attribute | Requirement | Note |
| --- | --- | --- |
| `error.type` | conditionally required: The call ended with finish reason `error` and was not canceled. | The error category from the harness, such as `rate_limit`, or `model_error` when the harness gives none. A canceled call keeps finish reason `error`, as the conventions require, but has no error status. |
| `gen_ai.conversation.id` | conditionally required: The gateway assigned an A2A context. | The A2A context ID. |
| `gen_ai.input.messages` | opt in | Never set here. With content capture on, the turn content is on the invoke_agent span. |
| `gen_ai.operation.name` | required | Always `chat`. |
| `gen_ai.output.messages` | opt in | Never set here. With content capture on, the turn content is on the invoke_agent span. |
| `gen_ai.provider.name` | conditionally required: The runtime is a harness compiled against one model. | The provider the agent is compiled against. |
| `gen_ai.request.model` | conditionally required: The runtime is a harness compiled against one model. | The model the agent is compiled against. |
| `gen_ai.response.finish_reasons` | recommended: The runtime reported it. | Claude stop reasons map to `stop`, `tool_call`, `length` or `content_filter`. Codex calls have only `error`, set for a failed call. |
| `gen_ai.response.id` | recommended: The runtime reported it. | The Claude message ID. Codex reports none. |
| `gen_ai.response.model` | recommended: The runtime reported it. | The model the harness reported. |
| `gen_ai.usage.cache_read.input_tokens` | recommended: The runtime reported a non-zero count. | The value SHOULD be included in `gen_ai.usage.input_tokens`. |
| `gen_ai.usage.cache_write.input_tokens` | recommended: The runtime reported a non-zero count. | The value SHOULD be included in `gen_ai.usage.input_tokens`. |
| `gen_ai.usage.input_tokens` | recommended | Includes cached tokens. Absent when the runtime reported no usage. |
| `gen_ai.usage.output_tokens` | recommended | Absent when the runtime reported no usage. |
| `gen_ai.usage.reasoning.output_tokens` | recommended: The runtime reported a non-zero count. | Included in gen_ai.usage.output_tokens. |
| `kagent.inference.request_id` | recommended: The runtime reported it. | The Claude request ID. Codex reports none. |
| `kagent.inference.retry.count` | conditionally required: The harness retried the call. | Set only when the call was retried. The chat span covers all attempts. Codex counts its reported retries, not the HTTP retries inside each one. Over a WebSocket, Codex does not report its first retry. Claude also counts a new stream that replaces one cut off mid-response. |
| `kagent.inference.usage_partial` | conditionally required: The call ended without final usage. | Set only when true. Input tokens, when present, stay exact. Calls cut short and Claude subagent calls can set it. |

### `kagent.invoke_agent.internal`

Refines `gen_ai.invoke_agent.internal`, kind `internal`. One execution segment of a kagent agent. Named `invoke_agent {gen_ai.agent.name}`. kagent opens it only for a runtime that emits no invoke_agent span of its own. A resumed segment links to the segment that started the native turn, with `kagent.invocation.relationship` set to `resume_origin`. Usage and finish reasons are only on inference spans, so they are opt-in here and never set.

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
| `gen_ai.response.finish_reasons` | opt in | Values correspond to generations in the same order as the returned choices/candidates. Each position SHOULD contain the finish reason for the corresponding choice/candidate. If a finish reason was expected but not received, for example because generation failed, was cancelled, or a stream ended before the final event, instrumentations SHOULD report `error` for that position instead of omitting it. `error` indicates that the generation ended abnormally, whether reported by the provider or inferred by the instrumentation. |
| `gen_ai.usage.input_tokens` | opt in | This value SHOULD include all types of input tokens, including cached tokens. Instrumentations SHOULD make a best effort to populate this value, using a total provided by the provider when available or, depending on the provider API, by summing different token types parsed from the provider output. When the provider reports both billed token counts and model-consumed token counts (for example, Cohere exposes both `usage.billed_units` and `usage.tokens`), instrumentations SHOULD report the billed count so the value matches the units the customer is charged for. Detailed usage attributes are subsets of total and aggregate counts. For example, if a request has 100 text tokens (40 cached) and 200 image tokens: - `gen_ai.usage.input_tokens`: 300 - `gen_ai.usage.cache_read.input_tokens`: 40 - `gen_ai.usage.text.input_tokens`: 100 - `gen_ai.usage.text.cache_read.input_tokens`: 40 - `gen_ai.usage.image.input_tokens`: 200 |
| `gen_ai.usage.output_tokens` | opt in | When the provider reports both billed token counts and model-consumed token counts (for example, Cohere exposes both `usage.billed_units` and `usage.tokens`), instrumentations SHOULD report the billed count so the value matches the units the customer is charged for. |
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
| `kagent.genai.producer` | description | conditionally required: The runtime is a native harness. | Set on the resource of a native harness process. With the value `runtime`, the kagent harness runtime writes the model and tool spans of this process. Its native spans repeat the same calls, so the reference collector removes their GenAI operation, model, finish reason and token usage attributes. Each call is then counted once. A standalone Claude request keeps them, since the runtime writes no span for it. Absent when the image writes its own native GenAI spans. |
| `kagent.runtime` | description | required | Every runtime declares it on its resource. The name of a Harness object is not its runtime. |


## Imported signals

The registry imports these upstream signals, so live-check compares the runtimes' own instrumentation with them.

Spans: `gen_ai.client.inference`, `gen_ai.execute_tool.internal`, `gen_ai.invoke_agent.internal`, `gen_ai.invoke_workflow.internal`, `http.client`, `http.server`, `rpc.call.client`, `rpc.call.server`.

Metrics: `gen_ai.client.inference.duration`, `gen_ai.client.inference.operation.input_tokens`, `gen_ai.client.inference.operation.output_tokens`, `gen_ai.client.inference.time_per_output_chunk`, `gen_ai.client.inference.time_to_first_chunk`, `gen_ai.client.inference.usage.cache_read.input_tokens`, `gen_ai.client.inference.usage.cache_write.input_tokens`, `gen_ai.client.inference.usage.input_tokens`, `gen_ai.client.inference.usage.output_tokens`, `gen_ai.client.inference.usage.reasoning.output_tokens`, `gen_ai.execute_tool.duration`, `gen_ai.invoke_agent.duration`, `gen_ai.invoke_agent.inference_calls`, `gen_ai.invoke_agent.tool_calls`, `gen_ai.invoke_workflow.duration`, `http.client.active_requests`, `http.client.connection.duration`, `http.client.open_connections`, `http.client.request.body.size`, `http.client.request.duration`, `http.client.response.body.size`, `http.server.active_requests`, `http.server.request.body.size`, `http.server.request.duration`, `http.server.response.body.size`, `rpc.client.call.duration`, `rpc.client.duration`, `rpc.client.request.size`, `rpc.client.requests_per_rpc`, `rpc.client.response.size`, `rpc.client.responses_per_rpc`, `rpc.server.call.duration`, `rpc.server.duration`, `rpc.server.request.size`, `rpc.server.requests_per_rpc`, `rpc.server.response.size`, `rpc.server.responses_per_rpc`.