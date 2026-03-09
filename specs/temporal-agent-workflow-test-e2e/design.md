# Temporal Agent Workflow E2E Testing -- Design Document

## Overview

Complete the E2E test coverage for kagent's Temporal agent workflow feature and integrate it into CI. The existing `temporal_test.go` covers ~40-50% of the design doc acceptance criteria. This work fills the gaps — focusing on integration concerns that unit tests cannot validate — and wires the tests into the GitHub Actions CI pipeline.

**Scope:** E2E tests only. No changes to Temporal workflow/activity implementation. No changes to Helm charts or CRD types.

## Detailed Requirements

1. Add E2E tests for tool execution via Temporal workflows (multi-turn LLM + tool call loop)
2. Add E2E test for HITL approval signal flow (workflow pauses, signal sent, workflow resumes)
3. Add E2E test for multi-agent child workflow orchestration (agent A invokes agent B)
4. Add mock LLM response files for tool calls, HITL, and A2A scenarios
5. Wire `test-e2e-temporal` into GitHub Actions CI pipeline
6. Consolidate environment variable gating (reduce from 3 env vars to 1)
7. All new tests reuse existing E2E framework helpers (`setupTemporalAgent`, `runSyncTest`, etc.)

## Architecture Overview

```
Test Layers (existing + new)
│
├── Layer 1: Unit Tests (go/adk/pkg/temporal/*_test.go)
│   └── Workflow logic, activities, client -- all mocked, < 5s
│
├── Layer 2: Integration Tests (embedded NATS)
│   └── NATS pub/sub, event forwarding -- < 2s
│
└── Layer 3: E2E Tests (go/core/test/e2e/temporal_test.go)  ← THIS WORK
    ├── EXISTING: infrastructure, CRD translation, basic workflow, fallback, crash recovery, custom config
    └── NEW: tool execution, HITL signals, child workflows, CI integration
```

```
E2E Test Flow (per test)
┌────────────┐    ┌──────────────┐    ┌─────────────┐    ┌──────────────┐
│ MockLLM    │    │ Agent CRD    │    │ Agent Pod    │    │ Temporal     │
│ Server     │◄───│ + ModelConfig│───▶│ (golang-adk) │───▶│ Server+NATS  │
│ (host)     │    │ (K8s)        │    │ (K8s)        │    │ (K8s)        │
└────────────┘    └──────────────┘    └──────┬───────┘    └──────────────┘
                                             │
                                     ┌───────▼───────┐
                                     │ A2A Client    │
                                     │ (test code)   │
                                     └───────────────┘
```

## Components and Interfaces

### 1. New Mock LLM Response Files

**Location:** `go/core/test/e2e/mocks/`

#### `invoke_temporal_with_tools.json`

Multi-turn mock: first response returns tool calls, second response returns final answer.

```json
{
  "openai": [
    {
      "name": "temporal_tool_call_request",
      "match": { "match_type": "contains", "message": { "content": "What tools do you have?", "role": "user" } },
      "response": {
        "choices": [{
          "message": {
            "role": "assistant",
            "content": null,
            "tool_calls": [{
              "id": "call_1",
              "type": "function",
              "function": { "name": "echo", "arguments": "{\"message\": \"hello\"}" }
            }]
          },
          "finish_reason": "tool_calls"
        }]
      }
    },
    {
      "name": "temporal_tool_result_response",
      "match": { "match_type": "contains", "message": { "content": "hello", "role": "tool" } },
      "response": {
        "choices": [{
          "message": { "role": "assistant", "content": "I used the echo tool and got: hello" },
          "finish_reason": "stop"
        }]
      }
    }
  ]
}
```

#### `invoke_temporal_hitl.json`

Mock that triggers HITL approval flow. The workflow detects approval requirement from a specific LLM response pattern.

```json
{
  "openai": [
    {
      "name": "temporal_hitl_request",
      "match": { "match_type": "contains", "message": { "content": "deploy to production", "role": "user" } },
      "response": {
        "choices": [{
          "message": {
            "role": "assistant",
            "content": "I need approval to deploy to production. [APPROVAL_REQUIRED]"
          },
          "finish_reason": "stop"
        }]
      }
    },
    {
      "name": "temporal_hitl_approved",
      "match": { "match_type": "contains", "message": { "content": "approved", "role": "user" } },
      "response": {
        "choices": [{
          "message": { "role": "assistant", "content": "Deployment to production completed successfully." },
          "finish_reason": "stop"
        }]
      }
    }
  ]
}
```

