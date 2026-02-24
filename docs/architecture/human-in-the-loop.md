# Human-in-the-Loop (HITL) Tool Approval — Design Document

## Overview

This document describes the design for adding Human-in-the-Loop (HITL) tool call approval to kagent. When configured, specific tools will pause before execution and require explicit human approval (or rejection) before proceeding.

## Goals

- Allow agent creators to mark specific tools as requiring human approval before execution
- Provide an inline approval UX in the chat interface (approve/reject buttons on tool call cards)
- Feed rejection reasons back to the LLM so it can adapt its approach
- Design for future extensibility (programmatic approval via API, webhooks, etc.)

## Non-Goals (v1)

- Agent response approval (reviewing agent output before showing to user)
- Tool argument modification before approval (edit args then approve)
- Programmatic / external approver systems (webhook, Slack bot, policy engine)
- Approval timeout / auto-approve
- Per-agent blanket approval (all tools require approval)

---

## Architecture

### Data Flow

```
┌──────────────────────────────────────────────────────────────────────┐
│                         APPROVAL LIFECYCLE                           │
│                                                                      │
│  Agent CRD (requireApproval: [tool_x])                              │
│       │                                                              │
│       ▼                                                              │
│  Go Translator → AgentConfig JSON (require_approval: [tool_x])      │
│       │                                                              │
│       ▼                                                              │
│  Python AgentConfig.to_agent()                                       │
│       │ Sets before_tool_callback on Google ADK Agent                │
│       ▼                                                              │
│  Agent execution begins                                              │
│       │                                                              │
│       ▼                                                              │
│  LLM decides to call tool_x                                         │
│       │                                                              │
│       ▼                                                              │
│  before_tool_callback fires                                          │
│       │ Checks: is tool_x in require_approval?                      │
│       │ Checks: is there a prior approval in session state?          │
│       │                                                              │
│       ├─ Not in list ──────────► return None (tool executes)         │
│       ├─ Already approved ─────► return None (tool executes)         │
│       └─ Needs approval ──────► return APPROVAL_REQUIRED marker     │
│                                                                      │
│  Executor event loop                                                 │
│       │ Detects APPROVAL_REQUIRED marker in tool response event     │
│       │ Collects ALL blocked tools from this turn (batching)         │
│       │ Breaks out of event loop, closes runner                      │
│       │                                                              │
│       ▼                                                              │
│  handle_tool_approval_interrupt() (existing _hitl.py)               │
│       │ Sends TaskState.input_required event with tool details       │
│       ▼                                                              │
│  UI renders inline Approve/Reject buttons on each tool call         │
│       │                                                              │
│       ├─ User clicks Approve ──► DataPart {decision_type: "approve"}│
│       └─ User clicks Reject ───► DataPart {decision_type: "deny",  │
│                                             reason: "..."}          │
│       │                                                              │
│       ▼                                                              │
│  Executor receives new A2A message                                   │
│       │ extract_decision_from_message() (existing _hitl.py)         │
│       │                                                              │
│       ├─ Approved: store in session state, re-run agent             │
│       │   "User approved [tool_x]. Please proceed."                 │
│       │   Callback finds approval → returns None → tool executes    │
│       │                                                              │
│       └─ Rejected: re-run agent with rejection context              │
│           "User rejected [tool_x] because: [reason].               │
│            Please try a different approach."                         │
│           LLM decides how to proceed (retry, alternative, explain)  │
│                                                                      │
└──────────────────────────────────────────────────────────────────────┘
```

### Multi-Tool Batching

When the LLM makes multiple tool calls in a single turn:

1. `before_tool_callback` fires for each tool call individually
2. Tools **not** requiring approval: callback returns `None`, tool executes normally
3. Tools **requiring** approval: callback returns `APPROVAL_REQUIRED` marker
4. After all tools are processed, the ADK yields events for each result
5. The executor collects all approval markers into a batch
6. The executor breaks out of the event loop **before** the LLM is called again with results
7. A single `input_required` event is sent with all pending tool calls

The user sees all pending approvals at once and can approve/reject each individually.

