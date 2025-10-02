# EP-001: Workflow Subagents with ADK Patterns

**Status**: Implementation  
**Issue**: https://github.com/kagent-dev/kagent/issues/991

## Background

This design introduces **subagents** with **workflow patterns** to kagent, enabling declarative multi-agent orchestration using ADK's `SequentialAgent`, `ParallelAgent`, and `LoopAgent`. Subagents are specialized agents that can be delegated tasks by a parent orchestrator agent. Workflow patterns define HOW subagents execute: sequentially (A → B → C), in parallel (A || B || C), or iteratively (loop with conditions).

**Reference**: [ADK Workflow Agents Documentation](https://github.com/google/adk-docs/blob/main/docs/agents/workflow-agents/)

## Motivation

### Current State
kagent currently supports individual agents that can use tools via MCP servers. However, there's no support for:
- **Agent-to-agent delegation**: One agent delegating work to another specialized agent
- **Subagent orchestration**: Parent agent coordinating multiple child agents
- **Workflow patterns**: Structured execution patterns (sequential, parallel, iterative)

This EP introduces both concepts together:
- ❌ No sequential task pipelines (A → B → C)
- ❌ No parallel execution (A || B || C)
- ❌ No iterative loops with conditions

### With Workflow Subagents
```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: research-workflow
spec:
  declarative:
    systemMessage: "Research and analysis pipeline"
    modelConfig: default-model-config
    
    # NEW: Workflow subagents with execution patterns
    subagents:
      - workflow:
          type: Sequential
          agents:
            - name: research-agent
            - name: analysis-agent
            - name: report-agent
```

### Goals

1. **Declarative Workflows**: Define SequentialAgent, ParallelAgent, LoopAgent patterns in YAML
2. **ADK Integration**: Leverage ADK's battle-tested workflow implementations
3. **Composability**: Allow nesting workflows (e.g., Sequential workflow containing Parallel steps)
4. **Backward Compatibility**: Existing subagents continue to work as before

### Non-Goals

- Custom workflow DSL (use ADK patterns only)
- Complex conditional logic (LangGraph/CrewAI territory)
- Workflow state persistence across restarts (Phase 1)

## Implementation Details

### 1. CRD Schema Changes

Update `Subagent` type to support both individual agents and workflow patterns:

```go
// go/api/v1alpha2/agent_types.go

type Subagent struct {
    // EITHER an individual agent reference
    // +optional
    Agent *TypedLocalReference `json:"agent,omitempty"`
    
    // OR a workflow definition
    // +optional
    Workflow *WorkflowSubagent `json:"workflow,omitempty"`
    
    // Common fields
    Role string `json:"role,omitempty"`
    Description string `json:"description,omitempty"`
    HeadersFrom []ValueRef `json:"headersFrom,omitempty"`
    DelegationMode *DelegationMode `json:"delegationMode,omitempty"`
}

// WorkflowSubagent defines a workflow pattern
// +kubebuilder:validation:XValidation:rule="self.type == 'Sequential' || self.type == 'Parallel' || self.type == 'Loop'",message="workflow type must be Sequential, Parallel, or Loop"
type WorkflowSubagent struct {
    // +kubebuilder:validation:Enum=Sequential;Parallel;Loop
    Type WorkflowType `json:"type"`
    
    // Agents to execute in the workflow
    // +kubebuilder:validation:MinItems=1
    Agents []WorkflowAgentRef `json:"agents"`
    
    // Loop-specific configuration
    // +optional
    LoopConfig *LoopConfig `json:"loopConfig,omitempty"`
}

// WorkflowType represents workflow execution patterns
// +kubebuilder:validation:Enum=Sequential;Parallel;Loop
type WorkflowType string

const (
    WorkflowType_Sequential WorkflowType = "Sequential"
    WorkflowType_Parallel   WorkflowType = "Parallel"
    WorkflowType_Loop       WorkflowType = "Loop"
)

// WorkflowAgentRef references an agent in a workflow
type WorkflowAgentRef struct {
    // Agent reference (namespace/name or name)
    Name string `json:"name"`
    
    // Optional description of this agent's role in the workflow
    // +optional
    Description string `json:"description,omitempty"`
}

// LoopConfig configures loop agent behavior
type LoopConfig struct {
    // Maximum iterations before terminating (hard limit)
    // +kubebuilder:validation:Minimum=1
    // +kubebuilder:validation:Maximum=100
    // +kubebuilder:default=5
    MaxIterations int `json:"maxIterations,omitempty"`
    
    // NOTE: Loops can also terminate early if a tool calls exit_loop() 
    // from google.adk.tools. This is handled in the Python runtime via
    // ADK's LoopAgent implementation, not in the CRD schema.
    //
    // Example tool that exits loop early:
    //   from google.adk.tools import exit_loop
    //   def check_quality(score: int):
    //       if score >= 80:
    //           exit_loop()  # Terminates loop before maxIterations
    
    // FUTURE: Could add declarative termination conditions
    // TerminationCondition string `json:"terminationCondition,omitempty"`
}
```

### 2. YAML Examples

#### Sequential Workflow
```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: code-development-pipeline
  namespace: kagent
spec:
  declarative:
    systemMessage: "Orchestrate code development workflow"
    modelConfig: default-model-config
    subagents:
      - role: "Development Pipeline"
        description: "Sequential code development process"
        workflow:
          type: Sequential
          agents:
            - name: code-writer
              description: "Generates initial code"
            - name: code-reviewer
              description: "Reviews code for errors"
            - name: code-refactorer
              description: "Refactors based on review"
```

**Maps to ADK**:
```python
SequentialAgent(sub_agents=[CodeWriterAgent, CodeReviewerAgent, CodeRefactorerAgent])
```

#### Parallel Workflow
```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: parallel-research
  namespace: kagent
spec:
  declarative:
    systemMessage: "Coordinate parallel research"
    modelConfig: default-model-config
    subagents:
      - role: "Research Coordinator"
        description: "Parallel research across domains"
        workflow:
          type: Parallel
          agents:
            - name: renewable-energy-researcher
              description: "Researches renewable energy"
            - name: ev-technology-researcher
              description: "Researches electric vehicles"
            - name: carbon-capture-researcher
              description: "Researches carbon capture"
```

**Maps to ADK**:
```python
ParallelAgent(sub_agents=[RenewableEnergyResearcher, EVTechnologyResearcher, CarbonCaptureResearcher])
```

#### Loop Workflow
```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: iterative-improvement
  namespace: kagent
spec:
  declarative:
    systemMessage: "Iteratively improve document quality"
    modelConfig: default-model-config
    subagents:
      - role: "Document Improver"
        description: "Iteratively refines documents"
        workflow:
          type: Loop
          agents:
            - name: writer-agent
              description: "Generates/refines drafts"
            - name: critic-agent
              description: "Critiques and suggests improvements"
          loopConfig:
            maxIterations: 5
```

**Maps to ADK**:
```python
LoopAgent(sub_agents=[WriterAgent, CriticAgent], max_iterations=5)
```

#### Nested Workflows
```yaml
apiVersion: kagent.dev/v1alpha2
kind: Agent
metadata:
  name: complex-workflow
  namespace: kagent
spec:
  declarative:
    systemMessage: "Complex nested workflow"
    modelConfig: default-model-config
    subagents:
      # First: Parallel research
      - workflow:
          type: Parallel
          agents:
            - name: research-agent-1
            - name: research-agent-2
      
      # Then: Sequential processing
      - workflow:
          type: Sequential
          agents:
            - name: synthesis-agent
            - name: analysis-agent
            - name: report-agent
```

### 3. Go Controller Changes

Update the ADK translator to convert workflow subagents:

```go
// go/internal/controller/translator/agent/adk_api_translator.go

func (a *adkApiTranslator) translateSubagents(ctx context.Context, agent *v1alpha2.Agent) error {
    for _, subagent := range agent.Spec.Declarative.Subagents {
        if subagent.Workflow != nil {
            // Translate workflow subagent
            err := a.translateWorkflowSubagent(ctx, agent, subagent)
            if err != nil {
                return err
            }
        } else if subagent.Agent != nil {
            // Translate individual subagent (existing logic)
            err := a.translateIndividualSubagent(ctx, agent, subagent)
            if err != nil {
                return err
            }
        }
    }
    return nil
}

func (a *adkApiTranslator) translateWorkflowSubagent(ctx context.Context, agent *v1alpha2.Agent, subagent *v1alpha2.Subagent) error {
    workflow := subagent.Workflow
    
    // Resolve all workflow agent references
    var subAgentConfigs []adk.SubagentConfig
    for _, wfAgent := range workflow.Agents {
        namespacedName := resolveNamespacedName(wfAgent.Name, agent.Namespace)
        
        agentObj := &v1alpha2.Agent{}
        err := a.kube.Get(ctx, namespacedName, agentObj)
        if err != nil {
            return fmt.Errorf("failed to get workflow agent %s: %v", namespacedName, err)
        }
        
        url := fmt.Sprintf("http://%s.%s:8080", agentObj.Name, agentObj.Namespace)
        
        subAgentConfigs = append(subAgentConfigs, adk.SubagentConfig{
            Name:        utils.ConvertToPythonIdentifier(utils.GetObjectRef(agentObj)),
            Url:         url,
            Description: wfAgent.Description,
        })
    }
    
    // Create workflow config based on type
    switch workflow.Type {
    case v1alpha2.WorkflowType_Sequential:
        cfg.WorkflowSubagents = append(cfg.WorkflowSubagents, adk.WorkflowConfig{
            Type:      "Sequential",
            Subagents: subAgentConfigs,
            Role:      subagent.Role,
        })
    
    case v1alpha2.WorkflowType_Parallel:
        cfg.WorkflowSubagents = append(cfg.WorkflowSubagents, adk.WorkflowConfig{
            Type:      "Parallel",
            Subagents: subAgentConfigs,
            Role:      subagent.Role,
        })
    
    case v1alpha2.WorkflowType_Loop:
        maxIter := 5 // default
        if workflow.LoopConfig != nil && workflow.LoopConfig.MaxIterations > 0 {
            maxIter = workflow.LoopConfig.MaxIterations
        }
        
        cfg.WorkflowSubagents = append(cfg.WorkflowSubagents, adk.WorkflowConfig{
            Type:          "Loop",
            Subagents:     subAgentConfigs,
            Role:          subagent.Role,
            MaxIterations: maxIter,
        })
    
    default:
        return fmt.Errorf("unknown workflow type: %s", workflow.Type)
    }
    
    return nil
}
```

### 4. Python ADK Integration

Update `AgentConfig` to support workflow subagents:

```python
# python/packages/kagent-adk/src/kagent/adk/types.py

from google.adk.agents.workflow_agents import SequentialAgent, ParallelAgent, LoopAgent

class WorkflowConfig(BaseModel):
    """Configuration for workflow subagents."""
    type: Literal["Sequential", "Parallel", "Loop"]
    subagents: list[SubagentConfig]
    role: str = ""
    max_iterations: int = 5  # For Loop agents

class AgentConfig(BaseModel):
    model: Union[OpenAI, Anthropic, ...] = Field(discriminator="type")
    description: str
    instruction: str
    http_tools: list[HttpMcpServerConfig] | None = None
    sse_tools: list[SseMcpServerConfig] | None = None
    remote_agents: list[RemoteAgentConfig] | None = None
    subagents: list[SubagentConfig] | None = None
    workflow_subagents: list[WorkflowConfig] | None = None  # NEW

    def to_agent(self, name: str) -> Agent:
        tools: list[ToolUnion] = []
        
        # Add MCP tools (existing)
        if self.http_tools:
            for http_tool in self.http_tools:
                tools.append(MCPToolset(connection_params=http_tool.params, tool_filter=http_tool.tools))
        
        # Add workflow subagents as tools
        if self.workflow_subagents:
            for workflow in self.workflow_subagents:
                # Create remote agents for each subagent in workflow
                workflow_agents = []
                for subagent in workflow.subagents:
                    client = None
                    if subagent.headers:
                        client = httpx.AsyncClient(
                            headers=subagent.headers,
                            timeout=httpx.Timeout(timeout=subagent.timeout)
                        )
                    
                    remote_agent = RemoteA2aAgent(
                        name=subagent.name,
                        agent_card=f"{subagent.url}/{AGENT_CARD_WELL_KNOWN_PATH}",
                        description=subagent.description,
                        httpx_client=client,
                    )
                    workflow_agents.append(remote_agent)
                
                # Create workflow agent based on type
                if workflow.type == "Sequential":
                    workflow_agent = SequentialAgent(
                        name=f"{name}_{workflow.role}_sequential",
                        sub_agents=workflow_agents
                    )
                elif workflow.type == "Parallel":
                    workflow_agent = ParallelAgent(
                        name=f"{name}_{workflow.role}_parallel",
                        sub_agents=workflow_agents
                    )
                elif workflow.type == "Loop":
                    workflow_agent = LoopAgent(
                        name=f"{name}_{workflow.role}_loop",
                        sub_agents=workflow_agents,
                        max_iterations=workflow.max_iterations
                    )
                    # Note: LoopAgent automatically handles exit_loop() calls from tools
                
                # Wrap workflow agent as a tool
                tools.append(AgentTool(agent=workflow_agent, skip_summarization=True))
        
        # ... rest of agent construction
        return Agent(
            name=name,
            model=model,
            description=self.description,
            instruction=self.instruction,
            tools=tools,
        )
```

### 5. Tool Context in Workflows

Workflow agents leverage ADK's Tool Context for state management and flow control.

**Reference**: [ADK Tools - Controlling Agent Flow](https://google.github.io/adk-docs/tools/#controlling-agent-flow)

#### State Management

Tools in workflow agents can access and modify shared state:

**Reading State**:
```python
def analyze_previous_results(tool_context: ToolContext) -> dict:
    """Analyzes results from previous workflow steps."""
    previous_results = tool_context.state.get("results", [])
    return {"analysis": f"Found {len(previous_results)} results"}
```

**Writing State**:
```python
def store_result(data: dict, tool_context: ToolContext) -> dict:
    """Stores result for next workflow step."""
    tool_context.state["current_result"] = data
    return {"status": "stored"}
```

**Appending to State** (accumulating results across steps):
```python
def collect_research(finding: str, tool_context: ToolContext) -> dict:
    """Collects research findings across workflow steps."""
    tool_context.append_to_state("findings", finding)
    # State now has findings: [finding1, finding2, ...]
    return {"collected": finding, "total": len(tool_context.state["findings"])}
```

#### Controlling Loop Flow

Tools can terminate loops early using `exit_loop()` from `google.adk.tools`:

```python
from google.adk.tools import exit_loop

def check_quality_threshold(
    document: str, 
    threshold: float, 
    tool_context: ToolContext
) -> dict:
    """Checks if document quality meets threshold. Exits loop if acceptable."""
    score = calculate_quality(document)
    
    tool_context.state["quality_score"] = score
    tool_context.append_to_state("score_history", score)
    
    if score >= threshold:
        exit_loop()  # Terminates loop immediately
        return {"status": "acceptable", "score": score}
    
    return {"status": "needs_improvement", "score": score}
```

**Important**: 
- `exit_loop()` only works within `LoopAgent` contexts
- Calling it outside a loop has no effect
- Loop terminates cleanly after the tool completes

#### State Lifecycle in Workflows

**Sequential Workflows**:
- State flows from Agent A → Agent B → Agent C
- Each agent can read/modify state via tools
- Use `append_to_state` to accumulate results across steps

**Parallel Workflows**:
- Each parallel agent gets a copy of initial state
- Results merged after all agents complete
- Use consistent state keys to avoid conflicts

**Loop Workflows**:
- State persists across iterations
- Use `append_to_state` to track iteration history
- Check state in tools to decide when to `exit_loop()`

### 6. Validation Rules

```go
// CRD validation
// +kubebuilder:validation:XValidation:rule="!has(self.agent) || !has(self.workflow)",message="subagent must specify either agent or workflow, not both"
// +kubebuilder:validation:XValidation:rule="has(self.agent) || has(self.workflow)",message="subagent must specify either agent or workflow"
// +kubebuilder:validation:XValidation:rule="self.workflow.type != 'Loop' || has(self.workflow.loopConfig)",message="Loop workflow must specify loopConfig"
```

## Implementation Timeline

### Phase 1: Core Workflow Support (6-8 hours)
- [ ] Update v1alpha2 Subagent CRD with workflow field (1h)
- [ ] Add WorkflowSubagent, WorkflowType, LoopConfig types (1h)
- [ ] Generate and apply updated CRDs (0.5h)
- [ ] Update ADK translator for workflow subagents (2-3h)
- [ ] Resolve workflow agent references (1h)
- [ ] Map to ADK workflow agent types (1-1.5h)

### Phase 2: Python Integration (4-5 hours)
- [ ] Add WorkflowConfig to kagent-adk types (1h)
- [ ] Implement workflow agent creation in AgentConfig.to_agent() (2h)
- [ ] Handle Sequential, Parallel, Loop patterns (1-2h)

### Phase 3: Testing (4-5 hours)
- [ ] Unit tests for workflow CRD validation (1h)
- [ ] Integration tests for each workflow type (2h)
- [ ] E2E workflow examples (1-2h)

### Phase 4: Documentation (2-3 hours)
- [ ] Update design documents (1h)
- [ ] Create workflow examples (1h)
- [ ] API reference updates (0.5-1h)

**Total Estimated Time: 16-21 hours**

## Test Plan

### Unit Tests (4 hours)

#### Go Tests
**Location**: `go/api/v1alpha2/`
- [ ] Test `WorkflowSubagent` type validation
- [ ] Test `WorkflowType` enum validation
- [ ] Test `LoopConfig` validation (min 1, max 100 iterations)
- [ ] Test mutual exclusion (agent XOR workflow)
- [ ] Test Loop workflow requires loopConfig

**Location**: `go/internal/controller/translator/agent/`
- [ ] Test `translateWorkflowSubagent()` for Sequential type
- [ ] Test `translateWorkflowSubagent()` for Parallel type
- [ ] Test `translateWorkflowSubagent()` for Loop type
- [ ] Test workflow agent resolution
- [ ] Test error handling for missing workflow agents

#### Python Tests
**Location**: `python/packages/kagent-adk/tests/`
- [ ] Test `WorkflowConfig` model validation
- [ ] Test `AgentConfig.to_agent()` with workflow_subagents
- [ ] Test SequentialAgent creation from workflow config
- [ ] Test ParallelAgent creation from workflow config
- [ ] Test LoopAgent creation with max_iterations
- [ ] Test workflow agent naming conventions

**Location**: `python/packages/kagent-adk/tests/test_workflow_toolcontext.py`
- [ ] Test tool reads from `tool_context.state`
- [ ] Test tool writes to `tool_context.state`
- [ ] Test `tool_context.append_to_state()` accumulates list values
- [ ] Test `exit_loop()` terminates LoopAgent early
- [ ] Test `exit_loop()` outside loop has no effect
- [ ] Test state passing in Sequential workflow
- [ ] Test state isolation in Parallel workflow
- [ ] Test state persistence across loop iterations

### Integration Tests (4-5 hours)

**Scenarios**:
1. **Sequential Workflow**
   - Deploy code-writer, code-reviewer, code-refactorer agents
   - Deploy pipeline agent with Sequential workflow
   - Send code generation request
   - Verify sequential execution order
   - Check that output of each step feeds into next

2. **Parallel Workflow**
   - Deploy 3 independent research agents
   - Deploy coordinator with Parallel workflow
   - Send research request
   - Verify agents run concurrently
   - Confirm results collected from all agents

3. **Loop Workflow**
   - Deploy writer and critic agents
   - Deploy improver with Loop workflow (max 5 iterations)
   - Send document generation request
   - Verify iterative refinement
   - Confirm loop terminates at max iterations

4. **Nested Workflows**
   - Deploy agents for nested workflow example
   - Verify Parallel → Sequential execution
   - Check state propagation between workflow stages

5. **Error Handling**
   - Deploy workflow referencing non-existent agent
   - Verify status shows error
   - Fix agent reference
   - Verify recovery

6. **Loop with Early Exit (Tool Context)**
   - Deploy loop workflow with quality checker tool
   - Tool calls `exit_loop()` when quality >= 80
   - Send document generation request
   - Verify loop exits before maxIterations
   - Check state contains quality_score
   - Confirm OpenTelemetry trace shows early termination

7. **Sequential State Passing (Tool Context)**
   - Deploy sequential workflow (research → analysis → report)
   - Research agent uses `append_to_state("findings", ...)`
   - Analysis agent reads findings, uses `append_to_state("insights", ...)`
   - Report agent reads both findings and insights
   - Verify state flows through all steps
   - Confirm final output synthesizes all data

8. **Parallel Result Aggregation (Tool Context)**
   - Deploy parallel workflow with 3 research agents
   - Each agent uses `append_to_state("results", ...)`
   - Verify all agents execute concurrently
   - Confirm state["results"] has 3 entries after completion
   - Check no state collisions between parallel agents

### E2E Tests (2-3 hours)

**Example 1: Code Development Pipeline** (from ADK docs)
```bash
# Deploy individual agents
kubectl apply -f examples/workflow-subagents/code-writer-agent.yaml
kubectl apply -f examples/workflow-subagents/code-reviewer-agent.yaml
kubectl apply -f examples/workflow-subagents/code-refactorer-agent.yaml

# Deploy sequential workflow
kubectl apply -f examples/workflow-subagents/code-pipeline.yaml

# Send request via A2A
curl -X POST http://kagent-controller:8083/api/a2a/kagent/code-pipeline \
  -H "Content-Type: application/json" \
  -d '{"input": "Create a Python function to calculate fibonacci numbers"}'

# Verify:
# 1. Writer generates initial code
# 2. Reviewer provides feedback
# 3. Refactorer improves code
```

**Example 2: Parallel Research** (from ADK docs)
```bash
# Deploy research agents
kubectl apply -f examples/workflow-subagents/research-agents/

# Deploy parallel workflow
kubectl apply -f examples/workflow-subagents/parallel-research.yaml

# Send request
curl -X POST http://kagent-controller:8083/api/a2a/kagent/parallel-research \
  -H "Content-Type: application/json" \
  -d '{"input": "Research latest developments in clean energy"}'

# Verify all agents execute concurrently
```

**Example 3: Iterative Improvement** (from ADK docs)
```bash
# Deploy writer and critic
kubectl apply -f examples/workflow-subagents/writer-critic/

# Deploy loop workflow
kubectl apply -f examples/workflow-subagents/iterative-improvement.yaml

# Send request
curl -X POST http://kagent-controller:8083/api/a2a/kagent/iterative-improvement \
  -H "Content-Type: application/json" \
  -d '{"input": "Write a technical blog post about Kubernetes agents"}'

# Verify iterative refinement up to max iterations
```

**Observability Verification**:
- [ ] OpenTelemetry traces show workflow pattern (seq/parallel/loop)
- [ ] Prometheus metrics track workflow executions
- [ ] Logs contain workflow type and iteration count (for loops)

## Alternatives

### Alternative 1: Single Workflow CRD
**Approach**: Create separate `Workflow` CRD instead of embedding in Agent.

**Pros**:
- Clean separation of concerns
- Reusable workflows across agents
- Could support more complex patterns

**Cons**:
- Additional CRD to learn and manage
- More complex for simple workflows
- Violates YAGNI principle
- Harder to version workflows with agents

**Rejected Because**: Embedding workflows in Agent CRD provides simpler UX for common cases. Can revisit if workflow reuse becomes critical.

### Alternative 2: Subagent Tags Only (No Workflow Types)
**Approach**: Use tags/annotations to group subagents, let LLM decide execution order.

**Example**:
```yaml
subagents:
  - agent: {name: agent-a}
    tags: ["stage-1"]
  - agent: {name: agent-b}
    tags: ["stage-1"]
  - agent: {name: agent-c}
    tags: ["stage-2"]
```

**Pros**:
- Simpler schema
- More flexible (LLM-driven)
- No need for explicit workflow types

**Cons**:
- Non-deterministic execution
- Loses benefits of ADK's proven workflow patterns
- Harder to debug and predict behavior
- No loop support

**Rejected Because**: Explicit workflow patterns (Sequential, Parallel, Loop) provide deterministic, testable behavior. LLM-driven orchestration can be added later as Alternative delegation mode.

### Alternative 3: Code-Only Workflows (Python/Java)
**Approach**: Don't add workflow support to CRD. Users write Python/Java with ADK workflow agents.

**Pros**:
- No CRD changes
- Full ADK flexibility
- Programmable logic

**Cons**:
- Not declarative (violates Principle V)
- Can't use GitOps
- Requires coding for simple patterns
- Not Kubernetes-native (violates Principle I)

**Rejected Because**: Main value of kagent is declarative, Kubernetes-native agent configuration. Common workflow patterns should be expressible in YAML.

### Alternative 4: Full LangGraph Integration
**Approach**: Integrate LangGraph's StateGraph for workflow definitions.

**Pros**:
- Very powerful workflow DSL
- Supports conditionals, loops, branches
- Python ecosystem

**Cons**:
- Complex to translate to YAML
- Requires learning LangGraph concepts
- Overkill for simple patterns
- ADK integration unclear

**Rejected Because**: Phase 1 focuses on proven ADK patterns (Sequential, Parallel, Loop) which cover 90% of use cases. Can add LangGraph integration later if needed.

## Open Questions

### 1. Workflow State Persistence
**Question**: Should workflow state (e.g., loop iteration count, parallel results) persist across agent pod restarts?

**Current Stance**: Phase 1 does NOT persist workflow state. Workflows execute in-memory. If agent pod restarts, workflow starts over.

**Future**: Could integrate with Memory CRD to persist workflow state. Requires design for state schema and recovery.

### 2. Workflow Monitoring & Observability
**Question**: How should workflow execution be observed? What metrics/traces are needed?

**Current Stance**: Phase 1 leverages ADK's built-in observability. OpenTelemetry traces show workflow execution. Add custom metrics for workflow-specific events (loop iterations, parallel completion).

**Future**: Could add Workflow-specific dashboards, alerting on stuck loops, parallel agent failures.

### 3. Conditional Workflows
**Question**: Do we need conditional branching (if-then-else) within workflows?

**Current Stance**: Not in Phase 1. Users can implement conditions via agent logic (e.g., critic agent returns "STOP" signal in loop).

**Future**: Could add `ConditionalAgent` type or integrate with LangGraph for complex conditionals.

### 4. Workflow Composition Depth
**Question**: Should we limit nesting depth? (e.g., max 3 levels: Agent → Sequential → Parallel → Loop)

**Current Stance**: No hard limit in Phase 1. Document best practices (recommend max 2-3 nesting levels).

**Rationale**: YAGNI - let users experiment. Can add validation if deep nesting causes issues.

### 5. Workflow Result Aggregation
**Question**: How should results from Parallel workflows be aggregated?

**Current Stance**: ADK's ParallelAgent collects all results. Parent agent's instruction should guide how to synthesize results.

**Future**: Could add explicit aggregation strategies (merge, vote, first-wins, etc).

### 6. Loop Termination Beyond Max Iterations ✅ RESOLVED
**Question**: Should we support custom termination conditions for loops?

**Current Stance**: ✅ **YES - via `exit_loop()` from `google.adk.tools`**

Tools can terminate loops early by importing and calling `exit_loop()`:

```python
from google.adk.tools import exit_loop

def check_completion(document: str, tool_context: ToolContext) -> dict:
    """Checks if work is complete. Exits loop early if done."""
    if is_complete(document):
        exit_loop()  # Terminates loop immediately
        return {"status": "complete"}
    return {"status": "continuing"}
```

**Phase 1 Support**:
- `maxIterations` (hard limit in CRD)
- `exit_loop()` (tool-driven early termination)

**Future**: Could add declarative `terminationCondition: "state.score >= 80"` field with CEL expressions for non-tool-based conditions, but `exit_loop()` covers most use cases.

### 7. Workflow Validation at Creation Time
**Question**: Should controller validate that all workflow agents exist and are Ready before creating parent?

**Current Stance**: Controller validates references exist. Does NOT require agents to be Ready (eventual consistency).

**Rationale**: Kubernetes pattern is eventual consistency. Status will show errors if agents aren't Ready.

## References

- [ADK Workflow Agents Documentation](https://github.com/google/adk-docs/blob/main/docs/agents/workflow-agents/)
- [ADK Sequential Agents](https://github.com/google/adk-docs/blob/main/docs/agents/workflow-agents/sequential-agents.md)
- [ADK Parallel Agents](https://github.com/google/adk-docs/blob/main/docs/agents/workflow-agents/parallel-agents.md)
- [ADK Loop Agents](https://github.com/google/adk-docs/blob/main/docs/agents/workflow-agents/loop-agents.md)
- [A2A Protocol](https://github.com/google/A2A)
- [ADK Tools - Controlling Agent Flow](https://google.github.io/adk-docs/tools/#controlling-agent-flow)

