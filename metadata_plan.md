# Plan: Remove `kagent_` Metadata Prefix and Adopt Upstream ADK Converters

## Overview

Replace kagent's custom A2A metadata prefix (`kagent_`) and converter implementations with upstream Google ADK (`google-adk`) equivalents. This is a **breaking change** — all persisted A2A task data will be invalidated and requires a major version bump.

## PR Sequence

Each PR is independently mergeable and deployable.

---

### PR 1 — Structural alignment: subclass upstream executor (no behavior change)

**Goal**: Get kagent's ADK executor structurally closer to upstream by subclassing it, while keeping all existing behavior.

**Files to modify**:
- `python/packages/kagent-adk/src/kagent/adk/_agent_executor.py`

**Changes**:
1. Change `A2aAgentExecutor` to subclass `google.adk.a2a.A2aAgentExecutor` instead of `a2a.server.agent_execution.AgentExecutor`
2. Pass kagent's existing converters via `A2aAgentExecutorConfig`:
   - `a2a_part_converter` → kagent's `convert_a2a_part_to_genai_part`
   - `gen_ai_part_converter` → kagent's `convert_genai_part_to_a2a_part`
   - `request_converter` → kagent's converter (needs adapter since signature differs)
   - `event_converter` → kagent's `convert_event_to_a2a_events`
3. Override `execute()` and/or `_handle_request()` to preserve:
   - Per-request runner creation + cleanup (`runner.close()`)
   - OpenTelemetry span attributes
   - Ollama-specific error handling
   - Partial event filtering
   - Session naming from first message
   - Request header forwarding to session state
   - Invocation ID tracking in final metadata

**Verification**: All existing tests pass. No change in wire protocol or metadata keys.

**Risk**: Medium — the upstream base class is marked `@a2a_experimental`. Need to verify that overriding `_handle_request()` provides enough control, or if `execute()` needs to be overridden entirely (which would reduce the benefit of subclassing).

**Mitigation**: If subclassing proves too constrained, fall back to composition (wrapping upstream executor) or skip this PR and proceed with PR 2+.

---

### PR 2 — Deduplicate constants (no behavior change)

**Goal**: Remove duplicate constant definitions from `kagent-core` that already exist in upstream.

**Files to modify**:
- `python/packages/kagent-core/src/kagent/core/a2a/_consts.py`
- `python/packages/kagent-core/src/kagent/core/a2a/__init__.py`

