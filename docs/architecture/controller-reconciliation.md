# Controller Reconciliation Architecture

This document explains how kagent's Kubernetes controllers reconcile resources and share state.

## Overview

The kagent controller manager runs multiple controllers that share a single `kagentReconciler` instance:

```text
┌─────────────────────────────────────────────────────────────────┐
│                    Controller Manager                           │
│                                                                 │
│  ┌──────────────────┐  ┌──────────────────┐  ┌───────────────┐ │
│  │ AgentController  │  │ RemoteMCPServer  │  │ MCPServer     │ │
│  │                  │  │ Controller       │  │ Controller    │ │
│  └────────┬─────────┘  └────────┬─────────┘  └───────┬───────┘ │
│           │                     │                    │         │
│           └─────────────────────┼────────────────────┘         │
│                                 │                              │
│                                 ▼                              │
│                    ┌────────────────────────┐                  │
│                    │   kagentReconciler     │                  │
│                    │   (shared instance)    │                  │
│                    │                        │                  │
│                    │  - adkTranslator       │                  │
│                    │  - kube client         │                  │
│                    │  - dbClient            │                  │
│                    └────────────────────────┘                  │
│                                 │                              │
│                                 ▼                              │
│                    ┌────────────────────────┐                  │
│                    │      SQLite DB         │                  │
│                    └────────────────────────┘                  │
└─────────────────────────────────────────────────────────────────┘
```

## Concurrency Model

The reconciler uses database-level concurrency control instead of application-level locks:

**Atomic Upserts**: Database operations for storing agents and tool servers use SQL `INSERT ... ON CONFLICT DO UPDATE` semantics. This makes the operations idempotent and safe for concurrent execution.

**Transactions**: Tool refresh operations wrap multiple statements (delete all existing tools, insert new tools) in a database transaction to ensure atomicity.

**No Application Locks**: The reconciler does not use mutexes or other Go synchronization primitives. SQLite handles write serialization internally.

## Reconciliation Flows

### Agent Reconciliation

When an Agent CR is created or updated:

1. The `AgentController` receives the event
2. Delegates to the shared `kagentReconciler`
3. The reconciler translates the Agent spec into Kubernetes manifests (Deployment, ConfigMap, etc.)
4. Reconciles the desired state with the cluster (create/update/delete owned resources)
5. Stores the agent configuration in the SQLite database (atomic upsert)
6. Updates the Agent status

### RemoteMCPServer Reconciliation

When a RemoteMCPServer CR is created or updated:

1. The RemoteMCPServer controller receives the event
2. Stores the tool server metadata in the database (atomic upsert)
3. Connects to the remote MCP server over the network
4. Lists available tools from the server
5. Replaces all tools for this server in the database (transaction)
6. Updates the RemoteMCPServer status with discovered tools

### Key Design Point

Network I/O (connecting to remote MCP servers, listing tools) happens **outside** of database transactions. This prevents long-running network operations from holding database locks and blocking other reconciliations.

## Event Filtering

The `AgentController` uses a custom event predicate to control which Kubernetes events trigger reconciliation:

- **Create events**: Always processed (ensures all agents reconcile on controller startup)
- **Delete events**: Always processed
- **Update events**: Only processed if the agent's generation or labels changed

This filtering prevents unnecessary reconciliations when only the agent's status changes.

## Related Files

- [reconciler.go](../../go/internal/controller/reconciler/reconciler.go) - Shared reconciler implementation
- [agent_controller.go](../../go/internal/controller/agent_controller.go) - Agent controller setup
- [service.go](../../go/internal/database/service.go) - Database helpers with atomic upserts
- [client.go](../../go/internal/database/client.go) - Database client implementation
