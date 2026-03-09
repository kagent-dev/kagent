# Implementation Plan

## Checklist

- [ ] Step 1: Add new mock LLM response files for tool calls and child workflows
- [ ] Step 2: Extend AgentOptions with Tools field and update setupTemporalAgent
- [ ] Step 3: Add TestE2ETemporalToolExecution test
- [ ] Step 4: Add TestE2ETemporalChildWorkflow test
- [ ] Step 5: Add TestE2ETemporalHITLApproval test (if HTTP endpoint exists)
- [ ] Step 6: Wire temporal E2E tests into GitHub Actions CI pipeline

---

## Step 1: Add new mock LLM response files

**Objective:** Create deterministic mock LLM responses for multi-turn tool call and child workflow scenarios.

**Implementation:**
- Create `go/core/test/e2e/mocks/invoke_temporal_with_tools.json`
  - First match: user message containing "What tools" → response with `tool_calls` array (echo tool)
  - Second match: tool role message → final assistant response referencing tool result
- Create `go/core/test/e2e/mocks/invoke_temporal_child.json`
  - First match: user message containing "ask the specialist" → response with `invoke_agent` tool call
  - Second match: tool role message with child result → final assistant response
- Verify mock file format matches existing `invoke_temporal_agent.json` structure
- Verify tool call format matches what the golang-adk workflow parser expects (read `workflows.go` for tool call detection logic)

**Test requirements:**
- `go build ./...` still compiles (embedded mocks)
- Existing tests unaffected

**Demo:** New mock JSON files in `mocks/` directory, embedded correctly.

---

## Step 2: Extend AgentOptions with Tools field and update setupTemporalAgent

**Objective:** Allow temporal agent setup to include MCP tools, enabling tool execution tests.

**Implementation:**
- Add `Tools []*v1alpha2.Tool` field to `AgentOptions` struct in `invoke_api_test.go`
- Update `generateAgent()` to use `opts.Tools` when provided (currently tools come from function parameter)
- Update `setupTemporalAgent()` to pass `opts.Tools` through to `generateAgent()`
  - Current: `agent := generateAgent(modelConfigName, nil, opts)` — tools hardcoded to nil
  - New: `agent := generateAgent(modelConfigName, opts.Tools, opts)` — pass tools from opts
- Verify no existing tests break (tools param was separate, now also available via opts)

**Test requirements:**
- All existing E2E tests pass unchanged
- `setupTemporalAgent` accepts tools via AgentOptions

**Demo:** `setupTemporalAgent(t, cli, modelCfg.Name, AgentOptions{Tools: tools})` works.

---

## Step 3: Add TestE2ETemporalToolExecution test

**Objective:** Verify multi-turn Temporal workflow with real MCP tool execution through the full stack.

**Implementation:**
- Add test function `TestE2ETemporalToolExecution` to `temporal_test.go`
- Setup: mock server with `invoke_temporal_with_tools.json`, MCP server with echo tool, temporal agent with tools
- Test flow:
  1. `setupMockServer(t, "mocks/invoke_temporal_with_tools.json")`
  2. `setupMCPServer(t, cli)` — creates MCPServer with echo tool
  3. Create tools reference to MCPServer
  4. `setupTemporalAgent(t, cli, modelCfg.Name, AgentOptions{Name: "temporal-tool-test", Tools: tools})`
  5. `runSyncTest(t, a2aClient, "What tools do you have?", "echo", nil)`
  6. `runStreamingTest(t, a2aClient, "What tools do you have?", "echo")`
- Gate: `skipIfNoTemporal(t)`

**Test requirements:**
- Test passes with `TEMPORAL_ENABLED=1` against a cluster with Temporal + NATS
- Mock LLM returns tool call, agent executes tool via MCP, workflow loops back to LLM with result

**Demo:** `TEMPORAL_ENABLED=1 go test -v -run TestE2ETemporalToolExecution ./go/core/test/e2e`

---

## Step 4: Add TestE2ETemporalChildWorkflow test

**Objective:** Verify multi-agent orchestration where parent agent invokes child agent via Temporal child workflow.

