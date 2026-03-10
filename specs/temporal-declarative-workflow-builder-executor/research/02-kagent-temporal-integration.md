# Kagent Temporal Integration Research

> Generated: 2026-03-10 | Branch: feature/kanban-mcp | Research only -- no implementation.

---

## 1. Workflow Definitions

### Single Workflow: `AgentExecutionWorkflow`

**File:** `go/adk/pkg/temporal/workflows.go`

There is exactly **one workflow definition** in the codebase: `AgentExecutionWorkflow`. It is a **long-running session workflow** that maps 1:1 to an A2A session.

**Structure:**

```
AgentExecutionWorkflow(ctx, *ExecutionRequest) (*ExecutionResult, error)
  1. SessionActivity        -- create/retrieve session
  2. Drain initial message   -- from SignalWithStart or req.Message (backward compat)
  3. processMessage()        -- LLM+tool loop for the first message
  4. Main loop:
     a. Selector: wait for MessageSignal OR idle timer (1h)
     b. On message: processMessage()
     c. On idle timeout: return completed
```

**`processMessage` inner loop (up to 100 turns):**
```
for turn in 0..MaxTurns:
  1. Serialize history -> LLMInvokeActivity
  2. If terminal (no tool calls, no agent calls, no HITL):
     - SaveTaskActivity
     - PublishCompletionActivity (NATS)
     - return nil (back to main loop -- workflow stays alive)
  3. If tool calls: executeToolsInParallel -> ToolExecuteActivity (parallel futures)
  4. If agent calls: executeChildWorkflows -> child AgentExecutionWorkflow (parallel)
  5. If HITL: PublishApprovalActivity -> block on "approval" signal channel
     - Approved: append "[APPROVED]" to history, continue loop
     - Rejected: PublishCompletionActivity("rejected"), return nil
```

**Key design decisions:**
- Workflow stays alive across multiple messages (session = single workflow execution).
- Uses `workflow.NewSelector` with timer + signal channel for idle timeout.
- Returns `nil, nil` after each message to re-enter the main wait loop (not an error, not a result).
- History is held in-workflow as `[]conversationEntry` (not persisted to external store between turns).

**Child workflows:**
- Launched via `workflow.ExecuteChildWorkflow` with `ParentClosePolicy: TERMINATE`.
- Task queue = target agent's name (cross-agent routing).
- Workflow ID = `"{targetAgent}:child:{parentSessionID}"`.

### Constants

| Constant | Value |
|---|---|
| MaxTurns | 100 |
| SessionIdleTimeout | 1 hour |
| DefaultLLMActivityTimeout | 5 min |
| DefaultToolActivityTimeout | 10 min |
| DefaultSessionActivityTimeout | 30 sec |
| DefaultTaskActivityTimeout | 30 sec |

---

## 2. Activity Definitions

**File:** `go/adk/pkg/temporal/activities.go`

All activities are methods on the `Activities` struct, which holds injected dependencies:

```go
type Activities struct {
    sessionSvc   session.SessionService
    taskStore    *taskstore.KAgentTaskStore
    natsConn     *nats.Conn
    publisher    *streaming.StreamPublisher
    modelInvoker ModelInvoker   // func(ctx, config, history, onToken) -> *LLMResponse
    toolExecutor ToolExecutor   // func(ctx, toolName, args) -> (result, error)
}
```

### Registered Activities

| Activity | Input | Output | Purpose |
|---|---|---|---|
| `SessionActivity` | `*SessionRequest` | `*SessionResponse` | Create/retrieve session via SessionService |
| `LLMInvokeActivity` | `*LLMRequest` | `*LLMResponse` | Invoke LLM model; streams tokens to NATS via `onToken` callback |
| `ToolExecuteActivity` | `*ToolRequest` | `*ToolResponse` | Execute MCP tool; publishes tool_start/tool_end events to NATS |
| `SaveTaskActivity` | `*TaskSaveRequest` | `error` | Persist A2A Task via KAgent task store |
| `PublishApprovalActivity` | `*PublishApprovalRequest` | `error` | Publish HITL approval request to NATS |
| `PublishCompletionActivity` | `*PublishCompletionRequest` | `error` | Publish message completion event to NATS |
| `AppendEventActivity` | `*AppendEventRequest` | `error` | Append event to session (defined but not called in current workflow) |

