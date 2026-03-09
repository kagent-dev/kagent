# End-to-End Data Flow

This document traces the complete path of a user message through the kagent system, from UI input to agent response.

## Overview

```
UI (Browser)
    │
    │  HTTP POST (JSON-RPC streaming)
    ▼
Controller HTTP Server (Go, :8083)
    │
    │  1. Create/get conversation + session in DB
    │  2. Proxy A2A JSON-RPC to agent pod
    ▼
Agent Pod Service (ClusterIP)
    │
    │  A2A JSON-RPC
    ▼
Agent Executor (Python/Go ADK)
    │
    │  3. Load/create session
    │  4. Build LLM request (system prompt + history + tools)
    ▼
LLM Provider (OpenAI, Anthropic, etc.)
    │
    │  5. LLM response (text + tool calls)
    ▼
Agent Executor
    │
    │  6. If tool calls: invoke via MCP → get results → loop to LLM
    │  7. Convert ADK events → A2A events
    │  8. Stream A2A events back
    ▼
Controller HTTP Server
    │
    │  9. Forward stream to UI
    ▼
UI (renders messages, tool calls, results)
```

## Step-by-Step: Sending a Message

### Step 1: UI sends message

**File:** `ui/src/components/chat/ChatInterface.tsx`

The user types a message and clicks Send. The UI creates an A2A `Message` with a `TextPart`:

```typescript
const message = {
  kind: "message",
  role: "user",
  parts: [{ kind: "text", text: userInput }],
  messageId: uuidv4(),
};
```

The UI calls `kagentA2AClient.sendMessageStream(namespace, agentName, sendParams, signal)` which sends a JSON-RPC request:

```json
{
  "jsonrpc": "2.0",
  "method": "message/send",
  "params": {
    "message": { ... },
    "metadata": { "contextId": "ctx_...", "conversationId": "conv_..." }
  },
  "id": "req_..."
}
```

**File:** `ui/src/lib/kagentA2AClient.ts`

### Step 2: Controller HTTP Server receives request

**File:** `go/core/internal/httpserver/server.go`

The controller's HTTP server receives the request at `/api/agents/{namespace}/{name}/conversations/{conversationId}/messages`.

It:
1. Looks up the agent in the database
2. Creates or retrieves the conversation
3. Determines the agent pod's Service URL: `http://{agent-name}.{namespace}:8080`
4. Proxies the A2A JSON-RPC request to the agent pod
5. Streams the response back to the UI

### Step 3: Agent Executor processes request

**File:** `python/packages/kagent-adk/src/kagent/adk/_agent_executor.py`

The agent pod's A2A server receives the JSON-RPC message. The `AgentExecutor._handle_request()` method:

1. Extracts the message content and context/task IDs
2. Creates or resumes an ADK `Session`
3. Prepares `run_args` for the ADK `Runner`:
   ```python
   run_args = {
       "user_id": user_id,
       "session_id": session.id,
       "new_message": Content(role="user", parts=[Part.from_text(text)]),
   }
   ```

### Step 4: ADK Runner manages LLM loop

**File:** Google ADK library (`google.adk.runners.Runner`)

The ADK Runner:
1. Builds the LLM request with: system prompt + conversation history + available tools
2. Sends to the configured LLM provider
3. Processes the response:
   - **Text response** → yields event with text content
   - **Tool call** → executes `before_tool_callback` (for HITL), then invokes tool via MCP, yields events
   - **Multiple tool calls** → can execute in parallel
4. If tool results need further LLM processing, loops back to step 2

### Step 5: MCP Tool Execution

When the LLM requests a tool call:

1. ADK identifies which MCP server provides the tool (from config)
2. Sends MCP `tools/call` request to the tool server
3. Tool server executes and returns result
4. Result is fed back to the LLM as a `FunctionResponse`

```
Agent Executor
    → MCP Client
        → HTTP POST to tool server (Streamable HTTP or SSE)
        → Tool server executes
        → Returns result
    → FunctionResponse added to conversation
    → LLM processes result
```

### Step 6: Event Conversion (ADK → A2A)

**File:** `python/packages/kagent-adk/src/kagent/adk/converters/event_converter.py`

Each ADK event is converted to A2A format:

| ADK Event | A2A Result |
|-----------|------------|
| Text content | `TextPart` in `TaskStatusUpdateEvent` |
| FunctionCall | `DataPart` with `metadata.type = "function_call"` |
| FunctionResponse | `DataPart` with `metadata.type = "function_response"` |
| Error | `TaskStatusUpdateEvent` with `state = failed` |
| Long-running tool | `TaskStatusUpdateEvent` with `state = input_required` |

