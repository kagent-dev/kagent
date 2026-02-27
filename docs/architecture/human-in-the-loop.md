# Human-in-the-Loop: ADK-Native Tool Approval

## Overview

Kagent implements Human-in-the-Loop (HITL) tool approval using the Google
ADK's built-in `ToolContext.request_confirmation()` mechanism. When an agent
calls a tool marked with `requireApproval`, the system pauses execution and
asks the user to approve or reject the call before proceeding.

This approach provides:

- **Deterministic replay** — on approval the ADK re-invokes the exact same
  function call (same ID, same arguments) via a built-in preprocessor. No LLM
  call is needed on the resume path.
- **Minimal custom code** — the executor is nearly HITL-unaware. It only
  handles the resume path (translating the user's A2A decision into the
  `FunctionResponse` the ADK expects). The interrupt path is handled entirely
  by the ADK and the existing event converter pipeline.

---

## Data Flow

```
before_tool_callback
  │ calls tool_context.request_confirmation()
  │ returns {"status": "confirmation_requested"} to block execution
  ▼
ADK generates adk_request_confirmation event (built-in)
  │ long_running_tool_ids set on the event
  ▼
Event converter (existing)
  │ Converts to A2A DataPart: {type: "function_call", is_long_running: true}
  │ Sets TaskState.input_required
  ▼
Executor event loop
  │ Forwards event to UI, breaks on long_running_tool_ids
  ▼
UI detects input_required + adk_request_confirmation DataPart
  │ Extracts originalFunctionCall, shows Approve/Reject buttons
  ▼
User clicks Approve or Reject
  │ UI sends DataPart {decision_type: "approve"|"deny"} with taskId
  ▼
Executor resume path
  │ Translates decision to FunctionResponse(name="adk_request_confirmation",
  │   response=ToolConfirmation(confirmed=True/False))
  ▼
ADK _RequestConfirmationLlmRequestProcessor (built-in)
  │ Finds original function call in session history
  │ Re-invokes exact same tool — no LLM call
  ▼
before_tool_callback
  │ Checks tool_context.tool_confirmation.confirmed
  │ True  → return None (tool executes)
  │ False → return {"status": "rejected", "message": "..."} (tool blocked)
```

---

## Layer-by-Layer Implementation

### Layer 1: CRD — Agent Types (Go)

**File:** `go/api/v1alpha2/agent_types.go`

The `McpServerTool` struct has a `RequireApproval` field listing tool names
that must be approved before execution. Each name must also appear in
`ToolNames`.

```go
type McpServerTool struct {
    TypedLocalReference `json:",inline"`

    // ToolNames lists the tool names provided by this ToolServer.
    ToolNames []string `json:"toolNames,omitempty"`

    // RequireApproval lists tool names that require human approval before
    // execution. Each name must also appear in ToolNames.
    // +optional
    RequireApproval []string `json:"requireApproval,omitempty"`

    AllowedHeaders []string `json:"allowedHeaders,omitempty"`
}
```

A CEL validation marker ensures `requireApproval` is always a subset of
`toolNames`:

```go
// +kubebuilder:validation:XValidation:rule="!has(self.requireApproval) || self.requireApproval.all(t, self.toolNames.exists(n, n == t))",message="each requireApproval entry must also appear in toolNames"
```

**Example CRD usage:**

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: file-manager
spec:
  type: Declarative
  declarative:
    systemMessage: "You help users manage files."
    tools:
    - type: McpServer
      mcpServer:
        name: filesystem-server
        toolNames:
          - read_file
          - write_file
          - delete_file
        requireApproval:
          - delete_file
          - write_file
```

---

### Layer 2: Go ADK Config Types

**File:** `go/pkg/adk/types.go`

Both MCP config types carry `RequireApproval` so the list can be serialised
into the JSON agent config that the Python runtime reads:

```go
type HttpMcpServerConfig struct {
    Params          StreamableHTTPConnectionParams `json:"params"`
    Tools           []string                       `json:"tools"`
    AllowedHeaders  []string                       `json:"allowed_headers,omitempty"`
    RequireApproval []string                       `json:"require_approval,omitempty"`
}