### Activity Option Configuration

Each activity type has its own `workflow.ActivityOptions` with tailored timeouts and retry policies:

- **Session:** 30s timeout, 3 attempts, 1-10s backoff
- **LLM:** 5 min timeout, configurable max attempts (default 5), 2s-2min backoff
- **Tool:** 10 min timeout, 1 min heartbeat, configurable max attempts (default 3), 1s-1min backoff
- **Task:** 30s timeout, 3 attempts, 1-10s backoff

### Functional Interfaces

Two key interface types decouple the activities from concrete implementations:

```go
type ModelInvoker func(ctx context.Context, config []byte, history []byte, onToken func(string)) (*LLMResponse, error)
type ToolExecutor func(ctx context.Context, toolName string, args []byte) ([]byte, error)
```

**ModelInvoker implementation:** `go/adk/pkg/agent/modelinvoker.go` -- Creates LLM from serialized AgentConfig, converts conversation history to genai format, invokes model with tool declarations, streams partial tokens via callback.

**ToolExecutor implementation:** Provided by `mcp.CreateToolExecutor()` which discovers tools from configured MCP servers at startup.

---

## 3. Signal Patterns

**File:** `go/adk/pkg/temporal/types.go` (signal name constants), `workflows.go` (usage)

### Signal Channels

| Signal Name | Constant | Payload Type | Direction |
|---|---|---|---|
| `"message"` | `MessageSignalName` | `MessageSignal` | Executor -> Workflow |
| `"approval"` | `ApprovalSignalName` | `ApprovalDecision` | External/UI -> Workflow |

### MessageSignal

```go
type MessageSignal struct {
    Message     []byte `json:"message"`     // serialized A2A message
    NATSSubject string `json:"natsSubject"` // NATS subject for streaming events back
}
```

**Usage in workflow:**
1. `msgCh := workflow.GetSignalChannel(ctx, MessageSignalName)` -- set up once
2. First message: `msgCh.ReceiveAsync(&firstMsg)` -- non-blocking drain from SignalWithStart
3. Main loop: `sel.AddReceive(msgCh, ...)` -- blocking receive via Selector

### ApprovalDecision

```go
type ApprovalDecision struct {
    Approved bool   `json:"approved"`
    Reason   string `json:"reason,omitempty"`
}
```

**Usage in workflow:** `approvalCh.Receive(ctx, &decision)` -- blocks workflow deterministically until signal arrives.

### SignalWithStartWorkflow (Primary Interaction Pattern)

**File:** `go/adk/pkg/temporal/client.go` -- `ExecuteAgent()`

This is the **central pattern** for all agent invocations:

```go
run, err := c.temporal.SignalWithStartWorkflow(ctx, workflowID, MessageSignalName, msg, opts, AgentExecutionWorkflow, req)
```

**Semantics:**
- If workflow is NOT running: starts a new workflow AND delivers the message signal atomically.
- If workflow IS running: delivers the message signal to the existing workflow (workflow stays alive).
- Guarantees exactly one workflow per session -- no race conditions between start and signal.

---

## 4. Worker Setup

**File:** `go/adk/pkg/temporal/worker.go`

```go
func NewWorker(temporalClient client.Client, taskQueue string, activities *Activities) (worker.Worker, error) {
    w := worker.New(temporalClient, taskQueue, worker.Options{})
    w.RegisterWorkflow(AgentExecutionWorkflow)
    w.RegisterActivity(activities)
    return w, nil
}
```

**Key points:**
- One workflow registered: `AgentExecutionWorkflow`
- All activities registered via the `Activities` struct (methods are auto-discovered)
- No custom `worker.Options` (defaults: 2 concurrent workflow tasks, 1000 concurrent activity tasks)
- Task queue is per-agent: derived from agent name via `TaskQueueForAgent(agentName)` which simply returns the name

**Lifecycle (in `go/adk/pkg/app/app.go`):**

```
app.Run() ->
  temporalWorker.Start()   // starts polling
  a2aServer.Run()          // blocks on HTTP

app.stop() ->
  temporalWorker.Stop()    // graceful stop
  natsConn.Close()
  temporalClient.Close()
  tokenService.Stop()
```

