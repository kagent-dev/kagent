# Controller Reconciliation Architecture

This document explains how kagent's Kubernetes controllers reconcile resources and share state.

## Overview

The kagent controller manager runs multiple controllers that share a common reconciler instance:

```
┌─────────────────────────────────────────────────────────────────┐
│                    Controller Manager                            │
│                                                                  │
│  ┌──────────────────┐  ┌──────────────────┐  ┌────────────────┐ │
│  │ AgentController  │  │ RemoteMCPServer  │  │ MCPServer      │ │
│  │                  │  │ Controller       │  │ Controller     │ │
│  └────────┬─────────┘  └────────┬─────────┘  └───────┬────────┘ │
│           │                     │                     │          │
│           └─────────────────────┼─────────────────────┘          │
│                                 │                                │
│                                 ▼                                │
│                    ┌────────────────────────┐                    │
│                    │   kagentReconciler     │                    │
│                    │   (shared instance)    │                    │
│                    │                        │                    │
│                    │  - adkTranslator       │                    │
│                    │  - kube client         │                    │
│                    │  - dbClient            │                    │
│                    │  - upsertLock (mutex)  │◄── shared lock     │
│                    └────────────────────────┘                    │
│                                 │                                │
│                                 ▼                                │
│                    ┌────────────────────────┐                    │
│                    │      SQLite DB         │                    │
│                    │   (agents, tools)      │                    │
│                    └────────────────────────┘                    │
└─────────────────────────────────────────────────────────────────┘
```

## Reconciliation Flow

### Agent Reconciliation

```
Agent CR Created/Updated
         │
         ▼
┌─────────────────────────┐
│  ReconcileKagentAgent   │
│                         │
│  1. Get Agent from API  │
│  2. reconcileAgent()    │
│  3. Update status       │
└───────────┬─────────────┘
            │
            ▼
┌─────────────────────────┐
│    reconcileAgent()     │
│                         │
│  1. TranslateAgent      │  ◄── Convert CR to deployment manifests
│  2. FindOwnedObjects    │
│  3. reconcileDesired    │  ◄── Create/update k8s resources
│  4. upsertAgent()       │
└───────────┬─────────────┘
            │
            ▼
┌─────────────────────────┐
│     upsertAgent()       │
│                         │
│  LOCK ──────────────┐   │
│  │ StoreAgent(db)   │   │  ◄── Fast DB operation
│  UNLOCK ────────────┘   │
└─────────────────────────┘
```

### RemoteMCPServer Reconciliation

```
RemoteMCPServer CR Created/Updated
         │
         ▼
┌─────────────────────────────────┐
│  ReconcileKagentRemoteMCPServer │
│                                 │
│  1. Get server from API         │
│  2. upsertToolServerFor...()    │
│  3. Update status               │
└───────────┬─────────────────────┘
            │
            ▼
┌─────────────────────────────────┐
│ upsertToolServerForRemoteMCP... │
│                                 │
│  LOCK ──────────────┐           │
│  │ StoreToolServer  │           │  ◄── Fast DB write
│  UNLOCK ────────────┘           │
│                                 │
│  createMcpTransport()           │  ◄── Network: connect to MCP server
│  listTools()                    │  ◄── Network: fetch tool list (SLOW)
│                                 │
│  LOCK ──────────────┐           │
│  │ RefreshTools     │           │  ◄── Fast DB write
│  UNLOCK ────────────┘           │
└─────────────────────────────────┘
```

## The Mutex Problem (Before Fix)

The original implementation held the lock during the entire `upsertToolServerForRemoteMCPServer` operation:

```
                    Time ──────────────────────────────────────────►

Goroutine A         ┌─────────────────────────────────────────────┐
(RemoteMCPServer)   │ LOCK HELD                                   │
                    │ StoreToolServer │ createTransport │ listTools│ RefreshTools │
                    └─────────────────────────────────────────────┘
                                      ▲
                                      │ Network I/O (seconds)
                                      │
Goroutine B         ────────────BLOCKED────────────────────────────
(Agent)                         waiting for lock...
                                      │
                                      ▼
                              Agent not reconciled!
```

**Impact**: If an MCP server was slow or unreachable, ALL agent reconciliations were blocked.

## The Fix

Split the lock into two fast critical sections, allowing network I/O to happen without holding the lock:

```
                    Time ──────────────────────────────────────────►

Goroutine A         ┌────┐                              ┌─────────┐
(RemoteMCPServer)   │LOCK│  Network I/O (no lock)       │  LOCK   │
                    │ DB │  createTransport, listTools  │  DB     │
                    └────┘                              └─────────┘
                         │                              │
                         │   Lock available here!       │
                         ▼                              ▼
Goroutine B              ┌────────────────────────┐
(Agent)                  │ LOCK - upsertAgent     │
                         │ (runs while A does I/O)│
                         └────────────────────────┘
```

**Result**: Agent reconciliations can proceed while RemoteMCPServer is waiting on network I/O.

## Key Design Decisions

### Why a Shared Reconciler?

All controllers need access to:
- The same database client (SQLite, single-writer)
- The same Kubernetes client
- The same ADK translator

Sharing a reconciler instance ensures consistent state and simplifies dependency injection.

### Why a Mutex at All?

The SQLite database client requires serialized writes. The mutex ensures:
1. No concurrent writes corrupt the database
2. Read-after-write consistency for related operations

### Why Not Use Database Transactions Instead?

The current `dbClient` abstraction doesn't expose transaction boundaries. The mutex is a pragmatic solution that works with the existing interface. A future refactor could move to explicit transactions.

## Related Files

- `go/internal/controller/agent_controller.go` - Agent controller setup
- `go/internal/controller/remotemcpserver_controller.go` - RemoteMCPServer controller setup
- `go/internal/controller/reconciler/reconciler.go` - Shared reconciler implementation
- `go/internal/database/client.go` - Database client interface