When re-running after approval, previously executed tools (non-approval ones) have their results in the session history. The LLM should not re-invoke them. As a safety net, their results can be cached in session state so the callback can return the cached result if the LLM re-calls them.

---

## Layer-by-Layer Implementation

### Layer 1: CRD — Agent Types (Go)

**File:** `go/api/v1alpha2/agent_types.go`

Add `RequireApproval` field to `McpServerTool`:

```go
type McpServerTool struct {
	TypedLocalReference `json:",inline"`

	// The names of the tools to be provided by the ToolServer.
	ToolNames []string `json:"toolNames,omitempty"`

	// RequireApproval lists tool names that require human approval before
	// execution. Each name must also appear in ToolNames. When a tool in
	// this list is invoked by the agent, execution pauses and the user is
	// prompted to approve or reject the call.
	// +optional
	RequireApproval []string `json:"requireApproval,omitempty"`

	AllowedHeaders []string `json:"allowedHeaders,omitempty"`
}
```

Add CEL validation to ensure `requireApproval` names are a subset of `toolNames`:

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
  description: "File management agent"
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
          - list_directory
        requireApproval:
          - delete_file
          - write_file
```

### Layer 2: Go ADK Config Types

**File:** `go/pkg/adk/types.go`

Add `RequireApproval` to both MCP config types:

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

### Layer 3: Go Translator

**File:** `go/internal/controller/translator/agent/adk_api_translator.go`

In `translateMCPServerTarget()` and `translateRemoteMCPServerTarget()`, copy the `RequireApproval` field from the CRD's `McpServerTool` to the `adk.HttpMcpServerConfig` or `adk.SseMcpServerConfig`:

```go
httpConfig := adk.HttpMcpServerConfig{
	Params:          params,
	Tools:           toolServer.ToolNames,
	AllowedHeaders:  toolServer.AllowedHeaders,
	RequireApproval: toolServer.RequireApproval, // NEW
}
```

### Layer 4: Python AgentConfig

**File:** `python/packages/kagent-adk/src/kagent/adk/types.py`

Add `require_approval` to the MCP config models:

```python
class HttpMcpServerConfig(BaseModel):
    params: StreamableHTTPConnectionParams
    tools: list[str] = Field(default_factory=list)
    allowed_headers: list[str] | None = None
    require_approval: list[str] | None = None  # NEW


class SseMcpServerConfig(BaseModel):
    params: SseConnectionParams
    tools: list[str] = Field(default_factory=list)
    allowed_headers: list[str] | None = None
    require_approval: list[str] | None = None  # NEW
```

In `AgentConfig.to_agent()`, collect all tools requiring approval and set the `before_tool_callback`:

```python
def to_agent(self, name: str, ...) -> Agent:
    tools: list[ToolUnion] = []
    tools_requiring_approval: set[str] = set()

    if self.http_tools:
        for http_tool in self.http_tools:
            # ... existing tool setup ...
            if http_tool.require_approval:
                tools_requiring_approval.update(http_tool.require_approval)

    if self.sse_tools:
        for sse_tool in self.sse_tools:
            # ... existing tool setup ...
            if sse_tool.require_approval:
                tools_requiring_approval.update(sse_tool.require_approval)

    # Build before_tool_callback if any tools require approval
    before_tool_callback = None
    if tools_requiring_approval:
        before_tool_callback = make_approval_callback(tools_requiring_approval)

    return Agent(
        name=name,
        model=model,
        description=self.description,
        instruction=self.instruction,
        tools=tools,
        code_executor=code_executor,
        before_tool_callback=before_tool_callback,  # NEW
    )
```

### Layer 5: Approval Callback

**File:** `python/packages/kagent-adk/src/kagent/adk/_approval.py` (new file)

```python
"""before_tool_callback implementation for HITL tool approval."""

from typing import Any
from google.adk.agents import CallbackContext

# Marker value returned by the callback to signal approval is required.
# The executor inspects tool response events for this marker.
APPROVAL_REQUIRED_STATUS = "__KAGENT_APPROVAL_REQUIRED__"


