# Implementation Plan

## Checklist

- [ ] Step 1: Add Temporal + NATS SDK dependencies and package structure
- [ ] Step 2: Implement NATS streaming publisher/subscriber
- [ ] Step 3: Implement activities (LLM, Tool, Session, Task) with NATS streaming
- [ ] Step 4: Implement AgentExecutionWorkflow with per-turn activities
- [ ] Step 5: Implement HITL signal support in workflow
- [ ] Step 6: Implement child workflows for A2A multi-agent
- [ ] Step 7: Implement worker and client
- [ ] Step 8: Integrate into executor.go with per-agent CRD spec gate
- [ ] Step 9: Add TemporalSpec to Agent CRD and translator
- [ ] Step 10: Helm chart: Temporal server (SQLite/PostgreSQL), NATS, Temporal UI plugin
- [ ] Step 11: E2E tests with Kind + Temporal + NATS

---

## Step 1: Add Temporal + NATS SDK dependencies and package structure

**Objective:** Bootstrap `pkg/temporal/` and `pkg/streaming/` packages with SDK dependencies.

**Implementation:**
- `cd go/adk && go get go.temporal.io/sdk@latest go.temporal.io/api@latest github.com/nats-io/nats.go@latest`
- Create `go/adk/pkg/temporal/` with stubs: `workflows.go`, `activities.go`, `worker.go`, `client.go`, `types.go`
- Create `go/adk/pkg/streaming/` with stubs: `nats.go`, `types.go`
- Define all request/response types in `types.go` files

**Test requirements:**
- `go build ./...` succeeds

**Demo:** Build passes with Temporal + NATS SDKs imported.

---

## Step 2: Implement NATS streaming publisher/subscriber

**Objective:** Build the streaming side-channel for real-time LLM token and tool progress delivery.

**Implementation:**
- `StreamPublisher` struct with `*nats.Conn`
  - `PublishToken(subject, *StreamEvent) error`
  - `PublishToolProgress(subject, *StreamEvent) error`
  - `PublishApprovalRequest(subject, *ApprovalRequest) error`
- `StreamSubscriber` struct with `*nats.Conn`
  - `Subscribe(subject, handler) (*nats.Subscription, error)`
- `StreamEvent` type: `{Type, Data, Timestamp}`
- Subject pattern: `agent.{agentName}.{sessionID}.stream`
- `NewNATSConnection(addr string) (*nats.Conn, error)` helper

**Test requirements:**
- Unit test publish/subscribe roundtrip with embedded NATS server (`github.com/nats-io/nats-server/v2/test`)
- Test event serialization/deserialization
- Test subscription cleanup

**Demo:** Token published to NATS subject, subscriber receives it in real-time.

---

## Step 3: Implement activities (LLM, Tool, Session, Task) with NATS streaming

**Objective:** Wrap LLM invocation, MCP tool execution, session management, and task persistence as Temporal activities. LLM and tool activities publish progress to NATS.

**Implementation:**
- `Activities` struct holding `agentFactory`, `sessionSvc`, `taskStore`, `mcpRegistry`, `natsConn`
- `LLMInvokeActivity(ctx, *LLMRequest) (*LLMResponse, error)`
  - Create LLM model from config
  - Execute single chat completion turn
  - Stream tokens to NATS via `StreamPublisher` as they arrive
  - Return full response + any tool calls
- `ToolExecuteActivity(ctx, *ToolRequest) (*ToolResponse, error)`
  - Call MCP tool via existing registry
  - Publish tool start/end events to NATS
  - Use `activity.RecordHeartbeat(ctx)` for long-running tools
- `SessionActivity(ctx, *SessionRequest) (*SessionResponse, error)` -- create/get session
- `AppendEventActivity(ctx, *AppendEventRequest) error` -- append event to session
- `SaveTaskActivity(ctx, *TaskSaveRequest) error` -- persist A2A task

**Test requirements:**
- Unit test each activity with mocked LLM, MCP, session service, task store
- Test LLM activity publishes tokens to NATS
- Test tool activity publishes start/end events
- Test retry behavior for transient LLM/tool errors
- Test heartbeat for long-running tool calls

**Demo:** Activity unit tests pass; LLM activity streams tokens to embedded NATS.

---

## Step 4: Implement AgentExecutionWorkflow with per-turn activities

**Objective:** Core workflow orchestrating the LLM + tool loop with per-turn granularity.

**Implementation:**
- `AgentExecutionWorkflow(ctx, *ExecutionRequest) (*ExecutionResult, error)`
- Configure per-activity retry policies and timeouts from `ExecutionRequest.Config`
- Workflow timeout: `workflowTimeout` from config (default 48h)
- Session creation as first activity
- LLM invoke loop:
  1. Call `LLMInvokeActivity` (single turn)
  2. If tool calls in response: execute each tool via `ToolExecuteActivity` in parallel (`workflow.Go` goroutines)
  3. Append tool results to conversation history
  4. Repeat from step 1