**Changes**:
1. Remove these constants from `_consts.py` (they're duplicates of upstream):
   - `A2A_DATA_PART_METADATA_TYPE_KEY`
   - `A2A_DATA_PART_METADATA_IS_LONG_RUNNING_KEY`
   - `A2A_DATA_PART_METADATA_TYPE_FUNCTION_CALL`
   - `A2A_DATA_PART_METADATA_TYPE_FUNCTION_RESPONSE`
   - `A2A_DATA_PART_METADATA_TYPE_CODE_EXECUTION_RESULT`
   - `A2A_DATA_PART_METADATA_TYPE_EXECUTABLE_CODE`
2. Re-export them from upstream: `from google.adk.a2a.converters.part_converter import ...`
3. Update `__init__.py` exports to re-export from the new source

**Verification**: All existing tests pass. grep for each constant to confirm no import breaks.

**Risk**: Low — the constant values are identical (`"type"`, `"function_call"`, etc.). The only risk is if upstream renames or moves them in a future version.

---

### PR 3 — Switch metadata prefix from `kagent_` to `adk_` (breaking change)

**Goal**: Align all metadata keys with upstream's `adk_` prefix.

**This is a major version bump. All persisted A2A task data will be invalidated.**

**Files to modify**:

*Python (prefix definition)*:
- `python/packages/kagent-core/src/kagent/core/a2a/_consts.py` — change `KAGENT_METADATA_KEY_PREFIX = "kagent_"` to `"adk_"`, or replace `get_kagent_metadata_key()` with imports of upstream's `_get_adk_metadata_key()`
- Rename `get_kagent_metadata_key` → `get_adk_metadata_key` (or alias) across all call sites
- Update `__init__.py` exports

*Python (all call sites — find/replace `get_kagent_metadata_key` → `get_adk_metadata_key`)*:
- `kagent-adk/src/kagent/adk/converters/event_converter.py`
- `kagent-adk/src/kagent/adk/converters/part_converter.py`
- `kagent-adk/src/kagent/adk/_agent_executor.py`
- `kagent-openai/src/kagent/openai/_event_converter.py`
- `kagent-openai/src/kagent/openai/_agent_executor.py`
- `kagent-langgraph/src/kagent/langgraph/_converters.py`
- `kagent-langgraph/src/kagent/langgraph/_metadata_utils.py`
- `kagent-langgraph/src/kagent/langgraph/_executor.py`
- `kagent-crewai/src/kagent/crewai/_listeners.py`
- `kagent-core/src/kagent/core/a2a/_task_store.py`
- `kagent-core/src/kagent/core/a2a/_hitl.py`

*Go*:
- `go/cli/internal/tui/chat.go` line 337 — `"kagent_type"` → `"adk_type"`

*TypeScript/UI*:
- `ui/src/lib/messageHandlers.ts` — update `ADKMetadata` interface fields from `kagent_*` to `adk_*`
- `ui/src/components/chat/ChatMessage.tsx` — update metadata key references
- `ui/src/components/chat/ToolCallDisplay.tsx` — update metadata key references
- `ui/src/lib/__tests__/messageHandlers.test.ts` — update test fixtures

*Note*: `ui/src/lib/userStore.ts` uses `kagent_user_id` as a localStorage key — this is NOT A2A metadata, it's a browser storage key. Can stay as-is or be renamed separately.

**Verification**:
- All Python, Go, and UI tests pass
- Manual test: deploy to Kind cluster, send a message, verify metadata keys in task store use `adk_` prefix
- Verify UI displays agent name, tool calls, token stats correctly

**Risk**: High — cross-cutting change across 3 languages. Must be done atomically in one PR to avoid mismatched prefixes between components.

---

### PR 4 — Replace part_converter with upstream

**Goal**: Delete kagent's custom part converter and use upstream's directly.

**Files to delete**:
- `python/packages/kagent-adk/src/kagent/adk/converters/part_converter.py`

**Files to modify**:
- `python/packages/kagent-adk/src/kagent/adk/converters/__init__.py` — remove local exports
- `python/packages/kagent-adk/src/kagent/adk/converters/event_converter.py` — update import to `from google.adk.a2a.converters.part_converter import convert_genai_part_to_a2a_part`
- `python/packages/kagent-adk/src/kagent/adk/converters/request_converter.py` — update import
- `python/packages/kagent-adk/src/kagent/adk/_agent_executor.py` — update import if needed
- Any other files importing from `kagent.adk.converters.part_converter`

**Behavior differences to accept**:
- Upstream wraps unhandled DataParts in `<a2a_datapart_json>` tags (more robust for round-trip). Kagent currently does `json.dumps(part.data)` as plain text. Upstream's behavior is better.

**Verification**: All converter tests pass. Manual test with function calls and file parts.

**Risk**: Low — the converters are nearly identical now that the prefix matches.

---

### PR 5 — Replace request_converter with upstream

**Goal**: Delete kagent's custom request converter and use upstream's `AgentRunRequest`.

**Files to delete**:
- `python/packages/kagent-adk/src/kagent/adk/converters/request_converter.py`

**Files to modify**:
- `python/packages/kagent-adk/src/kagent/adk/_agent_executor.py`:
  - Import `convert_a2a_request_to_agent_run_request` and `AgentRunRequest` from upstream
  - Update `_handle_request()` to consume `AgentRunRequest` fields instead of dict keys
  - Handle streaming mode at the executor level (apply `StreamingMode` to `run_request.run_config` after conversion, or set it in config)

**Key adaptation**: Upstream's request converter doesn't have a `stream` parameter. Streaming mode must be set by the executor on the `RunConfig` after conversion:
```python
run_request = convert_a2a_request_to_agent_run_request(context)
if self._stream:
    run_request.run_config.streaming_mode = StreamingMode.SSE
```

**Verification**: Test both streaming and non-streaming modes. Verify user_id extraction from auth context works.

**Risk**: Medium — the return type changes from `dict` to `AgentRunRequest`. All code that accesses `run_args["user_id"]` etc. must change to `run_request.user_id`.

---

### PR 6 — Replace event_converter with upstream + thin wrapper

**Goal**: Delete the bulk of kagent's event converter, keep only the kagent-specific error handling as a wrapper.

**Files to modify**:
- `python/packages/kagent-adk/src/kagent/adk/converters/event_converter.py` — gut it down to a thin wrapper

**What to keep**:
- `error_mappings.py` (kagent-specific value-add, no upstream equivalent)
- A wrapper function with this signature matching `AdkEventToA2AEventsConverter`:
  ```python
  def convert_event_to_a2a_events_with_error_handling(
      event, invocation_context, task_id, context_id, part_converter
  ) -> list[A2AEvent]:
  ```
  That:
  1. Checks if `event.error_code` is `FinishReason.STOP` — if so, treats it as normal completion (upstream doesn't do this)
  2. Delegates to upstream's `convert_event_to_a2a_events()` for everything else
  3. Post-processes error events to substitute user-friendly messages from `error_mappings.py`

**What to delete**:
- `_get_context_metadata()` — upstream handles this
- `_create_artifact_id()` — upstream handles this
- `_process_long_running_tool()` — upstream handles this
- `convert_event_to_a2a_message()` — upstream handles this
- `_create_status_update_event()` — upstream handles this
- `_serialize_metadata_value()` — upstream handles this

**Verification**: Test error scenarios (STOP, MAX_TOKENS, SAFETY). Test streaming with partial events. Test long-running tool detection.

**Risk**: Medium-High — the event converter is the most complex piece. Thorough testing needed to ensure upstream's handling matches kagent's for all edge cases. Suggest feature-flagging this (e.g., env var to fall back to old converter) during rollout.

---

### PR 7 (optional) — Rename HITL constants

**Goal**: Cosmetic cleanup of Python constant names.

**Files to modify**:
- `python/packages/kagent-core/src/kagent/core/a2a/_consts.py` — rename `KAGENT_HITL_*` to `HITL_*`
- `python/packages/kagent-core/src/kagent/core/a2a/_hitl.py` — update references
- `python/packages/kagent-core/src/kagent/core/a2a/__init__.py` — update exports
- `python/packages/kagent-langgraph/src/kagent/langgraph/_executor.py` — update imports
- All HITL test files

**Note**: The actual string values in the protocol are already unprefixed (`"decision_type"`, `"approve"`, etc.). This is purely renaming Python identifiers.

**Risk**: Low — internal rename only, no protocol change.

---

## Files That Can Be Fully Deleted (cumulative after all PRs)

- `kagent-adk/src/kagent/adk/converters/part_converter.py` (PR 4)
- `kagent-adk/src/kagent/adk/converters/request_converter.py` (PR 5)
- Most of `kagent-adk/src/kagent/adk/converters/event_converter.py` (PR 6, reduced to thin wrapper)

## Files That Must Stay

- `kagent-adk/src/kagent/adk/converters/error_mappings.py` — kagent-specific
- `kagent-adk/src/kagent/adk/_agent_executor.py` — subclass of upstream with kagent overrides
- `kagent-core/src/kagent/core/a2a/_task_store.py` — kagent-specific persistence
- `kagent-core/src/kagent/core/a2a/_hitl.py` — kagent-specific tool approval UX
- `kagent-core/src/kagent/core/a2a/_task_result_aggregator.py` — verify if upstream's is compatible
- All non-ADK converters (`kagent-openai`, `kagent-langgraph`, `kagent-crewai`) — no upstream equivalent

## Risks

1. **Upstream `@a2a_experimental` decorator** — upstream's A2A module is experimental and may change. Tight coupling increases maintenance burden on ADK version bumps.
2. **Private API usage** — upstream's `_get_adk_metadata_key()` has underscore prefix (private). May need to define own wrapper or petition upstream to make it public.
3. **Event converter edge cases** — upstream may handle edge cases differently than kagent (e.g., STOP finish reason). Thorough regression testing is critical.
4. **Non-ADK frameworks** — `kagent-openai`, `kagent-langgraph`, `kagent-crewai` still need their own converters and will continue to use `get_adk_metadata_key()` for consistent prefix.