def make_approval_callback(
    tools_requiring_approval: set[str],
):
    """Create a before_tool_callback that blocks tools requiring approval.

    Args:
        tools_requiring_approval: Set of tool names that need human approval.

    Returns:
        A callback compatible with Google ADK's before_tool_callback signature.
    """

    def before_tool(
        callback_context: CallbackContext,
        tool_name: str,
        tool_args: dict[str, Any],
    ) -> dict[str, Any] | None:
        if tool_name not in tools_requiring_approval:
            return None  # No approval needed, proceed normally

        # Check session state for a prior approval for this tool call
        approval_key = _make_approval_key(tool_name, tool_args)
        if callback_context.state.get(approval_key):
            # Approval found — consume it (one-time use) and proceed
            callback_context.state.pop(approval_key, None)
            return None

        # Block tool execution, return marker for executor to detect
        return {
            "status": APPROVAL_REQUIRED_STATUS,
            "tool": tool_name,
            "args": tool_args,
        }

    return before_tool


def _make_approval_key(tool_name: str, tool_args: dict[str, Any]) -> str:
    """Create a session state key for tracking tool approval.

    Uses tool name and a stable hash of arguments to create a unique key.
    This ensures that approving delete_file(path="/tmp/a") doesn't also
    approve delete_file(path="/tmp/b").
    """
    import hashlib
    import json

    args_str = json.dumps(tool_args, sort_keys=True, default=str)
    args_hash = hashlib.sha256(args_str.encode()).hexdigest()[:12]
    return f"_hitl_approved:{tool_name}:{args_hash}"
```

### Layer 6: Executor Integration

**File:** `python/packages/kagent-adk/src/kagent/adk/_agent_executor.py`

Modify the event processing loop in `_handle_request()` to detect approval markers and handle HITL continuation:

```python
async def _handle_request(self, context, event_queue, runner, run_args):
    # Check if this is a HITL continuation (user responding to approval request)
    decision = extract_decision_from_message(context.message)
    if decision and self._is_hitl_continuation(context):
        await self._handle_hitl_response(context, event_queue, runner, run_args, decision)
        return

    # Normal execution with approval detection
    pending_approvals: list[ToolApprovalRequest] = []

    async for adk_event in runner.run_async(**run_args):
        # Check for approval markers in tool response events
        blocked_tools = self._extract_approval_markers(adk_event)
        if blocked_tools:
            pending_approvals.extend(blocked_tools)
            continue  # Don't forward this event, collect more

        # Forward non-blocked events normally
        a2a_events = convert_event_to_a2a_events(adk_event, ...)
        for event in a2a_events:
            await event_queue.enqueue_event(event)

    # After the turn completes, check if any tools need approval
    if pending_approvals:
        await handle_tool_approval_interrupt(
            action_requests=pending_approvals,
            task_id=context.task_id,
            context_id=context.context_id,
            event_queue=event_queue,
            task_store=self._task_store,
            app_name=runner.app_name,
        )
        return  # Stop here, wait for user response

    # ... normal completion logic ...
```

The `_handle_hitl_response()` method handles the user's approval/rejection:

```python
async def _handle_hitl_response(self, context, event_queue, runner, run_args, decision):
    if decision == "approve":
        # Store approvals in session state
        for tool_call in self._get_pending_tool_calls(context):
            approval_key = _make_approval_key(tool_call.name, tool_call.args)
            # Store in session state so callback finds it
            session = await runner.session_service.get_session(...)
            session.state[approval_key] = True

        # Re-run agent with approval context
        run_args["new_message"] = Content(
            parts=[Part(text="The user approved the pending tool calls. Please proceed.")]
        )
    else:  # deny/reject
        # Extract rejection reason from message
        reason = self._extract_rejection_reason(context.message)
        reason_text = f" because: {reason}" if reason else ""

        run_args["new_message"] = Content(
            parts=[Part(text=f"The user rejected the pending tool calls{reason_text}. "
                            "Please try a different approach.")]
        )

    # Re-run the agent
    async for adk_event in runner.run_async(**run_args):
        # Normal event processing (recursive approval possible)
        ...