The `KAgentApp.SetTemporalInfra()` method stores temporal client, worker, and NATS connection for lifecycle management.

---

## 5. Client Usage

**File:** `go/adk/pkg/temporal/client.go`

### Client Struct

```go
type Client struct {
    temporal client.Client
}
```

### Methods

| Method | SDK Call | Purpose |
|---|---|---|
| `NewClient(cfg)` | `client.Dial(...)` | Connect to Temporal server |
| `ExecuteAgent(ctx, req, cfg)` | `SignalWithStartWorkflow(...)` | Start or signal session workflow |
| `SignalApproval(ctx, wfID, decision)` | `SignalWorkflow(...)` | Send HITL approval/rejection |
| `GetWorkflowStatus(ctx, wfID)` | `DescribeWorkflowExecution(...)` | Query workflow state |
| `WaitForResult(ctx, wfID)` | `GetWorkflow(...).Get(...)` | Block until workflow completes |
| `TerminateRunningWorkflows(ctx, taskQueue)` | `ListWorkflow(...)` + `TerminateWorkflow(...)` | Cleanup orphans on pod restart |
| `Temporal()` | -- | Access underlying SDK client |
| `Close()` | `client.Close()` | Shutdown |

### Orphan Cleanup on Startup

**File:** `go/adk/cmd/main.go` (lines 217-221)

On pod startup, before processing any requests:
```go
if n, err := temporalClient.TerminateRunningWorkflows(ctx, taskQueue); err != nil {
    logger.Error(err, "Failed to terminate orphaned workflows")
} else if n > 0 {
    logger.Info("Terminated orphaned workflows from previous pod lifecycle", "count", n)
}
```

This terminates workflows left running from a previous pod that crashed -- they have no executor waiting for completion events.

---

## 6. NATS Integration

### Streaming Architecture

**Files:** `go/adk/pkg/streaming/types.go`, `go/adk/pkg/streaming/nats.go`

NATS serves as the **real-time event bridge** between Temporal activities (running in the worker) and the A2A executor (running in the HTTP server process).

```
                NATS
Activity ----publish----> Subject ----subscribe----> TemporalExecutor -> A2A SSE
```

### NATS Subject Pattern

```
agent.{agentName}.{sessionID}.stream
```

Example: `agent.istio-agent.abc123.stream`

### Event Types

| EventType | Published By | Purpose |
|---|---|---|
| `token` | `LLMInvokeActivity` | Streaming LLM tokens |
| `tool_start` | `ToolExecuteActivity` | Tool call began (name, args) |
| `tool_end` | `ToolExecuteActivity` | Tool call finished (result/error) |
| `approval_request` | `PublishApprovalActivity` | HITL approval needed |
| `completion` | `PublishCompletionActivity` | Message processing done (with result) |
| `error` | `LLMInvokeActivity` | Error during processing |

### StreamEvent Envelope

```go
type StreamEvent struct {
    Type      EventType `json:"type"`
    Data      string    `json:"data"`      // JSON-encoded payload
    Timestamp int64     `json:"timestamp"` // UnixMilli
}
```

### TemporalExecutor NATS Subscription

**File:** `go/adk/pkg/a2a/temporal_executor.go` -- `Execute()`

The executor subscribes to the NATS subject **before** starting the workflow to avoid race conditions:

```go
sub, _ := e.natsConn.Subscribe(req.NATSSubject, func(msg *nats.Msg) {
    // Parse event
    if event.Type == EventTypeCompletion {
        completionCh <- &result   // signal completion
        return
    }
    e.forwardStreamEvent(...)     // forward to A2A SSE queue
})
defer sub.Unsubscribe()

// Then start/signal workflow
run, _ := e.client.ExecuteAgent(ctx, req, e.config)

// Wait for completion via NATS (not workflow.Get)
select {
case result := <-completionCh:
    return e.writeFinalStatus(ctx, reqCtx, queue, result)
case <-ctx.Done():
    // timeout/cancel
}
```

**Critical design point:** The executor does NOT wait for the workflow to finish (`WaitForResult`). It waits for a **completion event via NATS**. This is because the session workflow stays alive across messages -- only individual message processing "completes", not the workflow itself.

### Event Forwarding to A2A SSE

