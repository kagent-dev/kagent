# DAG-Based Step Execution in Temporal Go SDK

Research findings for the declarative workflow builder/executor design document.

---

## 1. Fan-Out / Fan-In Patterns

Temporal Go SDK provides three primary mechanisms for parallel execution within workflows.

### workflow.Go() — Deterministic Goroutines

`workflow.Go()` is the only way to spawn concurrent execution inside a Temporal workflow. Native Go goroutines are **forbidden** because they break deterministic replay. The SDK's deterministic runner controls thread execution order — one at a time — which eliminates race conditions and removes the need for mutexes.

```go
workflow.Go(ctx, func(gCtx workflow.Context) {
    // runs concurrently with other workflow.Go calls
    err := workflow.ExecuteActivity(gCtx, "MyActivity", input).Get(gCtx, &result)
})
```

### Future-Based Fan-Out / Fan-In (Split/Merge Future pattern)

Launch multiple activities, collect futures, then call `.Get()` on each:

```go
// Fan-out: launch all activities
var futures []workflow.Future
for _, chunk := range chunks {
    f := workflow.ExecuteActivity(ctx, "ProcessChunk", chunk)
    futures = append(futures, f)
}

// Fan-in: wait for all results
var results []Result
for _, f := range futures {
    var r Result
    if err := f.Get(ctx, &r); err != nil {
        return err
    }
    results = append(results, r)
}
```

**Trade-off:** Simple but blocks on futures in order. If future[2] completes before future[0], you still wait for future[0] first.

### Selector-Based Fan-Out / Fan-In (Split/Merge Selector pattern)

Process results as they arrive using `workflow.Selector`:

```go
selector := workflow.NewSelector(ctx)
var results []Result

for _, chunk := range chunks {
    f := workflow.ExecuteActivity(ctx, "ProcessChunk", chunk)
    selector.AddFuture(f, func(f workflow.Future) {
        var r Result
        if err := f.Get(ctx, &r); err != nil {
            // handle error
        }
        results = append(results, r)
    })
}

// Wait for all
for i := 0; i < len(chunks); i++ {
    selector.Select(ctx)
}
```

**Key Selector behaviors:**
- `AddFuture(future, callback)` — defers callback until future resolves
- `AddReceive(channel, callback)` — listens for channel messages
- `Select(ctx)` — blocks until one registered future/channel is ready, then executes its callback
- `HasPending()` — checks if there are unresolved items (useful before ContinueAsNew)
- Each future matches **only once** per Selector instance
- If multiple items are ready simultaneously, ordering is **undefined**

### Pick-First Pattern

Execute activities in parallel, take the first result, cancel the rest:

```go
childCtx, cancel := workflow.WithCancel(ctx)
selector := workflow.NewSelector(ctx)

for _, branch := range branches {
    f := workflow.ExecuteActivity(childCtx, branch.Name, branch.Input)
    selector.AddFuture(f, func(f workflow.Future) {
        // got first result, cancel others
        cancel()
        f.Get(ctx, &result)
    })
}
selector.Select(ctx) // wait for first only
```

---

## 2. Dynamic Activity Execution

Activities can be invoked by string name rather than static function reference:

```go
// Static (type-safe, validates parameters at registration):
future := workflow.ExecuteActivity(ctx, MyActivityFunc, arg1, arg2)

// Dynamic (string-based, no compile-time validation):
future := workflow.ExecuteActivity(ctx, "MyActivityName", arg1, arg2)
```

### How It Works

When a string is passed, Temporal looks up the activity by its registered name on the worker. The activity must be registered on the worker side:

```go
// Worker registration (the name defaults to the function name):
w.RegisterActivity(MyActivityFunc)

// Or with custom name:
w.RegisterActivityWithOptions(MyActivityFunc, activity.RegisterOptions{
    Name: "custom-activity-name",
})
```

### Dynamic Activity Handler

For fully dynamic dispatch, register a catch-all handler:

```go
// Registers a handler invoked for any activity name without a specific registration
w.RegisterDynamicActivity(func(ctx context.Context, args converter.EncodedValues) (interface{}, error) {
    activityName := activity.GetInfo(ctx).ActivityType.Name
    // dispatch based on activityName
    switch activityName {
    case "step1":
        // ...
    }
})
```