**Implementation:**
- Add test function `TestE2ETemporalChildWorkflow` to `temporal_test.go`
- Setup: two mock servers (parent + child), two temporal agents
- Test flow:
  1. `setupMockServer(t, "mocks/invoke_temporal_child.json")` for parent
  2. `setupMockServer(t, "mocks/invoke_temporal_agent.json")` for child (reuse existing "capital of France" mock)
  3. Create child agent first: `setupTemporalAgent(t, cli, childModelCfg.Name, AgentOptions{Name: "temporal-child-test"})`
  4. Create parent agent: `setupTemporalAgent(t, cli, parentModelCfg.Name, AgentOptions{Name: "temporal-parent-test"})`
  5. `runSyncTest(t, a2aClient, "ask the specialist", "4", nil)` — parent invokes child, gets result
- Gate: `skipIfNoTemporal(t)`
- Note: verify the `invoke_agent` tool call format matches what `executeChildWorkflows()` in `workflows.go` expects. The mock must produce the exact function name and argument structure the workflow parser recognizes.

**Test requirements:**
- Both agents deployed and ready
- Parent workflow starts child workflow on `agent-temporal-child-test` task queue
- Child workflow completes and result propagates to parent
- Parent returns combined response via A2A

**Demo:** `TEMPORAL_ENABLED=1 go test -v -run TestE2ETemporalChildWorkflow ./go/core/test/e2e`

---

## Step 5: Add TestE2ETemporalHITLApproval test

**Objective:** Verify HITL signal flow end-to-end — workflow pauses for approval, signal sent, workflow resumes.

**Implementation:**
- First, verify the HITL detection mechanism:
  - Read `workflows.go` to understand how the workflow detects HITL requirement from LLM response
  - Read HTTP handlers to confirm approval endpoint exists (`POST /api/sessions/{id}/approve`)
  - If HTTP endpoint doesn't exist, this test is deferred (noted in design)
- If viable, add `TestE2ETemporalHITLApproval` to `temporal_test.go`:
  1. Setup mock server with `invoke_temporal_hitl.json`
  2. Create temporal agent
  3. Send message via streaming A2A client (to observe `input_required` state)
  4. In a goroutine: collect SSE events until `input_required` is seen
  5. Extract workflow ID from the event or task context
  6. Send approval signal via HTTP endpoint or Temporal client
  7. Wait for workflow to complete
  8. Verify final response contains approved result
- Gate: `skipIfNoTemporal(t)` + consider additional gate if HTTP endpoint is missing

**Test requirements:**
- Workflow blocks on approval signal (observable via SSE `input_required` state)
- Approval signal unblocks workflow
- Final response reflects approved path

**Demo:** `TEMPORAL_ENABLED=1 go test -v -run TestE2ETemporalHITLApproval ./go/core/test/e2e`

---

## Step 6: Wire temporal E2E tests into GitHub Actions CI

**Objective:** Temporal E2E tests run automatically in CI on PRs affecting temporal code.

**Implementation:**
- Add `test-e2e-temporal` job to `.github/workflows/ci.yaml`:
  - `needs: [build]` (reuse built images)
  - `runs-on: ubuntu-latest`
  - Steps: checkout, setup-go, Kind cluster, load images, `make helm-install-temporal`, wait for readiness, run tests
  - Environment: `TEMPORAL_ENABLED=1`, `KAGENT_URL=http://localhost:8083`, `KAGENT_LOCAL_HOST=172.17.0.1`
  - Test command: `make -C go e2e-temporal`
- Add path filter so job only runs when relevant files change:
  - `go/adk/pkg/temporal/**`
  - `go/adk/pkg/a2a/**`
  - `go/adk/pkg/streaming/**`
  - `go/core/test/e2e/temporal_test.go`
  - `helm/kagent/templates/temporal*`
  - `helm/kagent/templates/nats*`
- Verify `make -C go e2e-temporal` target runs correct test subset
- Keep crash recovery and UI tests out of CI for now (separate env var gates)

**Test requirements:**
- CI job passes on a clean PR
- Non-temporal E2E tests unaffected
- Job adds ~3-4 min to CI time (only when temporal files change)

**Demo:** PR touching `go/adk/pkg/temporal/` triggers `test-e2e-temporal` job and passes.