The `forwardStreamEvent` method maps NATS events to A2A `TaskStatusUpdateEvent`:
- `token` -> `TaskStateWorking` with `TextPart` (marked `adk_partial`)
- `tool_start` -> `TaskStateWorking` with `DataPart` (metadata: `adk_type: function_call`)
- `tool_end` -> `TaskStateWorking` with `DataPart` (metadata: `adk_type: function_response`)
- `approval_request` -> `TaskStateInputRequired` with `TextPart`

---

## 7. Session Management

### Workflow ID Derivation

**File:** `go/adk/pkg/temporal/types.go`

```go
func WorkflowIDForSession(agentName, sessionID string) string {
    return agentName + ":" + sessionID
}

func ChildWorkflowID(parentSessionID, targetAgentName string) string {
    return targetAgentName + ":child:" + parentSessionID
}

func TaskQueueForAgent(agentName string) string {
    return agentName   // uses K8s agent name directly
}
```

**Naming examples:**
- Session workflow: `istio-agent:abc-123-def`
- Child workflow: `k8s-agent:child:abc-123-def`
- Task queue: `istio-agent`

The colon separator was chosen because it is URL-safe (slash would break Temporal UI deep links).

### Session-to-Workflow Mapping

- **One session = one workflow** (enforced by deterministic workflow ID from agent + session)
- **Multiple messages = one workflow** (SignalWithStart delivers signals to running workflow)
- **Conversation history** is maintained in-workflow as `[]conversationEntry` (not externalized between turns)
- **Session creation** happens as the first activity in the workflow (`SessionActivity`)

### CRD-to-Runtime Config Flow

```
Agent CRD spec.temporal (TemporalSpec)
  -> Controller translator (adk_api_translator.go)
    -> config.json with TemporalRuntimeConfig
      -> Pod env vars: TEMPORAL_HOST_ADDR, NATS_ADDR
        -> FromRuntimeConfig() -> TemporalConfig (runtime)
```

**CRD fields (`TemporalSpec`):**
```go
type TemporalSpec struct {
    Enabled         bool              `json:"enabled,omitempty"`
    WorkflowTimeout *metav1.Duration  `json:"workflowTimeout,omitempty"`
    RetryPolicy     *TemporalRetryPolicy `json:"retryPolicy,omitempty"`
}
type TemporalRetryPolicy struct {
    LLMMaxAttempts  *int32 `json:"llmMaxAttempts,omitempty"`
    ToolMaxAttempts *int32 `json:"toolMaxAttempts,omitempty"`
}
```

**Runtime config (`TemporalRuntimeConfig` in config.json):**
```go
type TemporalRuntimeConfig struct {
    Enabled         bool   `json:"enabled"`
    HostAddr        string `json:"host_addr,omitempty"`
    Namespace       string `json:"namespace,omitempty"`
    TaskQueue       string `json:"task_queue,omitempty"`
    NATSAddr        string `json:"nats_addr,omitempty"`
    WorkflowTimeout string `json:"workflow_timeout,omitempty"`
    LLMMaxAttempts  int    `json:"llm_max_attempts,omitempty"`
    ToolMaxAttempts int    `json:"tool_max_attempts,omitempty"`
}
```

The translator sets `Namespace` = agent's K8s namespace, `TaskQueue` = agent name.

---

## 8. Reusable Components for a Declarative Workflow Builder

### Directly Reusable

| Component | File | Why |
|---|---|---|
| `temporal.Client` wrapper | `go/adk/pkg/temporal/client.go` | SignalWithStartWorkflow, TerminateRunningWorkflows, approval signaling |
| `streaming.StreamPublisher` | `go/adk/pkg/streaming/nats.go` | NATS event publishing (token, tool, completion, approval) |
| `streaming.StreamEvent` types | `go/adk/pkg/streaming/types.go` | Event envelope, all event type constants |
| `Activities` struct pattern | `go/adk/pkg/temporal/activities.go` | Dependency injection for activities, NATS publishing pattern |
| `ModelInvoker` interface | `go/adk/pkg/temporal/activities.go` | `func(ctx, config, history, onToken) -> *LLMResponse` |
| `ToolExecutor` interface | `go/adk/pkg/temporal/activities.go` | `func(ctx, toolName, args) -> (result, error)` |
| `TemporalExecutor` (A2A bridge) | `go/adk/pkg/a2a/temporal_executor.go` | NATS subscription + A2A event forwarding pattern |
| Config conversion | `go/adk/pkg/temporal/types.go` | `FromRuntimeConfig()`, `DefaultTemporalConfig()`, ID generation |
| Worker factory | `go/adk/pkg/temporal/worker.go` | `NewWorker()` pattern for registration |
| App lifecycle | `go/adk/pkg/app/app.go` | `SetTemporalInfra()`, ordered shutdown |
| HITL types | `go/adk/pkg/a2a/hitl.go` | `ApprovalDecision`, `ToolApprovalRequest`, decision extraction |
| MCP tool executor | `go/adk/pkg/mcp/registry.go` | `CreateToolExecutor()` returns `ToolExecutor` + declarations |

