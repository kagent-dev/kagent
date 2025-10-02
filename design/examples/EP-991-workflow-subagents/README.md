# Workflow Subagents Examples

This directory contains examples of using ADK workflow patterns (Sequential, Parallel, Loop) as declarative subagents in kagent.

## Overview

**Reference Design**: [EP-991-workflow-subagents.md](../../EP-991-workflow-subagents.md)

These examples demonstrate three workflow patterns from [ADK](https://github.com/google/adk-python):

1. **Sequential**: Execute agents in strict order (A → B → C)
2. **Parallel**: Execute agents concurrently (A || B || C)
3. **Loop**: Execute agents iteratively with max iterations

## Examples

### 1. Sequential Code Pipeline

**File**: [`sequential-code-pipeline.yaml`](./sequential-code-pipeline.yaml)

**Pattern**: Code Writer → Code Reviewer → Code Refactorer

Demonstrates a fixed-order pipeline where each agent's output feeds into the next.

```bash
# Deploy
kubectl apply -f sequential-code-pipeline.yaml

# Test
curl -X POST http://kagent-controller:8083/api/a2a/kagent/code-pipeline \
  -H "Content-Type: application/json" \
  -d '{"input": "Create a Python function to calculate fibonacci numbers"}'
```

**Expected Flow**:
1. Code Writer generates initial implementation
2. Code Reviewer provides detailed feedback
3. Code Refactorer applies improvements
4. Final refactored code returned to user

---

### 2. Parallel Research

**File**: [`parallel-research.yaml`](./parallel-research.yaml)

**Pattern**: Renewable Energy Researcher || EV Tech Researcher || Carbon Capture Researcher

Demonstrates concurrent execution of independent research tasks.

```bash
# Deploy
kubectl apply -f parallel-research.yaml

# Test
curl -X POST http://kagent-controller:8083/api/a2a/kagent/parallel-research-coordinator \
  -H "Content-Type: application/json" \
  -d '{"input": "Research latest clean energy technologies for 2025"}'
```

**Expected Flow**:
1. All 3 researchers execute concurrently
2. Each researches their domain independently
3. Coordinator synthesizes all findings
4. Unified research report returned

**Performance**: Total time ≈ longest individual research (not sum)

---

### 3. Loop Document Improvement

**File**: [`loop-document-improvement.yaml`](./loop-document-improvement.yaml)

**Pattern**: (Writer → Critic) × max 5 iterations

Demonstrates iterative refinement with termination condition.

```bash
# Deploy
kubectl apply -f loop-document-improvement.yaml

# Test
curl -X POST http://kagent-controller:8083/api/a2a/kagent/iterative-document-improver \
  -H "Content-Type: application/json" \
  -d '{"input": "Write a technical blog post about Kubernetes agent patterns"}'
```

**Expected Flow**:
1. Loop iteration 1: Writer creates draft → Critic provides feedback
2. Loop iteration 2: Writer improves → Critic reviews
3. ... repeats up to 5 times
4. Final polished document returned

**Termination**: Stops at max 5 iterations or early exit signal from critic.

---

### 4. Nested Workflow

**File**: [`nested-workflow.yaml`](./nested-workflow.yaml)

**Pattern**: Parallel Research → Sequential Analysis

Demonstrates composing multiple workflow patterns.

```bash
# Deploy
kubectl apply -f nested-workflow.yaml

# Test
curl -X POST http://kagent-controller:8083/api/a2a/kagent/strategic-analysis-workflow \
  -H "Content-Type: application/json" \
  -d '{"input": "Analyze feasibility of a Kubernetes-native AI platform"}'
```

**Expected Flow**:
```
Strategic Analysis
├─ Stage 1: Parallel Research
│  ├─ Market Researcher (concurrent)
│  ├─ Technical Researcher (concurrent)
│  └─ Compliance Researcher (concurrent)
└─ Stage 2: Sequential Analysis
   ├─ Data Synthesizer (waits for all research)
   ├─ Risk Analyzer (uses synthesis)
   └─ Recommendation Generator (uses analysis)
```

---

## Workflow Patterns Explained

### Sequential

**When to Use**: Fixed-order pipelines where each step depends on previous output.

**YAML Structure**:
```yaml
subagents:
  - workflow:
      type: Sequential
      agents:
        - name: step-1-agent
        - name: step-2-agent
        - name: step-3-agent
```

**ADK Equivalent**:
```python
SequentialAgent(sub_agents=[Step1Agent, Step2Agent, Step3Agent])
```

---

### Parallel

**When to Use**: Independent tasks that can run concurrently to save time.

**YAML Structure**:
```yaml
subagents:
  - workflow:
      type: Parallel
      agents:
        - name: task-a-agent
        - name: task-b-agent
        - name: task-c-agent
```

**ADK Equivalent**:
```python
ParallelAgent(sub_agents=[TaskAAgent, TaskBAgent, TaskCAgent])
```

---

### Loop

**When to Use**: Iterative refinement with termination condition.

**YAML Structure**:
```yaml
subagents:
  - workflow:
      type: Loop
      agents:
        - name: generator-agent
        - name: evaluator-agent
      loopConfig:
        maxIterations: 5
```

**ADK Equivalent**:
```python
LoopAgent(sub_agents=[GeneratorAgent, EvaluatorAgent], max_iterations=5)
```

---

## Testing All Examples

```bash
# Deploy all examples
kubectl apply -f design/examples/EP-991-workflow-subagents/

# Check status
kubectl get agents -n kagent

# Wait for all agents to be Ready
kubectl wait --for=condition=Ready agent --all -n kagent --timeout=60s

# Test each workflow
# Sequential
curl -X POST http://kagent-controller:8083/api/a2a/kagent/code-pipeline \
  -H "Content-Type: application/json" \
  -d '{"input": "Create Python fibonacci function"}'

# Parallel
curl -X POST http://kagent-controller:8083/api/a2a/kagent/parallel-research-coordinator \
  -H "Content-Type: application/json" \
  -d '{"input": "Research clean energy"}'

# Loop
curl -X POST http://kagent-controller:8083/api/a2a/kagent/iterative-document-improver \
  -H "Content-Type: application/json" \
  -d '{"input": "Write blog about K8s agents"}'

# Nested
curl -X POST http://kagent-controller:8083/api/a2a/kagent/strategic-analysis-workflow \
  -H "Content-Type: application/json" \
  -d '{"input": "Analyze K8s AI platform feasibility"}'
```

## Observability

### View Traces
```bash
# Port-forward to Jaeger (if installed)
kubectl port-forward -n observability svc/jaeger-query 16686:16686

# Open http://localhost:16686
# Search for traces with tags:
# - workflow_type: Sequential|Parallel|Loop
# - agent: code-pipeline|parallel-research-coordinator|...
```

### View Metrics
```bash
# Port-forward to Prometheus
kubectl port-forward -n observability svc/prometheus 9090:9090

# Query:
# agent_workflow_executions_total{workflow_type="Sequential"}
# agent_workflow_duration_seconds{workflow_type="Parallel"}
# agent_loop_iterations_total
```

### View Logs
```bash
# Controller logs
kubectl logs -n kagent -l app=kagent-controller -f

# Agent logs
kubectl logs -n kagent code-writer -f
kubectl logs -n kagent parallel-research-coordinator -f
```

## Common Patterns

### Error Handling
All workflow agents propagate errors from subagents. If any subagent fails:
- **Sequential**: Stops at failed agent
- **Parallel**: Collects errors from all agents
- **Loop**: May terminate early on error

### State Passing
Agents use ADK's state management (`output_key` / `temp:` namespace) to pass data:

```yaml
# Agent 1
output_key: "my_data"  # Stores in temp:my_data

# Agent 2 (later in workflow)
systemMessage: "Read data from temp:my_data"
```

### Workflow Nesting
You can compose workflows:

```yaml
subagents:
  # First subagent is a workflow
  - workflow:
      type: Parallel
      agents: [...]
  
  # Second subagent is also a workflow
  - workflow:
      type: Sequential
      agents: [...]
```

## Troubleshooting

### Workflow Not Executing
```bash
# Check agent status
kubectl describe agent code-pipeline -n kagent

# Common issues:
# 1. Subagent doesn't exist
# 2. Subagent not Ready
# 3. Invalid workflow configuration
```

### Loop Never Terminates
```bash
# Check maxIterations is set
kubectl get agent iterative-document-improver -o yaml | grep maxIterations

# Check logs for iteration count
kubectl logs -n kagent iterative-document-improver | grep "iteration"
```

### Parallel Agents Not Concurrent
```bash
# Verify all agents deployed
kubectl get agents -n kagent | grep researcher

# Check if ParallelAgent was created correctly
# Look for logs indicating parallel execution
kubectl logs -n kagent parallel-research-coordinator | grep "parallel"
```

## References

- [ADK Workflow Agents](https://github.com/google/adk-docs/blob/main/docs/agents/workflow-agents/)
- [Workflow Subagents Design](../../EP-991-workflow-subagents.md)
- [A2A Protocol](https://github.com/google/A2A)