The converter also determines the task state:
- Normal events → `TaskState.working`
- Final text → `TaskState.completed`
- HITL pause → `TaskState.input_required`
- Auth needed → `TaskState.auth_required`

### Step 7: Streaming back to UI

A2A events are streamed as Server-Sent Events (SSE) through the JSON-RPC response:

```
Controller HTTP Server (proxy)
    ← SSE stream from agent pod
    → SSE stream to UI browser
```

The UI processes each event in `handleA2ATaskStatusUpdate()`:

**File:** `ui/src/lib/messageHandlers.ts`

- Text parts → rendered as chat messages
- Function call parts → rendered as tool call cards (via `ToolCallDisplay`)
- Function response parts → update tool call cards with results
- State changes → update UI status indicators

---

## Agent Configuration Flow

How `config.json` gets from CRD to agent pod:

```
Agent CRD (in etcd)
    │
    │ AgentController receives event
    ▼
adkApiTranslator.translateInlineAgent()
    │
    ├── Resolves ModelConfig CRD → provider, model, API key
    ├── Resolves RemoteMCPServer CRDs → URLs, protocols, tools
    ├── Resolves prompt template → final system message
    ├── Resolves skills → init containers
    │
    ▼
Builds AgentConfig struct (Go ADK types)
    │
    │ JSON marshal
    ▼
Kubernetes Secret (data: {"config.json": "..."})
    │
    │ Volume mount
    ▼
Agent Pod reads /config/config.json at startup
    │
    │ Pydantic parse (Python) or JSON unmarshal (Go)
    ▼
AgentConfig → creates ADK Agent with tools, callbacks, session service
```

**config.json structure** (Go types in `go/adk/types.go`, Python types in `python/packages/kagent-adk/src/kagent/adk/types.py`):

```json
{
  "agent": {
    "name": "my-agent",
    "description": "...",
    "instruction": "resolved system message...",
    "sub_agents": [],
    "http_tools": [
      {
        "params": {
          "url": "http://tool-server:8084/mcp",
          "timeout": 30
        },
        "tools": ["tool1", "tool2"],
        "require_approval": ["tool2"],
        "allowed_headers": []
      }
    ],
    "sse_tools": []
  },
  "model": {
    "model": "gpt-4o",
    "provider": "OpenAI",
    "api_key": "sk-...",
    "openai": { "temperature": "0.7" }
  },
  "stream": true,
  "memory": null,
  "context": null
}
```

---

## Database Interaction Flow

The database supplements Kubernetes storage for data that needs fast querying:

```
                    Controller
                        │
            ┌───────────┼───────────┐
            ▼           ▼           ▼
    Reconciliation   HTTP API    A2A Proxy
            │           │           │
            │           │           │
     ┌──────┴──────┐    │           │
     │  Upsert     │    │           │
     │  agents,    │    │           │
     │  tools,     │    │           │
     │  servers    │    │           │
     └──────┬──────┘    │           │
            │           │           │
            ▼           ▼           ▼
        ┌───────────────────────────────┐
        │          Database             │
        │   (SQLite or PostgreSQL)      │
        │                               │
        │  agents ← upsert on reconcile│
        │  tool_servers ← upsert       │
        │  tools ← refresh on reconcile│
        │  conversations ← HTTP API    │
        │  sessions ← A2A proxy        │
        └───────────────────────────────┘
```

**Write paths:**
- Agent reconciliation → upserts agent record
- RemoteMCPServer reconciliation → upserts tool server + refreshes tools (in transaction)
- HTTP API → creates conversations
- A2A proxy → creates/updates sessions

**Read paths:**
- HTTP API → lists agents, tools, conversations, sessions for the UI
- A2A proxy → looks up agent to find pod Service URL

---

## Cross-Namespace References

Agents can reference tools and other agents across namespaces, controlled by `AllowedNamespaces`:

```yaml
# In namespace "tools"
apiVersion: kagent.dev/v1alpha2
kind: RemoteMCPServer
metadata:
  name: shared-tools
  namespace: tools
spec:
  allowedNamespaces:
    from: Selector
    selector:
      matchLabels:
        kagent-access: "true"
  ...

# In namespace "agents" (with label kagent-access=true)
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: my-agent
  namespace: agents
spec:
  declarative:
    tools:
      - type: McpServer
        mcpServer:
          name: shared-tools
          namespace: tools
          kind: RemoteMCPServer
          apiGroup: kagent.dev
```

---

## Related Documents

- [controller-reconciliation.md](controller-reconciliation.md) — Concurrency model and event filtering
- [human-in-the-loop.md](human-in-the-loop.md) — HITL approval data flow
- [prompt-templates.md](prompt-templates.md) — Template resolution during reconciliation