type SseMcpServerConfig struct {
    Params          SseConnectionParams `json:"params"`
    Tools           []string            `json:"tools"`
    AllowedHeaders  []string            `json:"allowed_headers,omitempty"`
    RequireApproval []string            `json:"require_approval,omitempty"`
}
```

---

### Layer 3: Go Translator

**File:** `go/internal/controller/translator/agent/adk_api_translator.go`

`translateMCPServerTarget()` and `translateRemoteMCPServerTarget()` copy the
`RequireApproval` slice from the CRD's `McpServerTool` directly onto the
config struct:

```go
httpConfig := adk.HttpMcpServerConfig{
    Params:          params,
    Tools:           toolServer.ToolNames,
    AllowedHeaders:  toolServer.AllowedHeaders,
    RequireApproval: toolServer.RequireApproval,
}
```

No other translation logic is needed; the field passes through as-is.

---

### Layer 4: Python ADK Types and Agent Wiring

**File:** `python/packages/kagent-adk/src/kagent/adk/types.py`

The Pydantic models mirror the Go types:

```python
class HttpMcpServerConfig(BaseModel):
    params: StreamableHTTPConnectionParams
    tools: list[str] = Field(default_factory=list)
    allowed_headers: list[str] | None = None
    require_approval: list[str] | None = None

class SseMcpServerConfig(BaseModel):
    params: SseConnectionParams
    tools: list[str] = Field(default_factory=list)
    allowed_headers: list[str] | None = None
    require_approval: list[str] | None = None
```

`AgentConfig.to_agent()` collects every tool name that appears in any
`require_approval` list and passes the resulting set to
`make_approval_callback()`, which is then installed as `before_tool_callback`
on the ADK `Agent`:

```python
tools_requiring_approval: set[str] = set()
for http_tool in self.http_tools or []:
    if http_tool.require_approval:
        tools_requiring_approval.update(http_tool.require_approval)
for sse_tool in self.sse_tools or []:
    if sse_tool.require_approval:
        tools_requiring_approval.update(sse_tool.require_approval)

before_tool_callback = (
    make_approval_callback(tools_requiring_approval)
    if tools_requiring_approval
    else None
)