- Terminal condition: LLM response with no tool calls and no HITL request
- `AppendEventActivity` after each turn
- `SaveTaskActivity` as final step

**Test requirements:**
- Workflow unit tests using `testsuite.WorkflowTestSuite`
- Test determinism: workflow replays correctly after simulated crash
- Test tool call loop terminates on final response
- Test parallel tool execution (multiple tools in one turn)
- Test max turn limit (safety bound)

**Demo:** Workflow test suite passes with mocked activities simulating multi-turn LLM + tool execution.

---

## Step 5: Implement HITL signal support in workflow

**Objective:** Workflow blocks on Temporal signal for human approval, publishes request via NATS.

**Implementation:**
- In `AgentExecutionWorkflow`, detect HITL approval requirement from LLM response
- Publish `ApprovalRequest` to NATS via side-effect (`workflow.SideEffect` or dedicated activity)
- Create signal channel: `workflow.GetSignalChannel(ctx, "approval")`
- Block on signal: `approvalCh.Receive(ctx, &decision)`
- If approved, continue loop; if rejected, return rejection result
- Add HTTP handler: `POST /api/sessions/{sessionID}/approve`
  - Handler calls `temporalClient.SignalWorkflow(workflowID, "approval", decision)`
  - Register in `go/core/internal/httpserver/server.go`

**Test requirements:**
- Workflow test: send approval signal -> workflow resumes
- Workflow test: send rejection signal -> workflow returns rejection
- Workflow test: no signal within timeout -> workflow continues waiting (up to 48h)
- HTTP handler test: mock Temporal client, verify signal sent

**Demo:** Workflow pauses at HITL, HTTP POST sends signal, workflow resumes.

---

## Step 6: Implement child workflows for A2A multi-agent

**Objective:** A2A agent invocations become child workflows on the target agent's task queue.

**Implementation:**
- In `AgentExecutionWorkflow`, detect A2A tool calls (`invoke_agent` tool)
- Extract target agent name and message from tool call
- Start child workflow:
  ```go
  childOpts := workflow.ChildWorkflowOptions{
      TaskQueue:         "agent-" + targetAgentName,
      WorkflowID:        parentSessionID + "-child-" + targetAgentName,
      ExecutionTimeout:  48 * time.Hour,
      ParentClosePolicy: enums.PARENT_CLOSE_POLICY_TERMINATE,
  }
  ```
- Wait for child completion, incorporate result into parent conversation history
- Child workflow uses its own NATS subject for streaming
- Support parallel child workflows (multiple A2A calls in one turn)

**Test requirements:**
- Workflow test: parent starts child, receives result
- Workflow test: child failure propagates to parent
- Workflow test: parent cancellation terminates children
- Workflow test: parallel child workflows complete independently

**Demo:** Parent workflow starts child on different task queue; both visible as linked workflows in test output.

---

## Step 7: Implement worker and client

**Objective:** Worker polls per-agent Temporal task queue; client starts workflows and sends signals.

**Implementation:**
- `worker.go`:
  - `NewWorker(cfg WorkerConfig, activities *Activities) (worker.Worker, error)`
  - Registers `AgentExecutionWorkflow` and all activities
  - Task queue: `agent-{agentName}` from config
  - Returns `worker.Worker` for lifecycle management
- `client.go`:
  - `NewClient(cfg ClientConfig) (*Client, error)` -- dials Temporal server
  - `ExecuteAgent(ctx, *ExecutionRequest) (client.WorkflowRun, error)` -- starts workflow, returns run handle
  - `SignalApproval(ctx, workflowID, *ApprovalDecision) error` -- sends HITL signal
  - `GetWorkflowStatus(ctx, workflowID) (*WorkflowStatus, error)` -- query workflow state

**Test requirements:**
- Integration test with `temporalite` (in-process Temporal dev server)
- Test full workflow execution: start -> activities -> complete
- Test signal delivery
- Test workflow status query

**Demo:** Full workflow executes against local Temporal dev server with mocked LLM, tokens stream via embedded NATS.

---

## Step 8: Integrate into executor.go with per-agent CRD spec gate

**Objective:** Wire Temporal + NATS into the A2A executor, controlled per-agent via CRD spec.

**Implementation:**
- Modify `go/adk/pkg/a2a/executor.go`:
  - Check `agentConfig.Temporal.Enabled`
  - If true: start workflow via `temporal.Client`, subscribe to NATS for streaming, forward to SSE
  - If false: use existing synchronous `Agent.Run()` path (zero change)
- Modify `go/adk/pkg/app/app.go`:
  - If Temporal config present: create Temporal client + worker, create NATS connection
  - Start worker alongside A2A server
  - Graceful shutdown: stop worker, close NATS, then stop A2A server
