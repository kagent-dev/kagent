# Kagent NATS Integration (Current State)

## NATS is Already Deeply Integrated

### Dependencies
- `github.com/nats-io/nats.go v1.49.0` in `go/adk/go.mod`
- Embedded NATS server for testing (`nats-server/v2 v2.12.4`)
- Helm deploys `nats:2-alpine` on port 4222 when `temporal.enabled=true`

### Streaming Layer (`go/adk/pkg/streaming/`)

```
nats.go   — StreamPublisher, StreamSubscriber, connection factory
types.go  — StreamEvent, EventType enums, structured event types
```

**Event Types already published:**

| EventType | Description |
|-----------|-------------|
| `EventTypeToken` | LLM tokens (streaming output) |
| `EventTypeToolStart` | Tool execution begins |
| `EventTypeToolEnd` | Tool execution completes |
| `EventTypeApprovalRequest` | HITL approval needed |
| `EventTypeCompletion` | Workflow/message done |
| `EventTypeError` | Error occurred |

**Subject pattern:** `agent.{agentName}.{sessionID}.stream`

**Structured payloads:**
- `ToolCallEvent` — tool ID, name, arguments (JSON)
- `ToolResultEvent` — tool ID, name, response (JSON), error flag
- `ApprovalRequest` — WorkflowID, RunID, SessionID, Message, ToolName

### Publishing Locations
1. `go/adk/pkg/temporal/activities.go` — PublishToken, PublishToolProgress, PublishApprovalRequest, PublishCompletion
2. `go/adk/pkg/a2a/temporal_executor.go` — subscribes for A2A coordination

### UI Real-Time Patterns (Existing)
- **Temporal MCP plugin**: Polling-based SSE hub (`go/plugins/temporal-mcp/internal/sse/hub.go`)
- **Kanban MCP plugin**: Push-based SSE hub (`go/plugins/kanban-mcp/internal/sse/hub.go`)
- **Chat UI**: No direct NATS subscription; uses HTTP fetch for messages

### The Missing Piece: NATS → UI Bridge

```
Agents → NATS (agent.>) → [??? BRIDGE ???] → SSE → Browser UI
```

No component currently subscribes to NATS and forwards events to the UI. This is exactly what the activity feed needs to provide.

### Architecture Opportunity

The SSE hub pattern is already proven in both temporal-mcp and kanban-mcp plugins. A new component (or endpoint) would:
1. Subscribe to `agent.>` on NATS (wildcard for all agents)
2. Decode StreamEvent messages
3. Forward to connected browsers via SSE

This is a lightweight bridge — all the hard work (event types, structured payloads, NATS publishing) is already done.
