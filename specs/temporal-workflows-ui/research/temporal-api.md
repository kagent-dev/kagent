# Temporal Workflow Listing API Research

## Current State in Kagent

### Existing Temporal Client (`go/adk/pkg/temporal/client.go`)

Methods available:
- `ExecuteAgent()` — starts workflow
- `GetWorkflowStatus()` — describes single workflow by ID
- `WaitForResult()` — blocks until completion
- `SignalApproval()` — HITL signal

**No `ListWorkflows()` method exists.**

### Workflow ID Pattern

```
agent-{agentName}-{sessionID}
```

Task queue: `agent-{agentName}`

### Workflow Statuses

- running, completed, failed, canceled, terminated, timed_out, continued_as_new

## Temporal SDK List API

The Go SDK client exposes:

```go
client.ListWorkflow(ctx, &workflowservice.ListWorkflowExecutionsRequest{
    Namespace: "kagent",
    Query:     "WorkflowType = 'AgentExecutionWorkflow' AND ExecutionStatus = 'Running'",
    PageSize:  20,
})
```

Supports visibility queries to filter by:
- WorkflowType, WorkflowId, ExecutionStatus
- StartTime, CloseTime
- Custom search attributes

## What's Missing for a Custom Workflows Page

1. `ListWorkflows()` method in kagent's temporal client wrapper
2. HTTP API endpoint (e.g., `GET /api/workflows`)
3. No database tracking of workflow IDs (only discovered via session ID)
4. No UI implementation

## Alternative: Stock Temporal UI

The stock Temporal UI (temporalio/ui:2.34.0) already provides:
- Workflow listing with filters
- Workflow detail with event history
- Signal sending
- Namespace browsing

It's already registered as a plugin via RemoteMCPServer CRD. When Temporal is enabled in Helm, it appears in the sidebar as "Temporal Workflows".

## Decision Point

Two approaches:
1. **Use stock Temporal UI as plugin** — already works, zero custom code, but generic (no kagent-specific context like agent names, session links)
2. **Custom workflows plugin** — kagent-aware UI showing agent names, status, links to chat sessions, integrated with kagent's data model