- NATS subscriber in A2A server forwards `StreamEvent` to SSE response writer

**Test requirements:**
- Test feature gate: disabled path unchanged
- Test enabled path: workflow started, NATS subscription active
- Integration test with temporalite + embedded NATS
- Test graceful shutdown sequence

**Demo:** Agent pod with `temporal.enabled: true` in config executes via workflow with streaming; `false` uses old path.

---

## Step 9: Add TemporalSpec to Agent CRD and translator

**Objective:** Per-agent Temporal configuration via Agent CRD spec.

**Implementation:**
- Add `TemporalSpec` and `TemporalRetryPolicy` structs to `go/api/v1alpha2/agent_types.go`
  - `Enabled bool`, `WorkflowTimeout *metav1.Duration`, `RetryPolicy *TemporalRetryPolicy`
- Add `Temporal *TemporalSpec` field to `AgentSpec`
- Run `make -C go generate` (CRD codegen, deepcopy)
- Update `go/core/internal/controller/translator/agent/adk_api_translator.go`:
  - Translate `TemporalSpec` -> `TemporalConfig` in `config.json`
  - Set `taskQueue` to `agent-{agentName}`
  - Inject `TEMPORAL_HOST_ADDR` and `NATS_ADDR` env vars into pod spec
- Update `deployments.go` with Temporal/NATS env vars when `spec.temporal.enabled`
- Update Helm CRD templates

**Test requirements:**
- Unit test translator generates correct config.json with Temporal fields
- Unit test translator injects correct env vars
- E2E test: Agent CRD with `temporal.enabled: true` -> pod has correct config

**Demo:** `kubectl apply` Agent CRD with temporal config, pod starts with correct env vars and config.json.

---

## Step 10: Helm chart: Temporal server (SQLite/PostgreSQL), NATS, Temporal UI plugin

**Objective:** K8s-native deployment of all infrastructure via Helm.

**Implementation:**
- **Temporal server** -- Templates in `helm/kagent/templates/temporal-*` (part of main kagent chart, not a subchart)
  - `values.yaml` with `persistence.driver` switch: `sqlite` (dev) / `postgresql` (prod)
  - SQLite: uses `temporal server start-dev --headless` with CLI args (NOT env vars -- `temporalio/auto-setup` does not support SQLite via env vars); single-replica, emptyDir volume at `/temporal-data/`
  - PostgreSQL: uses `temporalio/auto-setup` with env vars (`DB=postgres12`, `DB_PORT`, `DBNAME`, `POSTGRES_SEEDS`, `POSTGRES_USER`, `POSTGRES_PWD`); configurable host/port/credentials/existingSecret
  - Temporal UI service on port 8080
- **NATS** -- Add embedded NATS deployment to `helm/kagent/`
  - Single-replica `nats:2-alpine` container
  - Service on port 4222
  - Auto-enabled when `temporal.enabled: true`
- **Temporal UI plugin** -- Add `RemoteMCPServer` manifest for Temporal UI
  - Plugin proxy routes `/temporal/*` to `temporal-ui:8080`
  - Sidebar entry: "Temporal Workflows" with badge
- Update `helm/kagent/values.yaml` with `temporal.*` and `nats.*` sections
- Update `Makefile` with `helm-install-temporal` target

**Test requirements:**
- `helm lint` passes for all charts
- `helm template` generates correct manifests for SQLite and PostgreSQL modes
- Kind cluster deployment succeeds with `make helm-install`
- Temporal UI accessible via plugin proxy

**Demo:** `make helm-install` deploys kagent + Temporal + NATS; Temporal UI visible in kagent sidebar.

---

## Step 11: E2E tests with Kind + Temporal + NATS

**Objective:** End-to-end validation of the full pipeline.

**Implementation:**
- Add Temporal + NATS to Kind cluster setup (Makefile target `create-kind-cluster-temporal`)
- E2E tests in `go/core/test/e2e/`:
  - **Workflow execution**: Create Agent CRD with `temporal.enabled: true` -> send A2A message -> verify workflow completed in Temporal -> verify response received
  - **Streaming**: Verify SSE events received during workflow execution (tokens, tool progress)
  - **Crash recovery**: Kill agent pod mid-execution -> verify workflow resumes after pod restart
  - **HITL signal**: Send message requiring approval -> POST approve -> verify workflow completes
  - **Child workflow**: Agent A invokes Agent B -> verify child workflow on `agent-B` queue -> verify parent receives child result
  - **Fallback**: Agent without temporal spec -> verify synchronous execution unchanged
- Add to CI pipeline (`.github/workflows/`)

**Test requirements:**
- All E2E tests pass in Kind cluster
- Tests are idempotent and isolated
- CI pipeline completes within reasonable time

**Demo:** CI green with full Temporal E2E test suite.