### Extendable Patterns

| Pattern | Current Use | Extension Opportunity |
|---|---|---|
| **Signal-with-start** | One workflow per session | Reuse for any long-running stateful workflow |
| **Selector (signal + timer)** | Idle timeout in session loop | Generalize to any wait-for-event-or-timeout |
| **Parallel activity execution** | `executeToolsInParallel` with futures | Reuse for any fan-out/fan-in step |
| **Child workflows** | A2A agent-to-agent calls | Reuse for sub-workflow orchestration in declarative pipelines |
| **NATS event bridge** | Activity -> NATS -> Executor -> SSE | Generalize for any workflow step -> UI streaming |
| **Conversation history as workflow state** | `[]conversationEntry` in-memory | Could externalize for pause/resume or workflow replay |
| **Activity options per type** | Separate timeout/retry configs | Expose as declarative step-level configuration |

### What Would Need to Be New

For a declarative workflow builder/executor:

1. **Workflow DSL/Schema** -- Define steps, branching, conditions, parallelism declaratively (YAML/JSON/CRD).
2. **Generic Step Executor** -- A workflow function that interprets the DSL and dispatches steps as activities.
3. **Step Registry** -- Map step types (llm, tool, condition, parallel, human-approval) to activity implementations.
4. **Workflow Builder** -- Convert declarative definition to Temporal workflow registration or to a generic interpreter workflow.
5. **State Management** -- The current workflow keeps history in-memory; a declarative builder may need explicit state passing between steps.

---

## 9. Temporal-MCP Plugin (Observability Sidecar)

**Directory:** `go/plugins/temporal-mcp/`

A separate MCP server plugin that provides Temporal workflow observability tools:

### MCP Tools Exposed

| Tool | Purpose |
|---|---|
| `list_workflows` | List workflow executions (filter by status, agent name) |
| `get_workflow` | Get workflow detail including activity history |
| `cancel_workflow` | Cancel a running workflow |
| `signal_workflow` | Send arbitrary signal to a workflow |

### Client Interface

```go
type WorkflowClient interface {
    ListWorkflows(ctx context.Context, filter WorkflowFilter) ([]*WorkflowSummary, error)
    GetWorkflow(ctx context.Context, workflowID string) (*WorkflowDetail, error)
    CancelWorkflow(ctx context.Context, workflowID string) error
    SignalWorkflow(ctx context.Context, workflowID, signalName string, data interface{}) error
}
```

The plugin also includes an SSE hub (`internal/sse/hub.go`) and REST API handlers (`internal/api/handlers.go`) for a web UI embedded in the plugin.

---

## 10. E2E Test Coverage

**File:** `go/core/test/e2e/temporal_test.go`

| Test | What It Validates |
|---|---|
| `TestE2ETemporalInfrastructure` | Temporal server + NATS deployed and healthy |
| `TestE2ETemporalAgentCRDTranslation` | Agent pod gets TEMPORAL_HOST_ADDR and NATS_ADDR env vars |
| `TestE2ETemporalWorkflowExecution` | Full sync + streaming workflow execution via A2A |
| `TestE2ETemporalUIPlugin` | Temporal UI accessible via kagent plugin proxy |
| `TestE2ETemporalFallbackPath` | Agent without temporal.enabled works via sync path |
| `TestE2ETemporalCrashRecovery` | Workflow resumes after pod delete (gated: TEMPORAL_CRASH_RECOVERY_TEST) |
| `TestE2ETemporalWithCustomTimeout` | Custom WorkflowTimeout + RetryPolicy from CRD |
| `TestE2ETemporalToolExecution` | Multi-turn workflow with MCP tool calls |
| `TestE2ETemporalChildWorkflow` | Parent agent invokes child agent via child workflow |
| `TestE2ETemporalHITLApproval` | HITL signal flow (gated: TEMPORAL_HITL_TEST) |
| `TestE2ETemporalWorkflowVisibleInTemporalUI` | Workflow visible via tctl (gated: TEMPORAL_UI_TEST) |