> **Note:** The exact mock structure depends on how the workflow detects HITL requirements from LLM responses. If the current implementation uses a specific tool call or response pattern, the mock must match that. Research shows `PublishApprovalActivity` is triggered by workflow logic — the mock needs to produce a response the workflow recognizes as needing approval. Implementation will verify the actual detection mechanism.

#### `invoke_temporal_child.json`

Mock for parent agent that invokes a child agent via A2A tool call.

```json
{
  "openai": [
    {
      "name": "temporal_parent_invokes_child",
      "match": { "match_type": "contains", "message": { "content": "ask the specialist", "role": "user" } },
      "response": {
        "choices": [{
          "message": {
            "role": "assistant",
            "content": null,
            "tool_calls": [{
              "id": "call_a2a_1",
              "type": "function",
              "function": { "name": "invoke_agent", "arguments": "{\"agent\": \"temporal-child-test\", \"message\": \"What is 2+2?\"}" }
            }]
          },
          "finish_reason": "tool_calls"
        }]
      }
    },
    {
      "name": "temporal_parent_after_child",
      "match": { "match_type": "contains", "message": { "content": "4", "role": "tool" } },
      "response": {
        "choices": [{
          "message": { "role": "assistant", "content": "The specialist says the answer is 4." },
          "finish_reason": "stop"
        }]
      }
    }
  ]
}
```

### 2. New E2E Test Functions

**Location:** `go/core/test/e2e/temporal_test.go`

#### `TestE2ETemporalToolExecution`

Verifies multi-turn workflow with tool calls through the full stack.

```go
func TestE2ETemporalToolExecution(t *testing.T) {
    skipIfNoTemporal(t)
    waitForTemporalReady(t)
    waitForNATSReady(t)

    baseURL, stopServer := setupMockServer(t, "mocks/invoke_temporal_with_tools.json")
    defer stopServer()

    cli := setupK8sClient(t, true) // v1alpha1 for MCPServer
    modelCfg := setupModelConfig(t, cli, baseURL)
    mcpServer := setupMCPServer(t, cli) // MCP server with "echo" tool

    tools := []*v1alpha2.Tool{{
        TypedLocalReference: v1alpha2.TypedLocalReference{
            Kind: "MCPServer",
            Name: mcpServer.Name,
        },
    }}

    agent := setupTemporalAgent(t, cli, modelCfg.Name, AgentOptions{
        Name: "temporal-tool-test",
    })
    // Note: setupTemporalAgent may need extension to accept tools parameter

    a2aClient := setupA2AClient(t, agent)

    t.Run("tool_call_workflow", func(t *testing.T) {
        runSyncTest(t, a2aClient, "What tools do you have?", "echo", nil)
    })

    t.Run("tool_call_streaming", func(t *testing.T) {
        runStreamingTest(t, a2aClient, "What tools do you have?", "echo")
    })
}
```

**What this validates (not covered by unit tests):**
- Real Temporal server schedules and executes multi-turn workflow
- Real MCP tool call via the agent pod's MCP registry
- Tool results flow through Temporal activity → workflow → response
- A2A response correctly includes tool execution result

#### `TestE2ETemporalHITLApproval`

Verifies the HITL signal flow end-to-end.

```go
func TestE2ETemporalHITLApproval(t *testing.T) {
    skipIfNoTemporal(t)
    waitForTemporalReady(t)
    waitForNATSReady(t)

    baseURL, stopServer := setupMockServer(t, "mocks/invoke_temporal_hitl.json")
    defer stopServer()

    cli := setupK8sClient(t, false)
    modelCfg := setupModelConfig(t, cli, baseURL)
    agent := setupTemporalAgent(t, cli, modelCfg.Name, AgentOptions{
        Name: "temporal-hitl-test",
    })

    // Send message that triggers HITL approval.
    // Use streaming to observe the approval_request event in SSE.
    a2aClient := setupA2AClient(t, agent)

    // Start workflow in background (will block waiting for approval).
    // Send approval signal.
    // Verify workflow completes with approved result.
    //
    // Implementation depends on:
    // 1. How the workflow detects HITL requirement from LLM response
    // 2. Whether the approval HTTP endpoint exists (POST /api/sessions/{id}/approve)
    // 3. Or whether we use the Temporal client directly to signal
}
```

> **Design note:** The HITL E2E test is the most complex because the workflow blocks waiting for a signal. The test must either:
> - (a) Use the A2A streaming client to detect the `input_required` task state, then call the approval HTTP endpoint
> - (b) Use a Temporal client directly to send the signal
> - (c) Use `kubectl exec` into the Temporal server pod to send the signal via tctl
>
> Option (a) is preferred as it tests the full user-facing flow. Implementation will verify which approach is viable given the current HTTP handler setup.