### Trade-offs for DAG Builder

| Approach | Pros | Cons |
|----------|------|------|
| String-based name | Fully dynamic, DSL-driven | No compile-time type safety |
| Dynamic activity handler | Single registration point | Centralizes all logic |
| Static function reference | Type-safe, validated | Cannot be driven from DSL |

**Recommendation for declarative workflow builder:** Use string-based `ExecuteActivity` calls. This is exactly what the Temporal DSL sample does. Register all available step activities on the worker, then invoke them by name from the DAG definition.

---

## 3. Dependency Resolution

### Topological Sort for DAG Execution

For executing a DAG where nodes have dependencies, the standard approach is:

1. **Build the dependency graph** from the declarative definition
2. **Compute in-degrees** (number of incoming edges per node)
3. **Use Kahn's algorithm (BFS)** to find execution layers — groups of nodes that can execute in parallel

```
Kahn's Algorithm:
1. Find all nodes with in-degree 0 (no dependencies) -> Layer 0
2. Execute Layer 0 in parallel
3. Remove completed nodes, decrement in-degrees of dependents
4. Find new nodes with in-degree 0 -> Layer 1
5. Repeat until all nodes processed
```

### Layer-Based Execution in Temporal

Each layer becomes a parallel block, layers execute sequentially:

```go
func executeDAG(ctx workflow.Context, dag *DAG, bindings map[string]string) error {
    remaining := dag.Nodes()
    inDegree := computeInDegrees(dag)

    for len(remaining) > 0 {
        // Find ready nodes (in-degree == 0)
        var ready []Node
        for _, n := range remaining {
            if inDegree[n.ID] == 0 {
                ready = append(ready, n)
            }
        }

        // Execute ready nodes in parallel
        if err := executeParallel(ctx, ready, bindings); err != nil {
            return err
        }

        // Remove completed, update in-degrees
        for _, n := range ready {
            for _, dep := range dag.Dependents(n.ID) {
                inDegree[dep]--
            }
            delete(remaining, n.ID)
        }
    }
    return nil
}
```

### Event-Driven Alternative (More Granular)

Instead of layer-by-layer, trigger each node as soon as all its dependencies complete:

```go
func executeDAGEventDriven(ctx workflow.Context, dag *DAG, bindings map[string]string) error {
    completedCh := workflow.NewChannel(ctx)
    completed := map[string]bool{}
    pending := len(dag.Nodes())

    // Launch all nodes, each waits for its dependencies
    for _, node := range dag.Nodes() {
        node := node
        workflow.Go(ctx, func(gCtx workflow.Context) {
            // Wait until all dependencies are satisfied
            workflow.Await(gCtx, func() bool {
                for _, dep := range node.Dependencies {
                    if !completed[dep] {
                        return false
                    }
                }
                return true
            })
            // Execute the node
            err := executeNode(gCtx, node, bindings)
            completedCh.Send(gCtx, nodeResult{ID: node.ID, Err: err})
        })
    }

    // Collect results
    for pending > 0 {
        var result nodeResult
        completedCh.Receive(ctx, &result)
        if result.Err != nil {
            return result.Err
        }
        completed[result.ID] = true
        pending--
    }
    return nil
}
```

**`workflow.Await(ctx, conditionFunc)`** blocks the goroutine until `conditionFunc` returns true. It is re-evaluated whenever any coroutine yields (e.g., after an activity completes), making it ideal for dependency checks.

### Comparison

