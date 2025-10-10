# EP-991: Workflow Subagents with ADK Patterns

**Status**: Implemented ✅  
**Issue**: [#991](https://github.com/kagent-dev/kagent/issues/991)  
**Version**: v0.7.0+

## Summary

This enhancement adds first-class support for **workflow agents** to KAgent, enabling declarative multi-agent orchestration through three workflow patterns: **SequentialAgent**, **ParallelAgent**, and **LoopAgent**. These workflow types align with Google's Agent Development Kit (ADK) and allow users to compose existing agents into complex, reusable workflows without writing custom orchestration code.

## Background

KAgent currently supports two agent types:
- **Declarative agents**: LLM-powered agents with tools and system messages
- **BYO (Bring Your Own) agents**: Custom container-based agents

While these agent types are powerful individually, orchestrating multiple agents to solve complex problems requires either custom code or manual coordination. Users need a declarative way to define agent workflows that execute in deterministic patterns.

Google's Agent Development Kit (ADK) provides three workflow agent types for multi-agent orchestration:
- **SequentialAgent**: Executes sub-agents in order with context propagation
- **ParallelAgent**: Executes sub-agents concurrently with isolated contexts
- **LoopAgent**: Executes sub-agents iteratively with configurable termination

By bringing these workflow patterns to KAgent as a native CRD feature, we enable:
1. Declarative workflow definition in YAML
2. Reusable agent composition
3. Kubernetes-native orchestration
4. Consistency with the ADK ecosystem

## Motivation

### User Problems

Platform operators and agent developers face several challenges:

1. **Complex orchestration requires custom code**: Building multi-step agent workflows requires writing and maintaining custom orchestration logic
2. **No reusability**: Agent compositions cannot be shared or reused across teams
3. **Lack of patterns**: Common patterns (sequential, parallel, iterative) must be reimplemented for each use case
4. **Limited observability**: Custom orchestration lacks standardized metrics and tracing
5. **ADK ecosystem gap**: KAgent supports ADK agents but not ADK workflow patterns

### Goals

1. **Enable declarative workflow definition**: Users can define workflow agents using Kubernetes YAML manifests
2. **Support ADK workflow patterns**: Implement SequentialAgent, ParallelAgent, and LoopAgent aligned with Google ADK
3. **Maintain backward compatibility**: Existing Declarative and BYO agents continue to work unchanged
4. **Provide resource control**: Limit concurrent execution in ParallelAgent via `maxWorkers` field (default: 10)
5. **Enable workflow composition**: Workflows can reference other workflows (nesting support)
6. **Ensure observability**: Workflow execution includes OpenTelemetry tracing and Prometheus metrics
7. **Validate at creation time**: Detect circular dependencies and invalid references during agent creation
8. **Handle failures gracefully**: Sub-agent failures are captured as error events; execution continues

### Non-Goals

1. **Dynamic workflow modification**: Workflows cannot be modified during execution (declarative only)
2. **Complex conditional logic**: No if/else branching (use LoopAgent with exit_loop tool instead)
3. **Cross-cluster orchestration**: Sub-agents must be in the same Kubernetes cluster
4. **State persistence**: Workflow state is ephemeral (not persisted between invocations)
5. **Advanced scheduling**: No support for cron-like scheduling or triggers (use external schedulers)
6. **Custom workflow types**: Only SequentialAgent, ParallelAgent, and LoopAgent are supported

## Implementation Details

### Architecture

Workflow agents are implemented across three layers:

1. **CRD Layer (Go)**: Extended `AgentSpec` with `WorkflowAgentSpec`, validation, and CRD generation
2. **Controller Layer (Go)**: Workflow validation, circular dependency detection, and ADK API translation
3. **Runtime Layer (Python)**: ParallelAgent implementation with semaphore-based concurrency limiting, metrics, and tracing

### CRD Schema

The `AgentSpec` CRD is extended with a new `Workflow` field and `Workflow` agent type:

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: example-workflow
  namespace: kagent
spec:
  type: Workflow  # New type
  workflow:
    # Exactly one of: sequential, parallel, or loop
    sequential:
      description: "Execute agents in sequence"
      timeout: "5m"
      subAgents:
        - name: agent-1
        - name: agent-2
    
    parallel:
      description: "Execute agents concurrently"
      maxWorkers: 10  # Limit concurrent execution
      timeout: "5m"
      subAgents:
        - name: agent-a
        - name: agent-b
    
    loop:
      description: "Execute agents iteratively"
      maxIterations: 5
      timeout: "5m"
      subAgents:
        - name: agent-x
```

### Key Components

#### 1. WorkflowAgentSpec

Discriminated union containing exactly one workflow type:

```go
type WorkflowAgentSpec struct {
    Sequential *SequentialAgentSpec `json:"sequential,omitempty"`
    Parallel   *ParallelAgentSpec   `json:"parallel,omitempty"`
    Loop       *LoopAgentSpec       `json:"loop,omitempty"`
}
```

#### 2. SubAgentReference

References existing Agent CRDs:

```go
type SubAgentReference struct {
    Name      string `json:"name"`      // Agent name (required)
    Namespace string `json:"namespace"` // Defaults to parent namespace
    Kind      string `json:"kind"`      // Must be "Agent"
}
```

#### 3. ParallelAgentSpec with MaxWorkers

Controls resource usage via concurrency limiting:

```go
type ParallelAgentSpec struct {
    BaseWorkflowSpec `json:",inline"`
    MaxWorkers *int32 `json:"maxWorkers,omitempty"` // Default: 10, Min: 1, Max: 50
}
```

**Implementation**: Python runtime uses `asyncio.Semaphore` to limit concurrent sub-agent execution. When `maxWorkers=5` with 20 sub-agents:
- First 5 sub-agents start immediately
- Remaining 15 wait in queue
- As each sub-agent completes, the next queued agent starts
- Never more than 5 concurrent executions

#### 4. Validation

**Creation-Time Validation** (Admission Webhook):
- Sub-agent references must resolve to existing Agents
- Circular dependency detection using DFS (Depth-First Search)
- Circular dependencies rejected for Sequential and Parallel agents
- Circular dependencies allowed for Loop agents (intentional repetition)
- Timeout format validation (e.g., "5m", "300s")
- Sub-agent count limits (min 2 for Parallel, max 50 for all)

**Runtime Validation** (Controller):
- Sub-agents must be Ready before execution
- RBAC permissions verified
- Timeout enforcement per sub-agent

### Behavior Specifications

#### SequentialAgent

**Context Flow**: InvocationContext flows sequentially through sub-agents. Each agent can modify context, and changes are visible to subsequent agents.

```
User Request → Agent[0] → modifies context
                         ↓
            Agent[1] → sees context changes
                         ↓
            Agent[N] → final result
```

**Error Handling**: Sub-agent failures are captured as error events; execution continues with remaining sub-agents.

#### ParallelAgent

**Context Flow**: Each sub-agent receives an independent copy of InvocationContext. Context modifications are isolated (no shared state). Events from all sub-agents are merged and returned.

```
User Request → ParallelAgent
                  ↓ (fork)
                  ├→ Agent[0] (isolated context)
                  ├→ Agent[1] (isolated context)
                  └→ Agent[N] (isolated context)
                  ↓ (join)
              Merged Events → Upstream
```

**Concurrency Control**: `asyncio.Semaphore` with limit=`maxWorkers` (default: 10). Semaphore acquire/release events traced via OpenTelemetry.

**Error Handling**: Sub-agent failures don't stop other concurrent sub-agents; all errors are collected.

#### LoopAgent

**Context Flow**: InvocationContext persists across iterations and accumulates state. Current iteration number is available in context.

```
User Request → Loop Iteration 1
                  ↓
              Agent[0..N] → modify context
                  ↓
              Check: iteration < maxIterations AND !exit_loop
                  ↓ YES
              Loop Iteration 2 (context accumulates)
                  ↓
              Agent[0..N]
                  ↓ NO (max reached or exit_loop)
              Final Result
```

**Termination**: Loop terminates when:
- `maxIterations` is reached, OR
- A sub-agent calls the `exit_loop` tool (from `google.adk.tools`)

**Error Handling**: Failures in any iteration are captured as error events; loop continues.

### Implementation Status

**Completed** ✅:
- CRD schema extension with Workflow types
- Go validation (circular dependency detection, reference resolution)
- Python ParallelAgent runtime with semaphore pattern
- Prometheus metrics for concurrency tracking
- OpenTelemetry tracing integration
- Comprehensive test coverage (Go + Python)
- UI support for workflow agent creation
- Documentation (data-model.md, quickstart.md)

**Deferred** ⏸️:
- Full SequentialAgent and LoopAgent Python runtime (only ParallelAgent is fully implemented)
- Cluster deployment and e2e validation

See [`specs/001-add-workflow-agents/IMPLEMENTATION_STATUS.md`](../specs/001-add-workflow-agents/IMPLEMENTATION_STATUS.md) for detailed status.

### Test Plan

#### Unit Tests (Go)

**Location**: `go/api/v1alpha2/agent_types_test.go`, `go/internal/controller/translator/agent/workflow_validator_test.go`

**Coverage**:
- CRD validation rules (maxWorkers min/max, timeout format)
- Circular dependency detection (Sequential, Parallel reject cycles; Loop allows)
- SubAgentReference resolution
- Workflow type mutual exclusion

**Example**:
```go
func TestParallelAgentSpec_MaxWorkers(t *testing.T) {
    // Valid: maxWorkers=5
    // Valid: maxWorkers=nil (defaults to 10)
    // Invalid: maxWorkers=0 (below minimum)
    // Invalid: maxWorkers=51 (above maximum)
}
```

#### Integration Tests (Python)

**Location**: `python/packages/kagent-adk/tests/test_parallel_agent.py`

**Coverage**:
- Concurrency limiting with semaphore (20 sub-agents, maxWorkers=5)
- Context isolation between parallel sub-agents
- Error handling (sub-agent failures don't stop others)
- Metrics emission (queue depth, active executions)
- Timeout enforcement

**Example**:
```python
@pytest.mark.asyncio
async def test_parallel_agent_max_workers_limits_concurrency():
    # Create 20 sub-agents with maxWorkers=5
    # Verify max 5 concurrent executions
    # Verify remaining 15 queue and execute as slots free
```

#### End-to-End Tests (Deferred)

**Location**: `specs/001-add-workflow-agents/quickstart.md`

**Scenarios**:
1. Sequential workflow: Upgrade application (pre-check → helm upgrade → validation)
2. Parallel workflow: Multi-layer diagnostics (K8s + Istio + Helm in parallel)
3. Parallel with maxWorkers: 20 namespace scans with maxWorkers=5
4. Loop workflow: SRE team (iterative fixes with exit_loop)
5. Nested workflow: Incident response (sequential → parallel → loop)
6. Timeout enforcement
7. Error handling (sub-agent failures)
8. Circular dependency rejection

### Observability

#### OpenTelemetry Tracing

**Spans**:
- `workflow.execute` (root span for workflow execution)
- `workflow.sub_agent.execute` (span per sub-agent)
- `workflow.parallel.semaphore.acquire` (wait time for concurrency slot)
- `workflow.parallel.semaphore.release` (slot released)

**Attributes**:
- `workflow.type`: "sequential" | "parallel" | "loop"
- `workflow.sub_agent.name`: Sub-agent name
- `workflow.sub_agent.index`: Execution order (sequential)
- `workflow.iteration`: Current iteration (loop)
- `workflow.parallel.max_workers`: Concurrency limit
- `workflow.parallel.queue_depth`: Sub-agents waiting for slot

#### Prometheus Metrics

**Location**: `python/packages/kagent-adk/src/kagent/adk/metrics.py`

**Metrics**:
```python
# Queue depth (sub-agents waiting for semaphore slot)
kagent_parallel_queue_depth{agent_name, namespace}

# Active executions (currently running sub-agents)
kagent_parallel_active_executions{agent_name, namespace}

# Sub-agent execution duration
kagent_workflow_sub_agent_duration_seconds{workflow_type, agent_name}

# Workflow execution duration
kagent_workflow_duration_seconds{workflow_type, agent_name}
```

### Files Changed

**Go (CRD/Controller)**:
- `go/api/v1alpha2/agent_types.go` - WorkflowAgentSpec, SubAgentReference
- `go/api/v1alpha2/agent_types_test.go` - CRD validation tests
- `go/internal/controller/translator/agent/workflow_validator.go` - Circular dependency detection
- `go/internal/controller/translator/agent/adk_api_translator.go` - Workflow translation
- `go/config/crd/bases/kagent.dev_agents.yaml` - Generated CRD manifest

**Python (Runtime)**:
- `python/packages/kagent-adk/src/kagent/adk/agents/parallel.py` - ParallelAgent with semaphore
- `python/packages/kagent-adk/src/kagent/adk/metrics.py` - Prometheus metrics
- `python/packages/kagent-adk/src/kagent/adk/types.py` - Type definitions
- `python/packages/kagent-adk/tests/test_parallel_agent.py` - Integration tests

**UI**:
- `ui/src/components/create/WorkflowSection.tsx` - Workflow agent creation UI
- `ui/src/app/agents/new/page.tsx` - Agent creation page updates

**Documentation**:
- `design/EP-991-workflow-subagents.md` - Feature specification

## Alternatives

NA - current implementation is SDK / ADK 

## Open Questions

~~1. **Should maxWorkers be configurable per sub-agent or globally?**~~ → Resolved: Global per ParallelAgent (simpler, aligns with ADK)

~~2. **How should circular dependencies in nested workflows be handled?**~~ → Resolved: DFS validation at creation time

~~3. **Should SequentialAgent support early termination (like LoopAgent's exit_loop)?**~~ → Deferred to future enhancement

~~4. **Should we support cross-namespace sub-agent references?**~~ → Resolved: Yes, with explicit namespace field (subject to RBAC)

## References

### External Documentation

- [ADK Workflow Agents Documentation](https://github.com/google/adk-docs/blob/main/docs/agents/workflow-agents/)
- [ADK Sequential Agents](https://github.com/google/adk-docs/blob/main/docs/agents/workflow-agents/sequential-agents.md)
- [ADK Parallel Agents](https://github.com/google/adk-docs/blob/main/docs/agents/workflow-agents/parallel-agents.md)
- [ADK Loop Agents](https://github.com/google/adk-docs/blob/main/docs/agents/workflow-agents/loop-agents.md)
- [A2A Protocol](https://github.com/google/A2A)
- [ADK Tools - Controlling Agent Flow](https://google.github.io/adk-docs/tools/#controlling-agent-flow)

### Internal Implementation

- **Feature Specification**: [`specs/001-add-workflow-agents/spec.md`](../specs/001-add-workflow-agents/spec.md) - Detailed feature requirements and acceptance criteria
- **Data Model**: [`specs/001-add-workflow-agents/data-model.md`](../specs/001-add-workflow-agents/data-model.md) - Entity definitions, validation rules, and data flow
- **Quickstart Guide**: [`specs/001-add-workflow-agents/quickstart.md`](../specs/001-add-workflow-agents/quickstart.md) - Step-by-step user guide with examples
- **Implementation Status**: [`specs/001-add-workflow-agents/IMPLEMENTATION_STATUS.md`](../specs/001-add-workflow-agents/IMPLEMENTATION_STATUS.md) - Current implementation status and blockers
- **Implementation Plan**: [`specs/001-add-workflow-agents/plan.md`](../specs/001-add-workflow-agents/plan.md) - Technical implementation planning

### Key Source Files

**Go (CRD/Controller)**:
- [`go/api/v1alpha2/agent_types.go`](../go/api/v1alpha2/agent_types.go) - WorkflowAgentSpec type definitions
- [`go/internal/controller/translator/agent/workflow_validator.go`](../go/internal/controller/translator/agent/workflow_validator.go) - Circular dependency detection
- [`go/internal/controller/translator/agent/adk_api_translator.go`](../go/internal/controller/translator/agent/adk_api_translator.go) - Workflow translation logic

**Python (Runtime)**:
- [`python/packages/kagent-adk/src/kagent/adk/agents/parallel.py`](../python/packages/kagent-adk/src/kagent/adk/agents/parallel.py) - ParallelAgent implementation with semaphore
- [`python/packages/kagent-adk/src/kagent/adk/metrics.py`](../python/packages/kagent-adk/src/kagent/adk/metrics.py) - Prometheus metrics
- [`python/packages/kagent-adk/src/kagent/adk/types.py`](../python/packages/kagent-adk/src/kagent/adk/types.py) - Python type definitions

**UI**:
- [`ui/src/components/create/WorkflowSection.tsx`](../ui/src/components/create/WorkflowSection.tsx) - Workflow agent creation UI

---

**Document Version**: v1.0  
**Last Updated**: 2025-10-05  
**Authors**: KAgent Development Team  
**Status**: Implementation Complete (Partial - ParallelAgent fully implemented)