```

### Layer 7: UI — Tool Call Approval Buttons

**File:** `ui/src/components/ToolDisplay.tsx`

Extend `ToolCallStatus` and add approval buttons:

```typescript
type ToolCallStatus = "requested" | "executing" | "completed" | "pending_approval";

// When status is "pending_approval", render:
{status === "pending_approval" && (
  <div className="flex gap-2 mt-2">
    <Button
      size="sm"
      variant="default"
      onClick={() => onApprove(toolCallId)}
    >
      Approve
    </Button>
    <Button
      size="sm"
      variant="destructive"
      onClick={() => setShowRejectInput(true)}
    >
      Reject
    </Button>
  </div>
)}

// Reject reason input (shown after clicking Reject):
{showRejectInput && (
  <div className="flex gap-2 mt-2">
    <Input
      placeholder="Reason (optional)"
      value={rejectReason}
      onChange={(e) => setRejectReason(e.target.value)}
    />
    <Button
      size="sm"
      variant="destructive"
      onClick={() => onReject(toolCallId, rejectReason)}
    >
      Confirm Reject
    </Button>
    <Button
      size="sm"
      variant="ghost"
      onClick={() => setShowRejectInput(false)}
    >
      Cancel
    </Button>
  </div>
)}
```

**File:** `ui/src/components/chat/ToolCallDisplay.tsx`

Detect `input_required` state with `interrupt_type: "tool_approval"` and set tool status to `"pending_approval"`. When multiple tools are pending, render an "Approve All" button above the individual tool cards.

**File:** `ui/src/lib/messageHandlers.ts`

Detect approval interrupt events:

```typescript
function isToolApprovalInterrupt(event: TaskStatusUpdateEvent): boolean {
  return (
    event.status.state === "input-required" &&
    event.metadata?.interrupt_type === "tool_approval"
  );
}
```

Extract tool call details from the DataPart:

```typescript
interface ToolApprovalData {
  interrupt_type: "tool_approval";
  action_requests: Array<{
    name: string;
    args: Record<string, unknown>;
    id: string | null;
  }>;
}
```

**File:** `ui/src/components/chat/ChatInterface.tsx`

When approving/rejecting, send a structured A2A message through the existing message flow:

```typescript
// Approve
const approveMessage = {
  parts: [
    { kind: "data", data: { decision_type: "approve" }, metadata: {} },
    { kind: "text", text: "Approved" }
  ]
};