#### `TestE2ETemporalChildWorkflow`

Verifies multi-agent orchestration via child workflows.

```go
func TestE2ETemporalChildWorkflow(t *testing.T) {
    skipIfNoTemporal(t)
    waitForTemporalReady(t)
    waitForNATSReady(t)

    // Two mock servers: parent and child agent LLMs.
    parentURL, stopParent := setupMockServer(t, "mocks/invoke_temporal_child.json")
    defer stopParent()
    childURL, stopChild := setupMockServer(t, "mocks/invoke_temporal_agent.json")
    defer stopChild()

    cli := setupK8sClient(t, false)
    parentModelCfg := setupModelConfig(t, cli, parentURL)
    childModelCfg := setupModelConfig(t, cli, childURL)

    // Create child agent first (must be ready before parent invokes it).
    childAgent := setupTemporalAgent(t, cli, childModelCfg.Name, AgentOptions{
        Name: "temporal-child-test",
    })

    // Create parent agent.
    parentAgent := setupTemporalAgent(t, cli, parentModelCfg.Name, AgentOptions{
        Name: "temporal-parent-test",
    })

    a2aClient := setupA2AClient(t, parentAgent)

    t.Run("parent_invokes_child", func(t *testing.T) {
        runSyncTest(t, a2aClient, "ask the specialist", "4", nil)
    })
}
```

**What this validates (not covered by unit tests):**
- Parent workflow starts child workflow on different task queue
- Child workflow executes on child agent's worker
- Child result propagates back to parent
- Both agents use real Temporal server for orchestration
- Task queue isolation (each agent on its own queue)

### 3. Helper Extensions

#### Extend `setupTemporalAgent` to Accept Tools

Current signature: `setupTemporalAgent(t, cli, modelConfigName, opts)`

The function currently doesn't accept tools. For the tool execution test, either:
- (a) Add a `Tools` field to `AgentOptions` (preferred, benefits all tests)
- (b) Create a new helper `setupTemporalAgentWithTools(t, cli, modelConfigName, tools, opts)`

**Recommendation:** Option (a) — add `Tools []*v1alpha2.Tool` to `AgentOptions`.

### 4. CI Pipeline Integration

**File:** `.github/workflows/ci.yaml`

Add a new job `test-e2e-temporal` that runs after the build job:

```yaml
test-e2e-temporal:
  name: E2E Tests (Temporal)
  needs: [build]
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v4
    - uses: actions/setup-go@v5
      with:
        go-version-file: go/go.work
    - name: Setup Kind cluster
      uses: helm/kind-action@v1
    - name: Load images
      # ... load golang-adk, controller, etc. from build artifacts
    - name: Helm install with Temporal
      run: make helm-install-temporal
    - name: Wait for Temporal readiness
      run: |
        kubectl wait --for=condition=Available --timeout=120s deployment/kagent-temporal-server -n kagent
        kubectl wait --for=condition=Available --timeout=60s deployment/kagent-nats -n kagent
    - name: Run Temporal E2E tests
      env:
        TEMPORAL_ENABLED: "1"
        KAGENT_URL: "http://localhost:8083"
        KAGENT_LOCAL_HOST: "172.17.0.1"
      run: make -C go e2e-temporal
```

**Timing:** ~3-4 min additional CI time (Kind setup + Helm + Temporal readiness + tests).

### 5. Environment Variable Consolidation

Current state: 3 separate env vars to run all temporal tests:
- `TEMPORAL_ENABLED=1` (main gate)
- `TEMPORAL_CRASH_RECOVERY_TEST=1` (crash recovery, slow/destructive)
- `TEMPORAL_UI_TEST=1` (tctl in pod)

**Decision:** Keep the separation. Crash recovery is genuinely destructive (kills pods) and slow. UI test requires `tctl` in the Temporal pod. CI should run `TEMPORAL_ENABLED=1` by default and optionally enable the others.

For CI, use:
```bash
TEMPORAL_ENABLED=1  # Always in CI temporal job
# TEMPORAL_CRASH_RECOVERY_TEST=1  # Optional, add later when stable
# TEMPORAL_UI_TEST=1  # Optional, requires tctl in image
```

## Data Models

No new data models. Tests use existing types:
- `v1alpha2.Agent` with `Spec.Temporal`
- `v1alpha2.TemporalSpec` (Enabled, WorkflowTimeout, RetryPolicy)
- Mock LLM responses (OpenAI chat.completion format)
- A2A protocol messages (`protocol.Task`, `protocol.Message`)

