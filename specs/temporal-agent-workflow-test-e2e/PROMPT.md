# PROMPT: Temporal Agent Workflow E2E Tests

## Objective

Add missing E2E tests for kagent's Temporal agent workflow feature and wire them into CI. Fill coverage gaps for tool execution, child workflows, and HITL approval. No implementation changes — tests only.

## Key Requirements

1. Create mock LLM response files in `go/core/test/e2e/mocks/`:
   - `invoke_temporal_with_tools.json` — multi-turn: tool call then final response
   - `invoke_temporal_child.json` — parent agent invokes child via `invoke_agent` tool call
   - `invoke_temporal_hitl.json` — response triggering HITL approval flow
   - Match tool call format to what `go/adk/pkg/temporal/workflows.go` expects

2. Extend `AgentOptions` in `go/core/test/e2e/invoke_api_test.go`:
   - Add `Tools []*v1alpha2.Tool` field
   - Update `setupTemporalAgent()` to pass `opts.Tools` to `generateAgent()`

3. Add E2E tests to `go/core/test/e2e/temporal_test.go`:
   - `TestE2ETemporalToolExecution` — agent with MCP tools, multi-turn workflow via Temporal
   - `TestE2ETemporalChildWorkflow` — parent agent invokes child agent via child workflow
   - `TestE2ETemporalHITLApproval` — workflow pauses for approval signal, resumes after signal (defer if HTTP endpoint missing)

4. Add `test-e2e-temporal` job to `.github/workflows/ci.yaml`:
   - Uses `make helm-install-temporal` and `make -C go e2e-temporal`
   - Sets `TEMPORAL_ENABLED=1`
   - Path filter: `go/adk/pkg/temporal/**`, `go/adk/pkg/a2a/**`, `go/core/test/e2e/temporal_test.go`, `helm/kagent/templates/temporal*`

## Acceptance Criteria (Given-When-Then)

- **Given** Temporal+NATS deployed, agent with tools and `temporal.enabled: true`, **When** LLM returns tool calls, **Then** workflow executes tools via activities and returns final response via A2A
- **Given** two temporal agents (parent+child), **When** parent LLM returns `invoke_agent` tool call, **Then** child workflow executes on child's task queue and result propagates to parent
- **Given** `TEMPORAL_ENABLED` not set, **When** E2E tests run, **Then** all temporal tests are skipped
- **Given** CI runs `test-e2e-temporal` job, **When** PR changes temporal code, **Then** temporal E2E tests execute and pass

## Reference

- Spec directory: `specs/temporal-agent-workflow-test-e2e/`
- Design: `specs/temporal-agent-workflow-test-e2e/design.md`
- Plan: `specs/temporal-agent-workflow-test-e2e/plan.md`
- Existing tests: `go/core/test/e2e/temporal_test.go`
- Existing helpers: `go/core/test/e2e/invoke_api_test.go`
- Workflow implementation: `go/adk/pkg/temporal/workflows.go`
- Temporal executor: `go/adk/pkg/a2a/temporal_executor.go`