// Reject with reason
const rejectMessage = {
  parts: [
    { kind: "data", data: { decision_type: "deny", reason: rejectReason }, metadata: {} },
    { kind: "text", text: `Rejected: ${rejectReason}` }
  ]
};
```

This reuses the existing `sendMessageStream()` flow — the approval is just a regular A2A message with a structured DataPart.

---

## Session State Keys

The following keys are used in session state for HITL tracking:

| Key Pattern | Value | Purpose |
|---|---|---|
| `_hitl_approved:{tool_name}:{args_hash}` | `true` | One-time approval for a specific tool call |
| `_hitl_pending_tools` | `list[dict]` | Tool calls awaiting approval (for continuation detection) |

Approval keys are consumed (deleted) after use to prevent stale approvals from applying to future calls with the same arguments.

---

## Existing Infrastructure Reused

The following existing code in `kagent-core` is reused as-is:

| Component | File | Purpose |
|---|---|---|
| `ToolApprovalRequest` | `_hitl.py` | Dataclass for tool calls needing approval |
| `handle_tool_approval_interrupt()` | `_hitl.py` | Sends `input_required` event with formatted approval message |
| `format_tool_approval_text_parts()` | `_hitl.py` | Human-readable approval message formatting |
| `extract_decision_from_message()` | `_hitl.py` | Two-tier decision extraction (DataPart then TextPart keywords) |
| `extract_decision_from_data_part()` | `_hitl.py` | Structured decision extraction |
| `extract_decision_from_text()` | `_hitl.py` | Keyword-based fallback ("approve", "deny", etc.) |
| `KAgentTaskStore.wait_for_save()` | `_task_store.py` | Race condition prevention for approval persistence |
| HITL constants | `_consts.py` | Decision types, keywords, metadata keys |

---

## Edge Cases

### LLM re-invokes already-executed tools after approval

When the agent is re-run after approval, the LLM may re-invoke tools that already executed in the previous turn (the non-approval ones). Mitigation: cache their results in session state during the first run. The `before_tool_callback` checks the cache and returns the cached result instead of re-executing.

### Multiple approval rounds in one conversation

A single conversation may trigger approval multiple times (e.g., the LLM calls another approval-requiring tool after the first one is approved). The design handles this naturally — each approval cycle follows the same flow. The `_handle_request` method's event loop re-detects approval markers on each run.

### User sends free-text instead of clicking buttons

The existing `extract_decision_from_text()` handles keyword matching ("approve", "yes", "proceed" → approve; "deny", "reject", "no", "cancel" → deny). This serves as a fallback when the user types instead of clicking buttons.

### Tool name collision across MCP servers

Two different MCP servers might expose tools with the same name. The `requireApproval` list is scoped to the MCP server reference in the CRD, so the approval configuration is unambiguous. However, the `before_tool_callback` receives only the tool name (not the server). If collisions are a concern, tool names should be namespaced at the MCP server level (upstream responsibility).

---

## Future Extensibility

### Programmatic Approval (API/Webhook)

The approval decision enters through the A2A message flow as a DataPart with `decision_type`. To support programmatic approval:

1. Add a REST endpoint: `POST /api/approvals/{task_id}` that accepts `{decision: "approve"|"deny", reason?: string}`
2. The endpoint internally constructs an A2A message with the appropriate DataPart and submits it through the existing message flow
3. Add an optional webhook configuration to the CRD for push notifications when approval is needed

No changes to the Python executor or callback would be required — the approval enters through the same channel.

### Per-Agent Default Approval

Add a `requireApprovalForAllTools: bool` field to `DeclarativeAgentSpec`. When true, all tools require approval unless explicitly exempted. This is a CRD-level change with a small modification to the approval set construction in `to_agent()`.

### Conditional Approval Rules

Support dynamic approval decisions based on tool arguments (e.g., "require approval for delete_file only when path matches /production/*"). This would extend the `requireApproval` field from `[]string` to a list of rule objects with optional conditions. The `before_tool_callback` would evaluate conditions against the actual arguments.

### Approval Timeout

Add `approvalTimeout` to the CRD. The executor would start a timer when sending `input_required`. On timeout, either auto-reject (safest) or auto-approve (configurable). Requires a background task or timer mechanism in the executor.

---

## Files Modified (Summary)

| Layer | File | Change |
|---|---|---|
| CRD | `go/api/v1alpha2/agent_types.go` | Add `RequireApproval` field to `McpServerTool` |
| Go types | `go/pkg/adk/types.go` | Add `RequireApproval` to `HttpMcpServerConfig`, `SseMcpServerConfig` |
| Go translator | `go/internal/controller/translator/agent/adk_api_translator.go` | Copy `RequireApproval` from CRD to config |
| Go codegen | Generated files | Run `make -C go generate` |
| Python config | `python/packages/kagent-adk/src/kagent/adk/types.py` | Add `require_approval` to MCP configs, wire callback in `to_agent()` |
| Python callback | `python/packages/kagent-adk/src/kagent/adk/_approval.py` | **New file**: `make_approval_callback()` and helpers |
| Python executor | `python/packages/kagent-adk/src/kagent/adk/_agent_executor.py` | Detect approval markers, handle HITL continuation |
| UI types | `ui/src/types/index.ts` | Extend status types if needed |
| UI tool display | `ui/src/components/ToolDisplay.tsx` | Add `pending_approval` status, approve/reject buttons |
| UI tool call | `ui/src/components/chat/ToolCallDisplay.tsx` | Detect approval interrupts, batch rendering |
| UI handlers | `ui/src/lib/messageHandlers.ts` | Parse approval interrupt events |
| UI chat | `ui/src/components/chat/ChatInterface.tsx` | Wire approval message submission |
| UI status | `ui/src/lib/statusUtils.ts` | Handle `input_required` for approval display |
| Tests | Multiple | Unit tests for callback, executor HITL flow, translator, UI components |