return Agent(
    ...,
    before_tool_callback=before_tool_callback,
)
```

---

### Layer 5: Approval Callback

**File:** `python/packages/kagent-adk/src/kagent/adk/_approval.py`

`make_approval_callback(tools_requiring_approval)` returns a
`before_tool_callback` with three branches:

1. **Tool not in approval set** — returns `None` immediately; the tool
   executes normally.
2. **First invocation** — calls `tool_context.request_confirmation(hint=...)`
   and returns `{"status": "confirmation_requested"}` to block execution. The
   ADK records this as a long-running function call and yields an
   `adk_request_confirmation` event.
3. **Re-invocation after user response** — checks
   `tool_context.tool_confirmation.confirmed`:
   - `True` → returns `None` (tool executes).
   - `False` → returns `{"status": "rejected", "message": "Tool call was
     rejected by user."}`.

The callback requires no session state of its own; the ADK tracks the
confirmation via session event history.

---

### Layer 6: Python Executor

**File:** `python/packages/kagent-adk/src/kagent/adk/_agent_executor.py`

The executor has two small HITL-specific pieces:

**Resume path (top of `_handle_request`):** When the incoming A2A message
contains a `decision_type` DataPart (sent by the UI after approve/reject), the
executor calls `_find_pending_confirmations(session)` to locate any
`adk_request_confirmation` FunctionCall events in session history that do not
yet have a matching FunctionResponse. For each, it constructs:

```python
FunctionResponse(
    name="adk_request_confirmation",
    id=confirmation_fc_id,
    response=ToolConfirmation(confirmed=(decision == "approve")),
)
```

This response is prepended to the new turn's content so the ADK's
`_RequestConfirmationLlmRequestProcessor` can find it and replay the original
tool call — no LLM call required on the resume path.

**Event loop break:** The event loop contains a single explicit break:

```python
if getattr(adk_event, "long_running_tool_ids", None):
    break
```

This is required because the ADK does not break automatically (see
[ADK Loop Break Caveat](#adk-loop-break-caveat) below).

---

### Layer 7: Event Converter

**File:** `python/packages/kagent-adk/src/kagent/adk/converters/event_converter.py`

No changes are needed. The existing pipeline handles HITL events natively:

1. `convert_genai_part_to_a2a_part()` converts the `adk_request_confirmation`
   FunctionCall into a DataPart with metadata `type: "function_call"`.
2. `_process_long_running_tool()` adds `is_long_running: true` to the
   DataPart's metadata.
3. `_create_status_update_event()` sees a long-running function call and sets
   `TaskState.input_required`.

The resulting DataPart structure consumed by the UI is:

```
{
  kind: "data",
  data: {
    name: "adk_request_confirmation",
    args: { originalFunctionCall: { name, args, id } },
    id: <confirm_fc_id>
  },
  metadata: { type: "function_call", is_long_running: true }
}
```

---

### Layer 8: UI — Message Handling

**File:** `ui/src/lib/messageHandlers.ts`

**Detection:** The UI identifies an approval request by looking for DataParts
where `metadata.type === "function_call"`, `metadata.is_long_running === true`,
and `data.name === "adk_request_confirmation"`. It extracts the original tool
name, arguments, and ID from `data.args.originalFunctionCall` and creates a
`ToolApprovalRequest` message in `streamingMessages`.

**Task ID tracking:** The approval message carries the A2A `taskId`. When the
user responds, the UI includes this `taskId` on the outgoing message. This
ensures the A2A framework updates the existing task (transitioning it from
`input-required` to `completed`) rather than creating a new one — which would
leave the original task stuck at `input-required` and cause the approval card
to reappear on every page reload.

**Page reload recovery:** `extractMessagesFromTasks()` scans task history for
`adk_request_confirmation` messages and reconstructs tool cards with the
correct resolved state:

- Task still `input-required` → pending approval card with Approve/Reject
  buttons.
- Task history contains a user decision DataPart → resolved card with an
  "Approved" or "Rejected" badge (no buttons).

User decision messages (DataParts containing `decision_type`) are filtered from
the regular message list so they do not appear as separate chat bubbles.

**Rejection result detection:** `isHitlRejection(toolData: ToolResponseData)`
inspects the raw `response.result` object returned by the callback for
`status === "rejected"`. This populates `ProcessedToolResultData.is_hitl_rejection`
at construction time — no downstream string parsing is needed.

---

### Layer 9: UI — Tool Display

**File:** `ui/src/components/ToolDisplay.tsx`

`ToolCallStatus` includes `"pending_approval"`, `"approved"`, and `"rejected"`
in addition to the standard `"requested"`, `"executing"`, and `"completed"`.

- `"pending_approval"` — renders Approve / Reject buttons inline on the tool
  card.
- `"approved"` — renders a green checkmark badge; buttons hidden.
- `"rejected"` — renders a red alert badge; buttons hidden.

When multiple tools are pending approval in the same turn, `ToolCallDisplay`
renders an "Approve All" button above the individual tool cards.

---

### Layer 10: UI — Chat Interface

**File:** `ui/src/components/chat/ChatInterface.tsx`

`sendApprovalDecision(decision, reason?)` sends a structured A2A message
through the existing `sendMessageStream()` flow:

```typescript
// Approve
{ parts: [{ kind: "data", data: { decision_type: "approve" }, metadata: {} }] }

// Reject with reason
{ parts: [{ kind: "data", data: { decision_type: "deny", reason }, metadata: {} }] }
```

The message is sent with `taskId` set to the pending approval's task ID so the
A2A framework routes the response to the correct task.

After sending, `chatStatus` is set back to `"ready"` (in a `finally` block) to
unblock the input regardless of whether the agent's subsequent response
succeeds or fails.

---

## ADK Loop Break Caveat

The executor's event loop contains an explicit break on `long_running_tool_ids`:

```python
if getattr(adk_event, "long_running_tool_ids", None):
    break
```

**This is required because the ADK does not break automatically.**

The ADK's `BaseLlmFlow.run_async()` loop checks `last_event.is_final_response()`
after consuming all events from one step. `is_final_response()` returns `True`
when `long_running_tool_ids` is set.

However, `_postprocess_handle_function_calls_async` yields **two** events in
order:

1. `tool_confirmation_event` — has `long_running_tool_ids` set.
2. `function_response_event` — carries the `confirmation_requested` function
   response; does **not** have `long_running_tool_ids`.

Since the loop assigns `last_event` to each event as it is yielded,
`last_event` ends up being the `function_response_event`.
`function_response_event.is_final_response()` returns `False` (events with
function responses are disqualified), so the loop continues to the next LLM
step instead of pausing.

Without the explicit break, the agent feeds the `confirmation_requested` stub
response back to the LLM and continues executing — bypassing the approval gate
entirely.

---

## Rejection Flow

1. Executor sends `ToolConfirmation(confirmed=False)` as a `FunctionResponse`.
2. `_RequestConfirmationLlmRequestProcessor` re-invokes the original tool.
3. `before_tool_callback` checks `tool_context.tool_confirmation.confirmed`
   → `False`, returns `{"status": "rejected", "message": "..."}`.
4. This dict becomes the tool's `FunctionResponse` content.
5. The LLM sees the rejection and generates a text response explaining it
   cannot proceed.

On the UI side the rejection `FunctionResponse` (which carries the original
`function_call_id`) is **not** filtered — unlike `confirmation_requested`
responses which are internal signals. `ToolCallDisplay` reads
`is_hitl_rejection` from `ProcessedToolResultData` and transitions the tool
card to `"rejected"`.
