# Test Gap Analysis: Temporal Agent Workflow E2E

## Coverage Summary

Existing `temporal_test.go` covers ~40-50% of design doc acceptance criteria.

## Covered Acceptance Criteria

| Criteria | Test | Notes |
|----------|------|-------|
| Infrastructure deployment | `TestE2ETemporalInfrastructure` | Verifies services + ports |
| CRD translation (env vars) | `TestE2ETemporalAgentCRDTranslation` | TEMPORAL_HOST_ADDR, NATS_ADDR |
| Basic workflow execution | `TestE2ETemporalWorkflowExecution` | sync + streaming |
| Fallback path (no temporal) | `TestE2ETemporalFallbackPath` | Sync invocation |
| Pod crash recovery | `TestE2ETemporalCrashRecovery` | Gated: TEMPORAL_CRASH_RECOVERY_TEST=1 |
| Custom timeout/retry config | `TestE2ETemporalWithCustomTimeout` | CRD persists, agent responds |
| Temporal UI plugin | `TestE2ETemporalUIPlugin` | Only route availability |
| Workflow visibility | `TestE2ETemporalWorkflowVisibleInTemporalUI` | Gated: TEMPORAL_UI_TEST=1, uses tctl |

## NOT Covered (Critical Gaps)

| Criteria | Impact | Why Missing |
|----------|--------|-------------|
| HITL approval flow (signal send/receive) | Critical | No mock for approval-requiring response |
| Child workflow (A2A multi-agent) | Critical | No multi-agent setup in E2E |
| Tool execution with streaming events | High | Mock only has terminal LLM response |
| NATS event validation (direct) | Medium | Streaming tested via A2A SSE, not NATS directly |
| Activity retry behavior | Medium | No failure injection mock |
| Per-agent task queue isolation | Medium | No concurrent multi-agent test |

## Missing Mock Data

Current `invoke_temporal_agent.json` only has single-turn "What is the capital of France?" → "Paris".

Needed mocks:
- Multi-turn with tool calls (test parallel tool execution + NATS events)
- HITL approval request (test signal flow)
- A2A agent invocation (test child workflows)
- Failure then recovery (test retry behavior)

## Implementation Features Without E2E Coverage

| Feature | Location | Unit Tested? |
|---------|----------|-------------|
| `executeToolsInParallel()` | workflows.go | Yes (unit) |
| `PublishApprovalActivity` | activities.go | Yes (unit) |
| `executeChildWorkflows()` | workflows.go | Yes (unit) |
| `MaxTurns = 100` limit | workflows.go | No |
| `ToolExecuteActivity` with NATS | activities.go | Yes (unit, embedded NATS) |
| Error/rejection status mapping | temporal_executor.go | Yes (unit) |