| Approach | Parallelism | Complexity | Best For |
|----------|-------------|------------|----------|
| Layer-by-layer (Kahn's) | Good (within layers) | Low | Simple DAGs, predictable execution |
| Event-driven (Await) | Maximum | Medium | Complex DAGs, varying step durations |
| Temporal DSL sample | Explicit parallel/sequence | Lowest | Tree-structured workflows |

---

## 4. Variable / Data Passing

### Bindings Map Pattern (from Temporal DSL sample)

The DSL sample uses a shared `map[string]string` called `bindings`:

```go
// Initialize from workflow variables
bindings := make(map[string]string)
for k, v := range dslWorkflow.Variables {
    bindings[k] = v
}

// Activity stores result in bindings
func (a ActivityInvocation) execute(ctx workflow.Context, bindings map[string]string) error {
    inputParam := makeInput(a.Arguments, bindings)
    var result string
    err := workflow.ExecuteActivity(ctx, a.Name, inputParam).Get(ctx, &result)
    if err != nil {
        return err
    }
    if a.Result != "" {
        bindings[a.Result] = result
    }
    return nil
}

// Resolve arguments from bindings
func makeInput(argNames []string, argsMap map[string]string) []string {
    var args []string
    for _, arg := range argNames {
        args = append(args, argsMap[arg])
    }
    return args
}
```

### Key Observations

1. **Bindings are shared across the entire workflow execution.** Sequential steps naturally chain outputs to inputs.
2. **Parallel branches share the same bindings map.** This is safe in Temporal because `workflow.Go()` goroutines are cooperatively scheduled (one at a time), so no mutex is needed.
3. **Type limitation:** The DSL sample uses `map[string]string`. For richer types, consider `map[string]interface{}` or `map[string][]byte` with serialization.

### Enhanced Pattern for Typed Data

```go
type StepContext struct {
    Bindings map[string]interface{}
    mu       sync.Mutex // not needed in Temporal workflows, but shown for clarity
}

func (sc *StepContext) Set(key string, value interface{}) {
    sc.Bindings[key] = value
}

func (sc *StepContext) Get(key string) (interface{}, bool) {
    v, ok := sc.Bindings[key]
    return v, ok
}
```

### Data Passing Considerations for DAGs

- **Within a workflow:** Use the bindings map. It's deterministic and replay-safe.
- **Large payloads:** Store in external storage (S3, blob store) and pass URLs via bindings. Temporal payloads have a 2MB default limit per event.
- **Between parent/child workflows:** Pass via child workflow input parameters and return values.
- **Across ContinueAsNew:** Pass the bindings map as workflow input to the new execution.

---

## 5. Child Workflows vs Activities

### When to Use Activities

- Default choice for individual work units
- Non-deterministic operations (HTTP calls, DB writes, file I/O)
- Short to medium duration tasks
- No need for independent lifecycle management

### When to Use Child Workflows

| Use Case | Reason |
|----------|--------|
| **Event history partitioning** | A single workflow is limited to 51,200 events. A parent can spawn 1,000 children that each spawn 1,000 activities = 1M total activities. |
| **Independent lifecycle** | Child workflows can outlive parents (via `ParentClosePolicy: ABANDON`). |
| **Sub-DAG execution** | A child workflow can execute an entire sub-DAG with its own event history. |
| **Periodic logic** | Child can use ContinueAsNew without polluting parent history. |
| **Different task queues** | Route sub-DAG execution to specialized workers. |

### When NOT to Use Child Workflows

- **Code organization alone** — use regular Go functions/structs instead
- **Simple sequential steps** — activities are simpler and more efficient
- **Tight coupling with parent** — if parent always waits synchronously, an activity is usually sufficient

### Pattern for Reusable Sub-DAGs

```go
// Parent workflow
func ParentWorkflow(ctx workflow.Context, dag DAG) error {
    // For large sub-graphs, execute as child workflow
    for _, subDAG := range dag.SubGraphs {
        cwo := workflow.ChildWorkflowOptions{
            WorkflowID: fmt.Sprintf("sub-dag-%s", subDAG.ID),
        }
        childCtx := workflow.WithChildOptions(ctx, cwo)
        err := workflow.ExecuteChildWorkflow(childCtx, SubDAGWorkflow, subDAG).Get(ctx, nil)
        if err != nil {
            return err
        }
    }
    return nil
}
```

### Heuristic for DAG Builder

- **< 100 steps:** Single workflow with activities
- **100-1000 steps:** Partition into child workflows by sub-graph
- **> 1000 steps:** Mandatory child workflow partitioning, consider ContinueAsNew within children

---

## 6. Error Handling in DAGs

### Error Types

```go
import "go.temporal.io/sdk/temporal"

// ApplicationError — business logic failure
var appErr *temporal.ApplicationError
if errors.As(err, &appErr) {
    errType := appErr.Type()    // custom error type string
    appErr.Details(&details)    // extract structured details
}

// CanceledError — workflow/activity was canceled
var cancelErr *temporal.CanceledError
if errors.As(err, &cancelErr) {
    // handle cancellation
}

// TimeoutError — operation timed out
var timeoutErr *temporal.TimeoutError
if errors.As(err, &timeoutErr) {
    timeoutType := timeoutErr.TimeoutType() // ScheduleToStart, StartToClose, Heartbeat
}

// ActivityError — wraps the above for activity failures
var actErr *temporal.ActivityError
if errors.As(err, &actErr) {
    // unwrap to get underlying cause
    errors.As(actErr, &appErr)
}
```

### Partial Failure Handling Patterns

**Pattern 1: Fail-fast (DSL sample default)**
If any branch fails, cancel all others and return the error:

```go
func (p Parallel) execute(ctx workflow.Context, bindings map[string]string) error {
    childCtx, cancelHandler := workflow.WithCancel(ctx)
    selector := workflow.NewSelector(ctx)
    var activityErr error

    for _, s := range p.Branches {
        f := executeAsync(s, childCtx, bindings)
        selector.AddFuture(f, func(f workflow.Future) {
            err := f.Get(ctx, nil)
            if err != nil {
                cancelHandler() // cancel all pending
                activityErr = err
            }
        })
    }

    for i := 0; i < len(p.Branches); i++ {
        selector.Select(ctx)
        if activityErr != nil {
            return activityErr
        }
    }
    return nil
}
```

**Pattern 2: Continue-on-error (collect all results)**

```go
func executeParallelContinueOnError(ctx workflow.Context, nodes []Node, bindings map[string]string) ([]error, error) {
    selector := workflow.NewSelector(ctx)
    errs := make([]error, len(nodes))

    for i, node := range nodes {
        i, node := i, node
        f := executeAsync(node, ctx, bindings) // no WithCancel
        selector.AddFuture(f, func(f workflow.Future) {
            errs[i] = f.Get(ctx, nil)
        })
    }

    for i := 0; i < len(nodes); i++ {
        selector.Select(ctx)
    }

    // Return individual errors, let caller decide
    return errs, nil
}
```

**Pattern 3: Configurable per-step error policy**

```go
type StepErrorPolicy string
const (
    FailFast      StepErrorPolicy = "fail_fast"
    ContinueOnErr StepErrorPolicy = "continue_on_error"
    Retry         StepErrorPolicy = "retry"
)

type Step struct {
    Name         string
    ErrorPolicy  StepErrorPolicy
    MaxRetries   int
    Dependencies []string
}
```

### Retry Configuration

Temporal provides built-in retry at the activity level:

```go
ao := workflow.ActivityOptions{
    StartToCloseTimeout: 10 * time.Minute,
    RetryPolicy: &temporal.RetryPolicy{
        InitialInterval:    time.Second,
        BackoffCoefficient: 2.0,
        MaximumInterval:    time.Minute,
        MaximumAttempts:    3,
        NonRetryableErrorTypes: []string{"PermanentError"},
    },
}
```

This means retry logic does **not** need to be reimplemented in the DAG executor — it is handled by the Temporal runtime per-activity.

---

## 7. Cancellation Propagation

### How Cancellation Flows

1. **Workflow cancellation request** is received by the Temporal server
2. **ctx.Done()** is triggered on the workflow context
3. **All derived contexts** (from `workflow.WithCancel`, `workflow.WithActivityOptions`, etc.) are also canceled
4. **In-progress activities** receive cancellation via their context (requires heartbeating)
5. **Child workflows** receive cancellation based on `ParentClosePolicy`

### Key APIs

```go
// Create a cancellable sub-context (for canceling a subset of operations)
childCtx, cancel := workflow.WithCancel(ctx)

// Create a context that does NOT propagate parent cancellation (for cleanup)
disconnectedCtx, _ := workflow.NewDisconnectedContext(ctx)

// Check if context is canceled
if ctx.Err() == workflow.ErrCanceled { ... }
```

### Cleanup After Cancellation

```go
func MyWorkflow(ctx workflow.Context) error {
    defer func() {
        if !errors.Is(ctx.Err(), workflow.ErrCanceled) {
            return
        }
        // Use disconnected context for cleanup
        newCtx, _ := workflow.NewDisconnectedContext(ctx)
        _ = workflow.ExecuteActivity(newCtx, CleanupActivity).Get(newCtx, nil)
    }()

    // Main workflow logic
    return workflow.ExecuteActivity(ctx, MainActivity).Get(ctx, nil)
}
```

### Cancellation in Parallel DAG Branches

For the DAG executor, cancellation should be handled at two levels:

1. **Branch-level:** When a parallel branch fails (fail-fast mode), cancel sibling branches via `workflow.WithCancel`:
   ```go
   childCtx, cancel := workflow.WithCancel(ctx)
   // On first error: cancel()
   ```

2. **Workflow-level:** When the entire workflow is canceled, all branches automatically receive cancellation through context propagation. Activities must heartbeat to detect this.

### Activity Heartbeat for Cancellation Detection

```go
func LongRunningActivity(ctx context.Context, input Input) error {
    for {
        select {
        case <-ctx.Done():
            // Cancellation received, clean up
            return ctx.Err()
        default:
            activity.RecordHeartbeat(ctx, progressInfo)
            // do work
        }
    }
}
```

**Important:** `WaitForCancellation: true` in activity options makes the workflow wait until in-progress activities complete, fail, or acknowledge cancellation before proceeding.

---

## 8. Temporal DSL Sample Analysis

Source: [`temporalio/samples-go/dsl/`](https://github.com/temporalio/samples-go/tree/main/dsl)

### Architecture

The DSL sample implements a tree-structured workflow executor with three constructs:

```
Statement (union type)
  |-- ActivityInvocation  (leaf node — executes a single activity)
  |-- Sequence            (ordered list of Statements)
  |-- Parallel            (concurrent list of Statements)
```

### Type Definitions

```go
type Workflow struct {
    Variables map[string]string  // initial variable bindings
    Root      Statement          // root of the execution tree
}

type Statement struct {
    Activity *ActivityInvocation
    Sequence *Sequence
    Parallel *Parallel
}

type ActivityInvocation struct {
    Name      string    // activity name (string-based, dynamic)
    Arguments []string  // keys into the bindings map
    Result    string    // key to store the result
}
```

### Execution Model

1. **Workflow entry:** `SimpleDSLWorkflow` copies initial variables into `bindings`, sets activity options, then calls `Root.execute(ctx, bindings)`
2. **Statement dispatch:** Checks which field is non-nil (Activity, Sequence, or Parallel) and delegates
3. **Sequential execution:** Iterates `Elements`, calling `execute()` on each, stopping on first error
4. **Parallel execution:** Uses `workflow.WithCancel` + `workflow.NewSelector` + `workflow.Go` + `workflow.NewFuture` pattern. Cancels all branches on first error.
5. **Activity invocation:** Calls `workflow.ExecuteActivity(ctx, a.Name, inputParam)` with string-based name. Resolves arguments from bindings, stores result back in bindings.
6. **Data flow:** Shared `map[string]string` bindings, safe due to cooperative scheduling.

### YAML Definitions

**workflow1.yaml** — Pure sequential:
```yaml
variables:
  arg1: value1
  arg2: value2
root:
  sequence:
    elements:
     - activity:
        name: SampleActivity1
        arguments: [arg1]
        result: result1
     - activity:
        name: SampleActivity2
        arguments: [result1]
        result: result2
     - activity:
        name: SampleActivity3
        arguments: [arg2, result2]
        result: result3
```

**workflow2.yaml** — Sequential with parallel fan-out/fan-in:
```yaml
variables:
  arg1: value1
  arg2: value2
  arg3: value3
root:
  sequence:
    elements:
      - activity:
         name: SampleActivity1
         arguments: [arg1]
         result: result1
      - parallel:
          branches:
            - sequence:
                elements:
                 - activity: {name: SampleActivity2, arguments: [result1], result: result2}
                 - activity: {name: SampleActivity3, arguments: [arg2, result2], result: result3}
            - sequence:
                elements:
                 - activity: {name: SampleActivity4, arguments: [result1], result: result4}
                 - activity: {name: SampleActivity5, arguments: [arg3, result4], result: result5}
      - activity:
         name: SampleActivity1
         arguments: [result3, result5]
         result: result6
```

### Limitations of the DSL Sample

1. **Not a true DAG executor.** It supports tree-structured workflows (sequence/parallel nesting) but not arbitrary DAG dependencies. You cannot express "Step C depends on both Step A and Step B" without wrapping A and B in an explicit parallel block.
2. **String-only data.** Bindings are `map[string]string` — no structured data.
3. **No error policies.** Parallel blocks always fail-fast. No continue-on-error option.
4. **No conditional branching.** No if/else or switch constructs.
5. **No retry configuration per step.** Uses global activity options.
6. **No step timeout per step.** All steps share the same `StartToCloseTimeout`.

### What to Adopt

- The `executable` interface pattern for polymorphic execution
- String-based `ExecuteActivity` for dynamic dispatch
- The `bindings` map for inter-step data flow
- The `WithCancel` + `Selector` + `Go` + `NewFuture` combination for parallel execution

### What to Extend

- Replace tree model with true DAG model (dependency graph + topological execution)
- Support typed data in bindings (`map[string]interface{}` or `map[string]json.RawMessage`)
- Per-step error policies, retry configs, and timeouts
- Conditional execution (skip steps based on bindings or previous results)
- Step-level observability hooks

---

## 9. Performance Considerations

### Event History Limits

| Limit | Default | Warning Threshold |
|-------|---------|-------------------|
| Max events per workflow | 51,200 | 10,240 |
| Max history size | 50 MB | 10 MB |
| Max transaction size | 4 MB | — |
| Max pending activities | 2,000 | — |
| Max pending child workflows | 2,000 | — |
| Max pending signals | 2,000 | — |
| Max pending cancellations | 2,000 | — |
| Max Nexus operations | 30 | — |
| Max callbacks | 32 | — |

**Recommended practical limit for concurrent operations:** 500 or fewer for optimal performance.

### Event Cost per Activity

Each activity execution generates multiple events:
- `ActivityTaskScheduled` (1 event)
- `ActivityTaskStarted` (1 event)
- `ActivityTaskCompleted` / `ActivityTaskFailed` (1 event)

So each activity costs ~3 events minimum. For a DAG with N steps: **~3N events** minimum.

**Implication:** A single workflow can execute at most ~17,000 activities before hitting the 51,200 event limit (less with retries, timers, and other events).

### Strategies for Large DAGs

**1. Child Workflow Partitioning**
Break the DAG into sub-graphs, each executed as a child workflow with its own event history:
```
Parent workflow (orchestrator)
  |-- Child workflow: sub-DAG-1 (up to ~15K activities)
  |-- Child workflow: sub-DAG-2 (up to ~15K activities)
  |-- ...
```

**2. ContinueAsNew for Long-Running DAGs**
If executing a DAG phase-by-phase (layer by layer), use ContinueAsNew between phases to reset history:
```go
if workflow.GetInfo(ctx).GetCurrentHistoryLength() > 10000 {
    return workflow.NewContinueAsNewError(ctx, DAGWorkflow, remainingDAG, bindings)
}
```

**3. Payload Size Management**
- Default max payload: 2 MB per event
- For large intermediate data: store in external storage, pass references
- Use Temporal's Data Converter for compression

**4. Batching Small Activities**
If the DAG has many tiny steps (< 1s each), consider batching them into a single activity to reduce event overhead:
```go
// Instead of 100 individual activities
func BatchActivity(ctx context.Context, steps []StepDef) ([]StepResult, error) {
    var results []StepResult
    for _, step := range steps {
        r := executeStep(step)
        results = append(results, r)
    }
    return results, nil
}
```

**Trade-off:** Batching loses per-step retry and visibility.

**5. Local Activities for Low-Latency Steps**
For steps that are fast and don't need independent retry:
```go
lao := workflow.LocalActivityOptions{
    StartToCloseTimeout: 5 * time.Second,
}
localCtx := workflow.WithLocalActivityOptions(ctx, lao)
workflow.ExecuteLocalActivity(localCtx, FastStep, input).Get(ctx, &result)
```

Local activities skip the task queue round-trip but: no independent retry, no heartbeating, shorter timeout limits (recommended < 10s).

### Performance Summary Table

| DAG Size | Strategy | Estimated Events |
|----------|----------|-----------------|
| < 50 steps | Single workflow | < 200 events |
| 50-500 steps | Single workflow, monitor history | 200-1,500 events |
| 500-5,000 steps | Child workflow partitioning | Distributed across children |
| > 5,000 steps | Child workflows + ContinueAsNew | Bounded per execution |
| Many tiny steps | Batch into fewer activities | Reduced event count |

---

## Key Takeaways for Design

1. **Start with the DSL sample pattern** as the foundation — it is the official Temporal reference for declarative workflow execution.
2. **Extend to true DAG** by replacing the tree model with dependency-graph-based execution using Kahn's algorithm or event-driven `workflow.Await`.
3. **Use string-based `ExecuteActivity`** for dynamic dispatch from declarative definitions.
4. **Use `map[string]interface{}`** bindings (or `map[string]json.RawMessage`) for inter-step data flow.
5. **Implement per-step error policies** (fail-fast, continue-on-error, retry) as an extension over the DSL sample's fail-fast-only approach.
6. **Plan for history limits** from day one — build child workflow partitioning into the design for DAGs with more than a few hundred steps.
7. **Cancellation propagation works naturally** through Temporal's context hierarchy — use `WithCancel` for branch-level control and `NewDisconnectedContext` for cleanup.

---

## Sources

- [Temporal Go SDK Multithreading](https://docs.temporal.io/develop/go/go-sdk-multithreading)
- [Temporal Go SDK Selectors](https://docs.temporal.io/develop/go/selectors)
- [Temporal Go SDK Error Handling](https://docs.temporal.io/develop/go/error-handling)
- [Temporal Go SDK Cancellation](https://docs.temporal.io/develop/go/cancellation)
- [Temporal Workflow Execution Limits](https://docs.temporal.io/workflow-execution/limits)
- [Temporal Child Workflows](https://docs.temporal.io/child-workflows)
- [Temporal Continue-As-New](https://docs.temporal.io/develop/go/continue-as-new)
- [Temporal Cloud System Limits](https://docs.temporal.io/cloud/limits)
- [Temporal DSL Sample (samples-go/dsl/)](https://github.com/temporalio/samples-go/tree/main/dsl)
- [Temporal Samples Go Repository](https://github.com/temporalio/samples-go)
- [workflow Package Documentation](https://pkg.go.dev/go.temporal.io/sdk/workflow)
- [Temporal Blog: How Many Activities?](https://temporal.io/blog/how-many-activities-should-i-use-in-my-temporal-workflow)
- [Temporal Blog: Managing Long-Running Workflows](https://temporal.io/blog/very-long-running-workflows)
- [Temporal Failures Reference](https://docs.temporal.io/references/failures)
- [Temporal Community: Executing a DAG in a Workflow](https://community.temporal.io/t/executing-a-dag-in-a-workflow/8472)
- [Temporal Community: Workflow for Running a DAG in DSL](https://community.temporal.io/t/workflow-for-running-a-dag-in-dsl/3880)
- [Temporal Community: Fan-Out Parallel Issues](https://community.temporal.io/t/fanning-out-many-small-activities-in-parallel-issues/13734)
- [Temporal Community: Parallel Activities and Cancellation](https://community.temporal.io/t/best-practice-for-parallel-activities-and-cancelation/10981)
- [Temporal Code Exchange: DSL](https://temporal.io/code-exchange/temporal-dsl)
