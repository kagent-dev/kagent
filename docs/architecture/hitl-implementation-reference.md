# HITL Implementation Reference

This document provides the detailed codebase reference for the Human-in-the-Loop (HITL) tool approval feature. It covers the exact code paths, data structures, and patterns across all layers of kagent.

For the high-level design, see [human-in-the-loop.md](human-in-the-loop.md).

---

## 1. End-to-End Request/Response Lifecycle

```
UI (Next.js) → POST /a2a/{namespace}/{agent} (JSON-RPC 2.0)
  → Go HTTP Server (port 8080) → A2A DefaultRequestHandler
    → Python Agent (FastAPI, A2A protocol)
      → A2aAgentExecutor.execute()
        → runner.run_async() → Google ADK Agent loop
          → LLM call → tool execution → yield Events
        ← Events converted to A2A TaskStatusUpdateEvents
      ← SSE stream of events
    ← Go HTTP Server streams SSE to client
  ← UI processes SSE via processSSEStream()
```

## 2. Agent CRD → Python Runtime Config Flow

The Agent CRD tool configuration flows through 4 layers:

**Layer 1: CRD** (`go/api/v1alpha2/agent_types.go`)
```go
type McpServerTool struct {
    TypedLocalReference `json:",inline"`
    ToolNames       []string `json:"toolNames,omitempty"`
    RequireApproval []string `json:"requireApproval,omitempty"`
    AllowedHeaders  []string `json:"allowedHeaders,omitempty"`
}
```

**Layer 2: Go ADK types** (`go/pkg/adk/types.go`)
```go
type HttpMcpServerConfig struct {
    Params          StreamableHTTPConnectionParams `json:"params"`
    Tools           []string                       `json:"tools"`
    AllowedHeaders  []string                       `json:"allowed_headers,omitempty"`
    RequireApproval []string                       `json:"require_approval,omitempty"`
}
// Also: SseMcpServerConfig with identical structure
```
Note the JSON field name change: CRD uses `toolNames`, Go ADK uses `tools`.

**Layer 3: Go translator** (`go/internal/controller/translator/agent/adk_api_translator.go`)
- `translateRemoteMCPServerTarget()` builds `HttpMcpServerConfig` / `SseMcpServerConfig` from CRD
- Copies `RequireApproval: mcpServerTool.RequireApproval`

**Layer 4: Python** (`python/packages/kagent-adk/src/kagent/adk/types.py`)
```python
class HttpMcpServerConfig(BaseModel):
    params: StreamableHTTPConnectionParams
    tools: list[str] = Field(default_factory=list)
    allowed_headers: list[str] | None = None
    require_approval: list[str] | None = None

class AgentConfig(BaseModel):
    # ... fields ...
    def to_agent(name, sts_integration) -> Agent:
        # Collects require_approval from all tools into a set
        # Creates before_tool_callback via make_approval_callback()
```

## 3. Google ADK Agent `before_tool_callback` Signature

From the installed `google-adk` package (v1.25.0):

```python
before_tool_callback: Union[
    Callable[[BaseTool, dict[str, Any], ToolContext], Union[Awaitable[Optional[dict]], Optional[dict]]],
    list[...],
    None
]
```

Parameters:
- `tool: BaseTool` — the tool being invoked (use `tool.name` to get name)
- `args: dict[str, Any]` — the tool arguments from the LLM
- `tool_context: ToolContext` — extends `CallbackContext`, has `.state` for session state access

Return:
- `None` → tool executes normally
- `dict` → tool is skipped, the dict is used as the tool's response (goes back to LLM)

## 4. Approval Callback (`_approval.py`)

The `make_approval_callback(tools_requiring_approval: set[str])` function returns a callback matching the ADK signature:

```python
def before_tool(tool: BaseTool, args: dict[str, Any], tool_context: ToolContext) -> dict | None:
    if tool.name not in tools_requiring_approval:
        return None  # proceed
    approval_key = _make_approval_key(tool.name, args)
    if tool_context.state.get(approval_key):
        tool_context.state.pop(approval_key, None)  # consume one-time approval
        return None
    return {"status": APPROVAL_REQUIRED_STATUS, "tool": tool.name, "args": args}
```

When the callback returns a dict, the ADK creates a `FunctionResponse` part containing that dict as the response data. The event will have `event.content.parts` containing a `Part` with `.function_response.response = {"status": "...", ...}`.

## 5. Executor Event Processing and HITL Flow