## Error Handling

Tests handle errors via:
- `require.NoError(t, err)` for fatal setup errors
- `assert.True(t, condition)` for verification assertions
- `wait.PollUntilContextTimeout()` for readiness polling (120s timeout)
- `t.Skip()` when infrastructure unavailable
- `cleanup()` with debug output on failure (logs, pod describe, agent yaml)
- `SKIP_CLEANUP=1` preserves failed resources for debugging

## Acceptance Criteria

**Given** Temporal server and NATS are deployed in the Kind cluster,
**When** `TEMPORAL_ENABLED=1` is set and temporal E2E tests run,
**Then** all temporal E2E tests pass including tool execution, HITL, and child workflow tests.

**Given** the CI pipeline runs the `test-e2e-temporal` job,
**When** a PR is submitted with changes to `go/adk/pkg/temporal/` or `go/adk/pkg/a2a/`,
**Then** temporal E2E tests execute and must pass for the PR to merge.

**Given** an agent with `temporal.enabled: true` and MCP tools configured,
**When** the LLM response includes tool calls,
**Then** the Temporal workflow executes tools via activities and returns the final response via A2A.

**Given** a parent agent configured to invoke a child agent via A2A,
**When** the parent sends a message triggering `invoke_agent` tool call,
**Then** a child workflow executes on the child agent's task queue and the result propagates to the parent.

**Given** the existing E2E test suite (non-temporal),
**When** `TEMPORAL_ENABLED` is not set,
**Then** all temporal tests are skipped and non-temporal tests are unaffected.

## Testing Strategy

This document IS the testing strategy. The tests are organized by priority:

### Priority 1 (CI-ready, must pass)
- `TestE2ETemporalInfrastructure` (existing)
- `TestE2ETemporalAgentCRDTranslation` (existing)
- `TestE2ETemporalWorkflowExecution` (existing)
- `TestE2ETemporalFallbackPath` (existing)
- `TestE2ETemporalWithCustomTimeout` (existing)
- `TestE2ETemporalToolExecution` (**new**)

### Priority 2 (CI-ready, complex setup)
- `TestE2ETemporalChildWorkflow` (**new**)
- `TestE2ETemporalUIPlugin` (existing)

### Priority 3 (Gated, optional in CI)
- `TestE2ETemporalHITLApproval` (**new** — depends on HITL HTTP endpoint availability)
- `TestE2ETemporalCrashRecovery` (existing, destructive)
- `TestE2ETemporalWorkflowVisibleInTemporalUI` (existing, requires tctl)

## Appendices

### A. Technology Choices

| Choice | Rationale |
|--------|-----------|
| Extend `temporal_test.go` (not new file) | All temporal E2E tests in one place, shared helpers |
| Separate CI job (not matrix) | Temporal infra adds ~3 min; don't slow down base E2E |
| Keep env var gating | Temporal tests require infrastructure not always available |
| MockLLM for tool calls | Deterministic responses, no real LLM needed |
| `setupMCPServer` for tool test | Reuse existing "everything" MCP server from framework |

### B. Research References

- [test-gap-analysis.md](research/test-gap-analysis.md) — Coverage gaps vs design doc acceptance criteria
- [e2e-framework-patterns.md](research/e2e-framework-patterns.md) — E2E framework helpers and patterns
- [ci-pipeline.md](research/ci-pipeline.md) — CI integration analysis
- [test-boundaries.md](research/test-boundaries.md) — Unit vs integration vs E2E responsibilities

### C. Dependencies on Implementation Status

The following tests depend on implementation features being complete:

| Test | Dependency | Status |
|------|-----------|--------|
| Tool execution | `ToolExecuteActivity` + MCP registry in agent pod | Implemented |
| HITL approval | HITL detection in workflow + approval HTTP endpoint | Partially implemented (no HTTP endpoint confirmed) |
| Child workflow | `invoke_agent` tool call detection + child workflow routing | Implemented |
| CI job | `helm-install-temporal` Makefile target | Exists |

### D. Mock File Naming Convention

```
mocks/
├── invoke_temporal_agent.json          # Existing: simple single-turn
├── invoke_temporal_with_tools.json     # New: multi-turn with tool calls
├── invoke_temporal_hitl.json           # New: HITL approval flow
├── invoke_temporal_child.json          # New: parent agent A2A invocation
└── invoke_temporal_child_response.json # New: child agent response (reuse invoke_temporal_agent.json)
```
