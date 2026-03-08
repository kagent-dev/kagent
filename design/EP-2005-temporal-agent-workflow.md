# EP-2005: Temporal Integration for Durable Agent Execution

* Status: **Implemented**
* Spec: [specs/temporal-agent-workflow](../specs/temporal-agent-workflow/)

## Background

Integrate Temporal as a durable workflow executor for kagent's Go ADK. Each agent execution becomes a Temporal workflow with per-turn LLM activities and per-call tool activities, providing crash recovery, retry policies, and execution history.

## Motivation

Agent executions can be long-running (hours) and involve multiple LLM calls and tool invocations. Without durable execution, a pod restart loses all progress. Temporal provides automatic recovery, configurable retries, and workflow visibility.

### Goals

- Per-turn LLM activities and per-call tool activities in Temporal workflows
- Real-time token streaming via NATS pub/sub
- Human-in-the-loop via Temporal signals
- Per-agent task queues (`agent-{name}`)
- Per-agent CRD spec control (`Agent.spec.temporal`)
- Self-hosted Temporal: SQLite for dev, PostgreSQL for prod
- 48h default workflow timeout with configurable retry policies

### Non-Goals

- Multi-cluster Temporal deployment
- Temporal Cloud integration (self-hosted only)
- Custom Temporal UI (separate EP-2007)

## Implementation Details

- **CRD:** `TemporalSpec` in `go/api/v1alpha2/agent_types.go` — `enabled`, `workflowTimeout`, `retryPolicy`
- **Worker:** In-process alongside A2A server in agent pod
- **Streaming:** NATS fire-and-forget pub/sub for token streaming
- **Translator:** Injects `TEMPORAL_HOST_ADDR` and `NATS_ADDR` env vars
- **Helm:** Temporal Server + NATS deployed via `helm-install-temporal` target

### Test Plan

- E2E tests: Temporal Server/NATS deployment, env var injection, workflow execution, crash recovery
- Configurable timeout and retry policy tests
