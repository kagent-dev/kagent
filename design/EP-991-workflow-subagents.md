# EP-991: Declarative Workflow Subagents

- Issue: [#991](https://github.com/kagent-dev/kagent/issues/991)

## Background

Google ADK provides three workflow agent primitives — `SequentialAgent`, `ParallelAgent`, and `LoopAgent` — that deterministically orchestrate in-process sub-agents. kagent currently supports agent-to-agent delegation via A2A tool references, but this is LLM-driven (the model decides when and whether to call sub-agents). There is no way to declare deterministic multi-agent workflows in YAML.

This EP adds a `workflow` field to the `DeclarativeAgentSpec` CRD that lets users declare Sequential, Parallel, and Loop orchestration patterns. Sub-agents are defined inline within the parent agent's CRD and run in-process within the same pod, sharing session state.

## Motivation

Users building multi-agent systems need deterministic orchestration patterns:
- **Sequential**: Run agents in a fixed order (e.g., writer → editor → publisher)
- **Parallel**: Run agents concurrently and merge results (e.g., research multiple topics simultaneously)
- **Loop**: Iterate until a condition is met (e.g., write → critique → refine cycles)

Today, users must either rely on the LLM to coordinate agents (non-deterministic) or write custom BYO agents in code. Declarative workflow support brings these patterns to YAML-only users and ensures reliable execution order.

### Goals

1. Support `Sequential`, `Parallel`, and `Loop` workflow types via CRD configuration
2. Sub-agents run in-process within a single pod, sharing session state
3. Each sub-agent can have its own system message, model config, and MCP tools
4. Loop workflows support `maxIterations` and exit-on-escalation
5. Both Python and Go runtimes support workflow agents

### Non-Goals

1. Remote sub-agents (separate pods communicating via A2A within a workflow)
2. Nested workflows (a sub-agent that is itself a workflow)
3. Conditional branching or DAG-based orchestration beyond what ADK provides
4. UI visualization of workflow topology

## Implementation

### 1. CRD Types (`go/api/v1alpha2/agent_types.go`)

New types added to the agent CRD:

```go
// +kubebuilder:validation:Enum=Sequential;Parallel;Loop
type WorkflowType string

type WorkflowSpec struct {
    Type          WorkflowType     `json:"type"`
    SubAgents     []InlineAgentSpec `json:"subAgents"`
    MaxIterations *int             `json:"maxIterations,omitempty"` // Loop only
}

type InlineAgentSpec struct {
    Name          string  `json:"name"`
    Description   string  `json:"description,omitempty"`
    SystemMessage string  `json:"systemMessage"`
    ModelConfig   string  `json:"modelConfig,omitempty"` // inherits parent if unset
    Tools         []*Tool `json:"tools,omitempty"`       // MCP tools only
}
```

The `Workflow` field is added to `DeclarativeAgentSpec` with CEL validation rules:
- `workflow` is mutually exclusive with `systemMessage`, `systemMessageFrom`, and `tools`
- `maxIterations` is only valid for `Loop` type

### 2. ADK Config Types (`go/api/adk/types.go`)

JSON-serializable types passed to both Python and Go runtimes:

```go
type WorkflowAgentConfig struct {
    Type          string           `json:"type"` // "sequential", "parallel", "loop"
    SubAgents     []SubAgentConfig `json:"sub_agents"`
    MaxIterations *int             `json:"max_iterations,omitempty"`
}

type SubAgentConfig struct {
    Name        string                `json:"name"`
    Description string                `json:"description,omitempty"`
    Instruction string                `json:"instruction"`
    Model       Model                 `json:"model"`
    HttpTools   []HttpMcpServerConfig `json:"http_tools,omitempty"`
    SseTools    []SseMcpServerConfig  `json:"sse_tools,omitempty"`
}
```

The `AgentConfig` struct gets a new `Workflow *WorkflowAgentConfig` field.

### 3. Translator Changes

The translator's `translateInlineAgent` method branches when `Workflow` is set, calling a new `translateWorkflowAgent` method that:

1. Resolves the default model config (used by sub-agents without their own)
2. For each sub-agent: resolves its model (own or inherited), translates MCP tools
3. Returns an `AgentConfig` with the `Workflow` field populated

The `translateMCPServerTarget` method is refactored to support writing tool configs to `SubAgentConfig` in addition to `AgentConfig`.

Validation rules enforced by the translator:
- Sub-agent names must be unique within a workflow
- Agent-as-tool references are not allowed within workflow sub-agents
- `maxIterations` only meaningful for Loop type

### 4. Python Runtime (`python/packages/kagent-adk/src/kagent/adk/types.py`)

The `AgentConfig.to_agent()` method is refactored:
- Existing logic moves to `_build_llm_agent()`
- New `_build_workflow_agent()` constructs in-process sub-agents and wraps them in the appropriate ADK workflow agent

```python
from google.adk.agents import SequentialAgent, ParallelAgent, LoopAgent

def _build_workflow_agent(self, name, sts_integration):
    sub_agents = [self._build_sub_agent(cfg, sts_integration) for cfg in self.workflow.sub_agents]
    match self.workflow.type:
        case "sequential": return SequentialAgent(name=name, sub_agents=sub_agents, ...)
        case "parallel":   return ParallelAgent(name=name, sub_agents=sub_agents, ...)
        case "loop":       return LoopAgent(name=name, sub_agents=sub_agents, max_iterations=...)
```

### 5. Go Runtime (`go/adk/pkg/agent/agent.go`)

New `CreateWorkflowAgent()` function creates sub-agents via `llmagent.New()` and wraps them in the appropriate workflow agent from `google.golang.org/adk/agent/workflowagents/`.

The runner adapter (`go/adk/pkg/runner/adapter.go`) routes to `CreateWorkflowAgent()` when `agentConfig.Workflow != nil`.

### Example: Sequential Workflow

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: writer-critic
spec:
  type: Declarative
  description: Generates content then reviews it
  declarative:
    runtime: python
    modelConfig: default-model-config
    workflow:
      type: Sequential
      subAgents:
        - name: writer
          description: Writes creative content
          systemMessage: |
            You are a creative writer. Write a compelling paragraph about the given topic.
        - name: critic
          description: Reviews and improves content
          systemMessage: |
            You are a writing critic. Review the previous content and provide improvements.
```

### Example: Loop Workflow

```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: iterative-refiner
spec:
  type: Declarative
  description: Iteratively refines content through write-critique cycles
  declarative:
    modelConfig: default-model-config
    workflow:
      type: Loop
      maxIterations: 5
      subAgents:
        - name: writer
          systemMessage: Write or refine content based on feedback.
        - name: critic
          systemMessage: Critique the content. If satisfactory, escalate to stop the loop.
```

### Test Plan

1. **Translator golden tests**: Input YAML + expected output JSON for sequential, parallel, and loop workflows
2. **Python unit tests**: Verify `to_agent()` returns correct workflow agent type with correct sub-agent count and configuration
3. **Go unit tests**: Verify `CreateWorkflowAgent()` for all three workflow types
4. **CRD validation**: Verify CEL rules reject invalid combinations (workflow + systemMessage, maxIterations on non-Loop)

## Alternatives

**Remote sub-agents via A2A**: Each sub-agent as a separate Agent CR and pod. Rejected because ADK workflow agents require in-process sub-agents sharing session state. Remote A2A adds network latency and breaks session state sharing. The existing agent-as-tool pattern already covers the remote case.

**Workflow as a separate CRD**: A dedicated `Workflow` resource type that references Agent CRs. Rejected for the same reason — ADK workflow agents need in-process sub-agents, not separate pods.

## Open Questions

1. Should sub-agents within a workflow be allowed to reference remote agents (other Agent CRs) as tools? Currently prohibited for simplicity; could be added later since the pod already has network access.
2. Should nested workflows (a sub-agent that is itself a workflow) be supported? Deferred to a future EP.
