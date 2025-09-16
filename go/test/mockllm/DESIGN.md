## Mock LLM Server Design

This document specifies a new, test-focused mock LLM server for end-to-end tests. It defines a structured scenario engine and protocol adapters for OpenAI and Anthropic chat/completions APIs, including streaming and tool invocation flows.

### Goals
- Support OpenAI Chat Completions API and Anthropic Messages API request/response schemas.
- Configurable behavior via in-code builders or JSON scenario files (bound to Go structs; no separate JSON Schema file).
- Deterministic, multi-turn conversations with optional tool/function call steps.
- Support both non-streaming and streaming responses (SSE) for each provider.
- Simple to plug into tests and assert deterministic outputs without network calls.

### Non-Goals
- Production-grade proxy or rate limiting.
- Full schema validation beyond what is needed for tests.

### High-Level Architecture
- Router: mounts provider-specific endpoints matching real APIs to minimize client-side conditionals.
- Scenario Engine: selects a deterministic scripted response (or stream) based on a `Scenario` definition.
- Provider Adapters: normalize request envelopes into a canonical internal request model and render a provider-correct response from the selected scenario step.
- Matchers: choose scenario steps by request attributes (model, messages, tools, tool results, metadata tags, etc.).
- Response Emitters: support one-shot JSON and SSE streaming line-by-line.

### Key Types (internal)
- `Scenario` (collection): named scenario with ordered `Steps` and optional `variables`.
- `Step` (atomic): describes the expected input matcher and the produced output.
  - `Match`: constraints for routing (provider, path), model, stream flag, messages predicate, tool call presence, tool result presence, optional custom headers.
  - `Produce`: either `NonStreaming` or `Streaming` response instructions.
  - `SideEffects`: optional assertions or counters, headers to echo, custom status codes.
- `CanonicalRequest`: provider-agnostic view: `model`, `messages[]`, `tools[]`, `toolCalls[]`, `stream`, `extra`.
- `CanonicalResponse`: provider-agnostic: `content[]`, `toolCalls[]`, `usage`, `finishReason`, `streamChunks[]`.

### Provider Coverage
- OpenAI Chat Completions
  - Endpoint: `POST /v1/chat/completions`
  - Auth: `Authorization: Bearer <token>` (presence-only check for tests)
  - Streaming: SSE events with `data: {"id":..., "choices":[{"delta":...}]}` and terminal `data: [DONE]`
  - Tool calls: function/tool call objects in `choices[].message.tool_calls[]` and in stream via `delta.tool_calls[]`

- Anthropic Messages API (Claude)
  - Endpoint: `POST /v1/messages`
  - Auth: `x-api-key` (presence-only check)
  - Headers: `anthropic-version` required
  - Streaming: SSE events with `event: ...` and JSON payload lines per SDK docs
  - Tool use: `content` blocks with `type: "tool_use"` and `tool_result` handling

References: OpenAI Go SDK `openai-go` and Anthropic Go SDK `anthropic-sdk-go` for shapes and stream semantics.

### Configuration
Two ways to configure scenarios:
1) In-code builder (for brevity in tests):
   - Fluent API: `NewScenario("name").Expect(OpenAI()).WithModel("gpt-4o").WithMessages(...).Then().RespondWithText("hello").AsStream(...).Build()`
2) JSON scenario file(s):
   - Located under `go/test/mockllm/scenarios/*.json`.
   - Loaded at server start (env var `MOCKLLM_SCENARIOS` can point to a file or directory). Multiple files merge by scenario name.
   - JSON is unmarshaled directly into Go structs defined in `types.go`.

### Matching Algorithm
- Normalize incoming request into `CanonicalRequest`.
- Iterate scenarios in priority order, then steps in order:
  - Check `provider` and `path` match.
  - Check `model` equality or regex.
  - Check `stream` flag.
  - Check `messages` predicate (exact, contains, or regex on user/assistant/tool content).
  - Check tool/tool-call expectations (by name, arguments presence).
  - First match wins; mark step consumed if `consume: true` (default) to enable multi-turn progression.

### Response Generation
- For non-streaming: build a provider-correct JSON using the adapter from the `CanonicalResponse` data in the step.
- For streaming: emit SSE lines defined either as explicit `chunks[]` or auto-chunked from `text` and `toolCalls` using a provider-specific template. Always finish with provider’s terminal event (`[DONE]` for OpenAI, `event: message_stop` for Anthropic).
- Include optional `usage` when provided.

### Error Injection & Timings
- Each step can specify `latency_ms` to sleep before responding.
- Steps can specify forced errors (HTTP code + body) to test client error paths.
- Steps can omit a match to act as a default (catch-all) for a provider/path.

### Tool/Function Calls
- Steps can produce tool calls:
  - OpenAI: `tool_calls: [{ id, type: "function", function: { name, arguments } }]`
  - Anthropic: content blocks with `type: "tool_use"` including `id`, `name`, `input`.
- Steps can require the next request to include tool results, which the matcher validates:
  - OpenAI: user/assistant messages with `tool` role and `tool_call_id` or messages array `tool` role as per latest schema.
  - Anthropic: `tool_result` content blocks with `tool_use_id`.

### Files and Layout
- `server.go` — minimal HTTP server scaffold that wires routes and delegates to the engine.
- `types.go` — Go structs for scenarios, steps, matchers, and canonical request/response.
- `engine/` — scenarios, matching, canonical normalization interfaces (implementation to follow).
- `providers/openai` — adapter + streaming templates (implementation to follow).
- `providers/anthropic` — adapter + streaming templates (implementation to follow).
- `scenarios/` — JSON examples checked into repo for tests.

### Example Scenario (JSON)
An example lives in `scenarios/openai_tool_calls.example.json`. JSON files are parsed into the Go structs in `types.go`.

### Running in Tests
- Start server on random free port with in-memory scenarios or JSON loaded from testdata.
- Return base URL and teardown function to the test.
- Provide helpers to push scenario steps and to reset/inspect counters.

### SDK Schema References
- Anthropic Go SDK: `anthropic-sdk-go` repository. See message creation and streaming event shapes. Link: [anthropic-sdk-go](https://github.com/anthropics/anthropic-sdk-go)
- OpenAI Go SDK: `openai-go` repository. See chat completions and streaming deltas. Link: [openai-go](https://github.com/openai/openai-go)

### Open Questions
- Do we need Azure OpenAI compatibility in this iteration? (Out of scope unless requested.)
- Should we support image or file inputs? (Not planned initially.)
- Do we need to validate tokens beyond presence? (Currently presence-only.)