**File:** `python/packages/kagent-adk/src/kagent/adk/_agent_executor.py`

### Detection in event loop

In `_handle_request()`, after each ADK event:
- Inspect `adk_event.content.parts` for `FunctionResponse` parts
- If `.response` dict contains `{"status": "__KAGENT_APPROVAL_REQUIRED__"}`, collect into `pending_approvals`
- After the loop, if pending approvals exist, call `handle_tool_approval_interrupt()` and return

### HITL continuation

At the start of `_handle_request()`:
- Check incoming message for approval decision via `extract_decision_from_message()`
- Check session state for `_hitl_pending_tools`
- **Approved:** Store approval keys in session state, set `new_message` to guide LLM
- **Rejected:** Set `new_message` with rejection reason
- Clear `_hitl_pending_tools`, continue normal execution

## 6. ADK Event Structure

ADK events from `runner.run_async()` are `google.adk.events.Event` objects with:
- `event.content` — `Content` with `.parts` list
- `event.partial` — `bool`, True for streaming chunks
- `event.invocation_id` — unique run identifier
- `event.author` — "system", "agent", "user"
- `event.actions` — `EventActions` with `.state_delta` dict

## 7. Event Converter

**File:** `python/packages/kagent-adk/src/kagent/adk/converters/event_converter.py`

`convert_event_to_a2a_events()` converts genai Parts to A2A Parts:
- `FunctionCall` → `DataPart` with metadata `kagent_type: "function_call"`
- `FunctionResponse` → `DataPart` with metadata `kagent_type: "function_response"`

## 8. Existing HITL Scaffolding

**File:** `python/packages/kagent-core/src/kagent/core/a2a/_hitl.py`

```python
@dataclass
class ToolApprovalRequest:
    name: str
    args: dict[str, Any]
    id: str | None = None

async def handle_tool_approval_interrupt(
    action_requests, task_id, context_id, event_queue, task_store, ...
) -> None:
    # Sends TaskState.input_required event with tool details
    # Waits for task save (race condition prevention)

def extract_decision_from_message(message: Message | None) -> DecisionType | None:
    # Priority 1: DataPart with decision_type field
    # Priority 2: TextPart keyword matching
```

**Constants** (`_consts.py`):
```python
KAGENT_HITL_INTERRUPT_TYPE_TOOL_APPROVAL = "tool_approval"
KAGENT_HITL_DECISION_TYPE_APPROVE = "approve"
KAGENT_HITL_DECISION_TYPE_DENY = "deny"
```

## 9. A2A Message Structure for Approval

The `handle_tool_approval_interrupt()` sends:
```python
TaskStatusUpdateEvent(
    status=TaskStatus(
        state=TaskState.input_required,
        message=Message(parts=[
            TextPart(text="**Approval Required**\n\n..."),
            DataPart(
                data={
                    "interrupt_type": "tool_approval",
                    "action_requests": [{"name": "...", "args": {...}, "id": "..."}]
                },
                metadata={"kagent_type": "interrupt_data"}
            )
        ])
    ),
    metadata={"interrupt_type": "tool_approval"}
)
```

The user's approval response:
```python
Message(parts=[
    DataPart(data={"decision_type": "approve"}),
    TextPart(text="Approved")
])
```

## 10. UI Architecture

**Message sending** (`ChatInterface.tsx`): `handleSendMessage()` → `kagentA2AClient.sendMessageStream()`

**Event handling** (`messageHandlers.ts`): `handleA2ATaskStatusUpdate()` processes `TaskStatusUpdateEvent`, maps state via `mapA2AStateToStatus()`

**Tool rendering** (`ToolDisplay.tsx`): Renders tool name, expandable args/results, status icons

**ToolCallDisplay** (`ToolCallDisplay.tsx`): Three-pass status algorithm (requested → executing → completed)

## 11. Task Result Aggregator

**File:** `python/packages/kagent-core/src/kagent/core/a2a/_task_result_aggregator.py`

State priority: `failed > auth_required > input_required > working`

The HITL flow naturally uses this: the approval interrupt event sets `input_required`, which becomes the final task state.

## 12. Session State Keys

| Key Pattern | Value | Purpose |
|---|---|---|
| `_hitl_approved:{tool_name}:{args_hash}` | `true` | One-time approval for a specific tool call |
| `_hitl_pending_tools` | `list[dict]` | Tool calls awaiting approval (for continuation detection) |