---

## 11. Architecture Summary Diagram

```
                          Kubernetes Cluster
 +-----------------------------------------------------------------+
 |                                                                 |
 |   Agent CRD (spec.temporal.enabled: true)                      |
 |       |                                                        |
 |       v                                                        |
 |   Controller (translator)                                      |
 |       |-- config.json (TemporalRuntimeConfig)                  |
 |       |-- env: TEMPORAL_HOST_ADDR, NATS_ADDR                   |
 |       v                                                        |
 |   Agent Pod                                                    |
 |   +-----------------------------------------------------------+|
 |   |  main.go                                                  ||
 |   |    |                                                      ||
 |   |    +-- Temporal Client (client.Dial)                      ||
 |   |    |     |-- TerminateRunningWorkflows (startup cleanup)  ||
 |   |    |     |-- ExecuteAgent (SignalWithStart)               ||
 |   |    |     |-- SignalApproval                               ||
 |   |    |                                                      ||
 |   |    +-- Temporal Worker (polls task queue)                 ||
 |   |    |     |-- AgentExecutionWorkflow                       ||
 |   |    |     |-- Activities (Session, LLM, Tool, Task, NATS)  ||
 |   |    |                                                      ||
 |   |    +-- A2A Server (HTTP/SSE)                              ||
 |   |    |     |-- TemporalExecutor                             ||
 |   |    |           |-- NATS Subscribe (streaming events)      ||
 |   |    |           |-- SignalWithStart (trigger workflow)      ||
 |   |    |           |-- Wait for completion via NATS           ||
 |   |    |                                                      ||
 |   |    +-- NATS Connection                                    ||
 |   +-----------------------------------------------------------+|
 |                                                                 |
 |   Temporal Server (temporal-server:7233)                        |
 |   NATS Server (nats:4222)                                      |
 +-----------------------------------------------------------------+
```

---

## 12. Key Files Index

| Purpose | Path |
|---|---|
| Workflow definition | `go/adk/pkg/temporal/workflows.go` |
| Activity implementations | `go/adk/pkg/temporal/activities.go` |
| Type definitions (signals, configs, IDs) | `go/adk/pkg/temporal/types.go` |
| Temporal client wrapper | `go/adk/pkg/temporal/client.go` |
| Worker factory | `go/adk/pkg/temporal/worker.go` |
| A2A-to-Temporal executor bridge | `go/adk/pkg/a2a/temporal_executor.go` |
| NATS streaming types | `go/adk/pkg/streaming/types.go` |
| NATS streaming publisher/subscriber | `go/adk/pkg/streaming/nats.go` |
| Model invoker (LLM activity impl) | `go/adk/pkg/agent/modelinvoker.go` |
| App lifecycle management | `go/adk/pkg/app/app.go` |
| ADK entry point (wiring) | `go/adk/cmd/main.go` |
| HITL types and decision extraction | `go/adk/pkg/a2a/hitl.go` |
| A2A executor (sync path) | `go/adk/pkg/a2a/executor.go` |
| Event queue wrapper | `go/adk/pkg/a2a/eventqueue.go` |
| CRD types (TemporalSpec) | `go/api/v1alpha2/agent_types.go` |
| Runtime config types | `go/api/adk/types.go` |
| CRD-to-config translator | `go/core/internal/controller/translator/agent/adk_api_translator.go` |
| Env var definitions | `go/core/pkg/env/kagent.go` |
| Temporal-MCP plugin (observability) | `go/plugins/temporal-mcp/` |
| E2E tests | `go/core/test/e2e/temporal_test.go` |
| Config conversion tests | `go/adk/pkg/temporal/config_convert_test.go` |
| Worker tests | `go/adk/pkg/temporal/worker_test.go` |
