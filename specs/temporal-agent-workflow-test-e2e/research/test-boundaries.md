# Test Boundaries: Unit vs Integration vs E2E

## Layered Test Architecture

### Layer 1: Pure Unit Tests (go/adk/pkg/temporal/*_test.go)

**Speed:** < 5 seconds total
**Dependencies:** None (all mocked)

Uses `testsuite.WorkflowTestSuite` for workflow tests:
- Activities are mocked via `s.env.OnActivity(...).Return(...)`
- Workflow logic tested in isolation
- Deterministic replay verified

Coverage (comprehensive):
- Single/multi-turn LLM + tool loops
- Parallel tool execution
- HITL approval + rejection signals
- Child workflow success + failure
- Error handling paths
- Config parsing

Activity tests use dependency injection:
- `mockSessionService` with configurable errors
- Function-type mocks for ModelInvoker, ToolExecutor
- Optional embedded NATS for streaming tests

### Layer 2: Hybrid Integration Tests (embedded NATS)

**Speed:** < 2 seconds
**Dependencies:** In-process NATS server only

Tests in `activities_test.go` and `temporal_executor_test.go`:
- `TestLLMInvokeActivity_WithNATSStreaming` — real NATS pub/sub
- `TestToolExecuteActivity_WithNATSEvents` — tool start/end via NATS
- `TestTemporalExecutor_NATSStreaming` — full event forwarding

Pattern:
```go
ns, addr := startEmbeddedNATS(t)  // in-process NATS
conn := connectNATS(t, addr)
// ... test with real NATS messaging
```

### Layer 3: E2E Tests (go/core/test/e2e/temporal_test.go)

**Speed:** 30-120 seconds per test
**Dependencies:** Kind cluster + Temporal + NATS + agent pods

Tests full stack:
- Agent CRD → controller → pod deployment → env vars
- Real Temporal server workflow execution
- Real NATS connectivity
- A2A message → workflow → response
- Pod crash → recovery → new request
- Custom CRD config persisted and applied

## What Each Layer Validates

| Concern | Unit | Integration | E2E |
|---------|------|-------------|-----|
| Workflow logic (turn loop) | ✅ | - | - |
| Activity implementation | ✅ | - | - |
| NATS event publishing | - | ✅ | Indirect |
| Client wrapper | ✅ | - | - |
| Config conversion | ✅ | - | - |
| A2A executor event mapping | ✅ | ✅ | ✅ |
| CRD → pod translation | - | - | ✅ |
| Real Temporal server | - | - | ✅ |
| Pod crash recovery | - | - | ✅ |
| Helm deployment | - | - | ✅ |

## Key Insight

Unit tests already cover HITL, child workflows, and tool execution thoroughly.
E2E tests should focus on **integration concerns** not testable at unit level:
- CRD translation correctness
- Real Temporal server behavior
- Pod lifecycle (crash recovery)
- End-to-end message flow
- Infrastructure health
