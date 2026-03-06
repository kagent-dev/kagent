# Temporal Go SDK for Agent Workflows

## Core Concepts

| Concept | Description | Agent Mapping |
|---------|-------------|---------------|
| **Workflow** | Durable, deterministic orchestration function | Agent execution run |
| **Activity** | Non-deterministic I/O work (retryable) | LLM calls, tool execution, persistence |
| **Worker** | Process executing workflow/activity code | Agent pod |
| **Task Queue** | Decoupling between clients and workers | Per-agent or shared queue |
| **Signal** | Async message to running workflow | HITL approval, user input |
| **Query** | Sync read-only state inspection | Execution status check |
| **Child Workflow** | Hierarchical composition | Multi-agent orchestration (A2A) |
| **Saga** | Distributed transactions with compensation | Rollback on multi-step failures |

## Key Temporal Properties

- **Event sourcing**: Every workflow decision logged to persistent store
- **Deterministic replay**: Workflow re-executed with logged events on recovery
- **Activity retry**: Built-in exponential backoff, deadletter, heartbeat
- **Workflow isolation**: Survives crashes, network partitions, code upgrades
- **Versioning**: Safe code evolution without breaking running workflows

## Deployment on Kubernetes

### Self-Hosted (temporal-helm)

```
Temporal Server (3+ replicas)
  |-- Frontend Service (gRPC :7233)
  |-- History Service
  |-- Matching Service
  |-- Worker Service
  |-- PostgreSQL/MySQL backend
  |-- Temporal UI (:8080)
```

### Temporal Cloud

- Managed SaaS by Temporal Inc.
- mTLS + API key auth
- No infrastructure overhead
- Pay-per-workflow pricing

## OpenTelemetry Integration

Temporal Go SDK provides `go.temporal.io/sdk/contrib/opentelemetry`:

- Auto-instrumented spans for workflow/activity execution
- Workflow type, ID, activity type, attempt count as attributes
- Integrates with existing OTel provider via `MetricsHandler`
- Custom interceptors can inject kagent-specific attributes

## Go SDK Dependencies

```
go.temporal.io/sdk v1.x
go.temporal.io/api v1.x
go.temporal.io/sdk/contrib/opentelemetry (optional)
```

## Retry Policies

```go
RetryPolicy{
    InitialInterval:    1 * time.Second,
    MaximumInterval:    1 * time.Minute,
    MaximumAttempts:    3,
    BackoffCoefficient: 2.0,
    NonRetryableErrors: []string{"InvalidArgument"},
}
```

## Risks & Mitigations

| Risk | Mitigation |
|------|------------|
| Workflow determinism violations | Lint rules, replay tests, design reviews |
| Temporal server downtime | Multi-replica HA, PostgreSQL persistence, Temporal Cloud |
| Large message payloads | External storage, compression, streaming |
| Cold start latency | Worker warmup, pre-allocated K8s pods |
| Version skew | Workflow versioning API, gradual rollout |
