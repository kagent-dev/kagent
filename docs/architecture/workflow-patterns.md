# Workflow Design Patterns

Each pattern below is written twice: with the Go ADK API (`google.golang.org/adk/v2/workflow`, described in the [ADK graph documentation](https://adk.dev/graphs/)) and as a declarative `adk.declarative/v0.2` document (see [Declarative workflow agents](workflow-agents.md)). The code is not illustrative: it lives in [`go/adk/examples/workflowpatterns`](../../go/adk/examples/workflowpatterns), the tests run both versions on the same inputs and require the same events, and a test fails when this page is out of date.

Run any pattern, in either form or both, from `go/`:

```sh
go run ./adk/examples/workflowpatterns -list
go run ./adk/examples/workflowpatterns -pattern router -input "bug: crash"
go run ./adk/examples/workflowpatterns -pattern human_approval -input "ship it" -reply yes
go run ./adk/examples/workflowpatterns -pattern fan_in -input Go -mode yaml
go test ./adk/examples/workflowpatterns
```

The models are the offline stub, so nothing needs credentials.

## Building blocks

Both columns use the same behavior, so only the wiring differs. The Go column wraps a plain function with `step` or `router`; the YAML column names the same function, which the registry exposes through `textFunc` and `routeFunc`. The behaviors themselves (`clean`, `upper`, `classify`, ...) are one-liners in `helpers.go`.

```go
// step wraps a text function as a graph node.
func step(name string, f func(string) string) workflow.Node {
	return workflow.NewFunctionNode(name, func(_ agent.Context, in string) (string, error) {
		return f(in), nil
	}, workflow.NodeConfig{})
}
```

```go
// router wraps a routing decision as a node. It forwards its input unchanged
// and emits the routes that decide which outgoing edges run.
func router(name string, decide func(string) []string) workflow.Node {
	return workflow.NewEmittingFunctionNode(name, func(ctx agent.Context, in string, emit func(*session.Event) error) (any, error) {
		ev := session.NewEvent(ctx, ctx.InvocationID())
		ev.Routes = decide(in)
		ev.Output = in
		return nil, emit(ev)
	}, workflow.NodeConfig{})
}
```

```go
// textFunc registers a text function for function nodes.
func textFunc(f func(string) string) declarative.Func {
	return func(_ agent.Context, in any) (declarative.Result, error) {
		return declarative.Output(f(fmt.Sprint(in))), nil
	}
}
```

```go
// routeFunc registers a routing decision: the input is forwarded unchanged
// and the chosen routes select the outgoing edges.
func routeFunc(f func(string) []string) declarative.Func {
	return func(_ agent.Context, in any) (declarative.Result, error) {
		return declarative.Route(in, f(fmt.Sprint(in))...), nil
	}
}
```

## Patterns

Every row is built twice and run by the tests; the Go and YAML versions must produce identical results. The last line of each Pattern cell is one verified run.

<table>
<thead><tr><th>Pattern</th><th>Go (native ADK)</th><th>YAML (declarative)</th></tr></thead>
<tbody>
<tr>
<td valign="top">

**Sequence (pipeline)**

Run steps one after another; each step's output is the next step's input.

```text
START → clean → upper → exclaim
```

Run: `"  hello "` → `out:HELLO!`

</td>
<td valign="top">

```go
func sequenceGo() (agent.Agent, error) {
	return workflowagent.New(workflowagent.Config{
		Name:  "sequence",
		Edges: workflow.Chain(workflow.Start, step("clean", clean), step("upper", upper), step("exclaim", exclaim)),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: sequence
    agent_class: Workflow
    nodes:
      - { id: clean, type: function, function: clean }
      - { id: upper, type: function, function: upper }
      - { id: exclaim, type: function, function: exclaim }
    edges:
      - { from: START, to: clean }
      - { from: clean, to: upper }
      - { from: upper, to: exclaim }
```

</td>
</tr>
<tr>
<td valign="top">

**Router (content-based, with default)**

A classifier picks exactly one branch by route; DEFAULT catches everything else.

```text
START → classify ─BUG→ bug
                  ├BILLING→ billing
                  └DEFAULT→ fallback
```

Run: `"bug: crash"` → `out:bug handled: bug: crash`

</td>
<td valign="top">

```go
func routerGo() (agent.Agent, error) {
	classifier := router("classify", classify)
	bug, billing, other := step("bug", bugHandler), step("billing", billingHandler), step("fallback", fallbackReply)
	return workflowagent.New(workflowagent.Config{
		Name: "router",
		Edges: workflow.Concat(
			workflow.Chain(workflow.Start, classifier),
			[]workflow.Edge{
				{From: classifier, To: bug, Route: workflow.StringRoute("BUG")},
				{From: classifier, To: billing, Route: workflow.StringRoute("BILLING")},
				{From: classifier, To: other, Route: workflow.Default},
			},
		),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: router
    agent_class: Workflow
    nodes:
      - { id: classify, type: function, function: classify }
      - { id: bug, type: function, function: bug_handler }
      - { id: billing, type: function, function: billing_handler }
      - { id: fallback, type: function, function: fallback_reply }
    edges:
      - { from: START, to: classify }
      - { from: classify, to: bug, route: BUG }
      - { from: classify, to: billing, route: BILLING }
      - { from: classify, to: fallback, route: DEFAULT }
```

</td>
</tr>
<tr>
<td valign="top">

**Conditional (if / else)**

A boolean decision sends the message down one of two branches. Go uses BoolRoute; YAML uses the strings "true" and "false", which is what BoolRoute matches.

```text
START → is_long ─true→ truncate
                 └false→ keep
```

Run: `"short"` → `out:short`

</td>
<td valign="top">

```go
func conditionalGo() (agent.Agent, error) {
	check := router("is_long", isLong)
	short, keep := step("truncate", truncate), step("keep", clean)
	return workflowagent.New(workflowagent.Config{
		Name: "conditional",
		Edges: workflow.Concat(
			workflow.Chain(workflow.Start, check),
			[]workflow.Edge{
				{From: check, To: short, Route: workflow.BoolRoute(true)},
				{From: check, To: keep, Route: workflow.BoolRoute(false)},
			},
		),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: conditional
    agent_class: Workflow
    nodes:
      - { id: is_long, type: function, function: is_long }
      - { id: truncate, type: function, function: truncate }
      - { id: keep, type: function, function: clean }
    edges:
      - { from: START, to: is_long }
      - { from: is_long, to: truncate, route: "true" }
      - { from: is_long, to: keep, route: "false" }
```

</td>
</tr>
<tr>
<td valign="top">

**Fan-out and fan-in (scatter-gather)**

Several unconditional edges leave one node and its branches run concurrently. A join node is the barrier that waits for all of them and hands their outputs, keyed by node, to one aggregator.

```text
START → shout   ─┐
      ├→ whisper ─┼→ gather → combine
      └→ mirror  ─┘
```

Run: `"Go"` → `out:mirror=oG|shout=GO|whisper=go`

</td>
<td valign="top">

```go
func fanInGo() (agent.Agent, error) {
	shout, whisper, mirror := step("shout", upper), step("whisper", lower), step("mirror", reverse)
	gather := workflow.NewJoinNode("gather")
	merge := workflow.NewFunctionNode("combine", func(_ agent.Context, in map[string]any) (string, error) {
		return combine(in), nil
	}, workflow.NodeConfig{})
	return workflowagent.New(workflowagent.Config{
		Name: "fan_in",
		Edges: workflow.NewEdgeBuilder().
			AddFanOut(workflow.Start, shout, whisper, mirror).
			AddFanIn(gather, shout, whisper, mirror).
			Add(gather, merge).
			Build(),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: fan_in
    agent_class: Workflow
    nodes:
      - { id: shout, type: function, function: upper }
      - { id: whisper, type: function, function: lower }
      - { id: mirror, type: function, function: reverse }
      - { id: gather, type: join }
      - { id: combine, type: function, function: combine }
    edges:
      - { from: START, to: shout }
      - { from: START, to: whisper }
      - { from: START, to: mirror }
      - { from: shout, to: gather }
      - { from: whisper, to: gather }
      - { from: mirror, to: gather }
      - { from: gather, to: combine }
```

</td>
</tr>
<tr>
<td valign="top">

**Map-reduce (dynamic fan-out)**

A parallel worker runs one node per item of a list at run time, then a reducer combines the results. Fan-out width is data-dependent.

```text
START → split → shout_all (×N items) → reduce
```

Run: `"a b c"` → `out:A B C`

</td>
<td valign="top">

```go
func mapReduceGo() (agent.Agent, error) {
	split := workflow.NewFunctionNode("split", func(_ agent.Context, in string) ([]any, error) {
		return words(in), nil
	}, workflow.NodeConfig{})
	shoutWord := workflow.NewFunctionNode("shout_word", func(_ agent.Context, in string) (string, error) {
		return upper(in), nil
	}, workflow.NodeConfig{})
	shoutAll, err := workflow.NewParallelWorker("shout_all", shoutWord, 2, workflow.NodeConfig{})
	if err != nil {
		return nil, err
	}
	reduce := workflow.NewFunctionNode("reduce", func(_ agent.Context, in []any) (string, error) {
		return joinWords(in), nil
	}, workflow.NodeConfig{})
	return workflowagent.New(workflowagent.Config{
		Name:  "map_reduce",
		Edges: workflow.Chain(workflow.Start, split, shoutAll, reduce),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: map_reduce
    agent_class: Workflow
    nodes:
      - { id: split, type: function, function: split }
      - { id: shout_all, type: parallel_worker, function: upper, max_concurrency: 2 }
      - { id: reduce, type: function, function: join_words }
    edges:
      - { from: START, to: split }
      - { from: split, to: shout_all }
      - { from: shout_all, to: reduce }
```

</td>
</tr>
<tr>
<td valign="top">

**Loop until done (conditional cycle)**

A routed edge points back to an earlier node. A cycle is allowed as long as it contains a conditional edge, so it can end.

```text
START → grow → check ─again→ grow
                   └done→ finish
```

Run: `"ab"` → `out:final: ab!!!`

</td>
<td valign="top">

```go
func retryLoopGo() (agent.Agent, error) {
	grow, check, finish := step("grow", exclaim), router("check", longEnough), step("finish", finalReport)
	return workflowagent.New(workflowagent.Config{
		Name: "retry_loop",
		Edges: workflow.Concat(
			workflow.Chain(workflow.Start, grow, check),
			[]workflow.Edge{
				{From: check, To: grow, Route: workflow.StringRoute("again")},
				{From: check, To: finish, Route: workflow.StringRoute("done")},
			},
		),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: retry_loop
    agent_class: Workflow
    nodes:
      - { id: grow, type: function, function: exclaim }
      - { id: check, type: function, function: long_enough }
      - { id: finish, type: function, function: final_report }
    edges:
      - { from: START, to: grow }
      - { from: grow, to: check }
      - { from: check, to: grow, route: again }
      - { from: check, to: finish, route: done }
```

</td>
</tr>
<tr>
<td valign="top">

**Human approval gate (pause / resume)**

A hitl node pauses the run and asks a person; the reply resumes it and can drive a router.

```text
START → draft → approve ⏸ → verdict ─APPROVED→ publish
                             └REJECTED→ reject
```

Run: `"ship it"`, reply `"yes"` → `out:published after reply: yes`

</td>
<td valign="top">

```go
func humanApprovalGo() (agent.Agent, error) {
	draft := step("draft", draftText)
	rerun := true
	approve := workflow.NewEmittingFunctionNode("approve", func(ctx agent.Context, _ any, emit func(*session.Event) error) (any, error) {
		return workflow.ResumeOrRequestInput(ctx, emit, session.RequestInput{
			InterruptID: "approve-" + ctx.InvocationID(),
			Message:     "Publish this draft?",
		})
	}, workflow.NodeConfig{RerunOnResume: &rerun})
	decide := router("verdict", verdict)
	publish, reject := step("publish", publisher), step("reject", rejecter)
	return workflowagent.New(workflowagent.Config{
		Name: "human_approval",
		Edges: workflow.Concat(
			workflow.Chain(workflow.Start, draft, approve, decide),
			[]workflow.Edge{
				{From: decide, To: publish, Route: workflow.StringRoute("APPROVED")},
				{From: decide, To: reject, Route: workflow.StringRoute("REJECTED")},
			},
		),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: human_approval
    agent_class: Workflow
    nodes:
      - { id: draft, type: function, function: draft_text }
      - { id: approve, type: hitl, message: "Publish this draft?" }
      - { id: verdict, type: function, function: verdict }
      - { id: publish, type: function, function: publisher }
      - { id: reject, type: function, function: rejecter }
    edges:
      - { from: START, to: draft }
      - { from: draft, to: approve }
      - { from: approve, to: verdict }
      - { from: verdict, to: publish, route: APPROVED }
      - { from: verdict, to: reject, route: REJECTED }
```

</td>
</tr>
<tr>
<td valign="top">

**Nested workflow (sub-workflow as a node)**

A whole graph with its own START is one node of an outer graph, so a sub-flow can be reused and reasoned about alone.

```text
START → prep → subflow[ START → plan → execute ] → finish
```

Run: `" x "` → `out:exec(plan(x))!`

</td>
<td valign="top">

```go
func nestedGo() (agent.Agent, error) {
	inner, err := workflow.NewWorkflowNode("subflow", workflow.Chain(workflow.Start, step("plan", plan), step("execute", execute)))
	if err != nil {
		return nil, err
	}
	return workflowagent.New(workflowagent.Config{
		Name:  "nested",
		Edges: workflow.Chain(workflow.Start, step("prep", clean), inner, step("finish", exclaim)),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: nested
    agent_class: Workflow
    nodes:
      - { id: prep, type: function, function: clean }
      - id: subflow
        type: workflow
        nodes:
          - { id: plan, type: function, function: plan }
          - { id: execute, type: function, function: execute }
        edges:
          - { from: START, to: plan }
          - { from: plan, to: execute }
      - { id: finish, type: function, function: exclaim }
    edges:
      - { from: START, to: prep }
      - { from: prep, to: subflow }
      - { from: subflow, to: finish }
```

</td>
</tr>
<tr>
<td valign="top">

**Dynamic orchestrator (bounded child runs)**

A dynamic node runs a child node as many times as its code decides, under a hard iteration budget. In YAML the budget is max_iters and the child is declared in children.

```text
START → prep → refine (runs critic ≤ 3×) → finish
```

Run: `" x "` → `out:critic(critic(critic(x)))!`

</td>
<td valign="top">

```go
func dynamicGo() (agent.Agent, error) {
	review := step("critic", critic)
	refine := workflow.NewDynamicNode("refine", func(ctx agent.Context, in any, _ func(*session.Event) error) (any, error) {
		out := in
		for range 3 {
			next, err := workflow.RunNode[any](ctx, review, out)
			if err != nil {
				return nil, err
			}
			out = next
		}
		return out, nil
	}, workflow.NodeConfig{})
	return workflowagent.New(workflowagent.Config{
		Name:  "dynamic",
		Edges: workflow.Chain(workflow.Start, step("prep", clean), refine, step("finish", exclaim)),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: dynamic
    agent_class: Workflow
    nodes:
      - { id: prep, type: function, function: clean }
      - { id: refine, type: dynamic, function: refine, max_iters: 3, children: [critic] }
      - { id: critic, type: function, function: critic }
      - { id: finish, type: function, function: exclaim }
    edges:
      - { from: START, to: prep }
      - { from: prep, to: refine }
      - { from: refine, to: finish }
```

</td>
</tr>
<tr>
<td valign="top">

**LLM stage inside a graph**

A model-backed agent is one step of a deterministic graph. The offline stub model echoes its input, so the run needs no credentials.

```text
START → prep → answer (LLM) → finish
```

Run: `"hi"` → `out:[stub] hi!`

</td>
<td valign="top">

```go
func llmStageGo() (agent.Agent, error) {
	answer, err := llmagent.New(llmagent.Config{
		Name:        "answer",
		Instruction: "Answer briefly.",
		Model:       declarative.NewStubModel("stub"),
		Mode:        llmagent.ModeSingleTurn,
	})
	if err != nil {
		return nil, err
	}
	answerNode, err := workflow.NewAgentNode(answer, workflow.NodeConfig{})
	if err != nil {
		return nil, err
	}
	return workflowagent.New(workflowagent.Config{
		Name:  "llm_stage",
		Edges: workflow.Chain(workflow.Start, step("prep", clean), answerNode, step("finish", exclaim)),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: llm_stage
    agent_class: Workflow
    nodes:
      - { id: prep, type: function, function: clean }
      - { id: answer, type: llm, model: stub, instruction: Answer briefly. }
      - { id: finish, type: function, function: exclaim }
    edges:
      - { from: START, to: prep }
      - { from: prep, to: answer }
      - { from: answer, to: finish }
```

</td>
</tr>
<tr>
<td valign="top">

**Tool stage inside a graph**

A tool runs as a graph node: the previous output is its arguments and its result feeds the next step.

```text
START → args → lookup (tool) → describe
```

Run: `"c-7"` → `out:customer c-7 is on plan pro`

</td>
<td valign="top">

```go
func toolStageGo() (agent.Agent, error) {
	lookup, err := newLookupTool()
	if err != nil {
		return nil, err
	}
	lookupNode, err := workflow.NewNamedToolNode("lookup", lookup, workflow.NodeConfig{})
	if err != nil {
		return nil, err
	}
	args := workflow.NewFunctionNode("args", func(_ agent.Context, in string) (map[string]any, error) {
		return customerArgs(in), nil
	}, workflow.NodeConfig{})
	describe := workflow.NewFunctionNode("describe", func(_ agent.Context, in map[string]any) (string, error) {
		return describeCustomer(in), nil
	}, workflow.NodeConfig{})
	return workflowagent.New(workflowagent.Config{
		Name:  "tool_stage",
		Edges: workflow.Chain(workflow.Start, args, lookupNode, describe),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: tool_stage
    agent_class: Workflow
    nodes:
      - { id: args, type: function, function: customer_args }
      - { id: lookup, type: tool, tool: lookup_customer }
      - { id: describe, type: function, function: describe_customer }
    edges:
      - { from: START, to: args }
      - { from: args, to: lookup }
      - { from: lookup, to: describe }
```

</td>
</tr>
<tr>
<td valign="top">

**Typed contract between steps (JSON Schema)**

A node can carry an input and an output JSON Schema. The engine validates what the node returns and what the next node receives, and compares the two schemas on every edge when it builds the graph.

```text
START → parse (output: {name}) → greet (input: {name})
```

Run: `"Ada"` → `out:hello Ada`

</td>
<td valign="top">

```go
func typedContractGo() (agent.Agent, error) {
	parse, err := workflow.NewFunctionNodeWithSchema(
		"parse", func(_ agent.Context, in string) (map[string]any, error) { return makePerson(in), nil },
		nil, personSchema, workflow.NodeConfig{})
	if err != nil {
		return nil, err
	}
	greet, err := workflow.NewFunctionNodeWithSchema(
		"greet", func(_ agent.Context, in map[string]any) (string, error) { return greetPerson(in), nil },
		personSchema, nil, workflow.NodeConfig{})
	if err != nil {
		return nil, err
	}
	return workflowagent.New(workflowagent.Config{
		Name:  "typed_contract",
		Edges: workflow.Chain(workflow.Start, parse, greet),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: typed_contract
    agent_class: Workflow
    nodes:
      - id: parse
        type: function
        function: make_person
        output_schema: { type: object, properties: { name: { type: string } }, required: [name] }
      - id: greet
        type: function
        function: greet_person
        input_schema: { type: object, properties: { name: { type: string } }, required: [name] }
    edges:
      - { from: START, to: parse }
      - { from: parse, to: greet }
```

</td>
</tr>
<tr>
<td valign="top">

**Resilient step (retry and timeout)**

A node carries a retry policy and a timeout. The step below fails twice, then succeeds on the third attempt.

```text
START → unreliable (retry ×3, timeout 5s)
```

Run: `"go"` → `out:recovered: go`

</td>
<td valign="top">

```go
func resilientGo() (agent.Agent, error) {
	retry := workflow.DefaultRetryConfig()
	retry.MaxAttempts = 3
	retry.InitialDelay, retry.MaxDelay = time.Millisecond, 2*time.Millisecond
	retry.BackoffFactor, retry.Jitter = 1, 0
	flaky := newFlaky(2)
	unreliable := workflow.NewFunctionNode("unreliable", func(_ agent.Context, in string) (string, error) {
		return flaky(in)
	}, workflow.NodeConfig{RetryConfig: retry, Timeout: 5 * time.Second})
	return workflowagent.New(workflowagent.Config{
		Name:  "resilient",
		Edges: workflow.Chain(workflow.Start, unreliable),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: resilient
    agent_class: Workflow
    nodes:
      - id: unreliable
        type: function
        function: flaky
        timeout: 5s
        retry: { max_attempts: 3, initial_delay: 1ms, max_delay: 2ms, backoff_factor: 1, jitter: 0 }
    edges:
      - { from: START, to: unreliable }
```

</td>
</tr>
<tr>
<td valign="top">

**Fan-out of agents (isolated by default)**

Concurrent branches get their own branch of the session: neither agent is shown the other's reply, so both answer saw=1 (just the user's message). Sibling isolation is automatic for static fan-out, parallel workers and ParallelAgent.

```text
START → left  ─┐
      └→ right ─┴→ gather → combine
```

Run: `"hi"` → `out:left=saw=1|right=saw=1`

</td>
<td valign="top">

```go
func fanOutAgentsGo() (agent.Agent, error) {
	left, err := chatAgent("left")
	if err != nil {
		return nil, err
	}
	right, err := chatAgent("right")
	if err != nil {
		return nil, err
	}
	leftNode, err := workflow.NewAgentNode(left, workflow.NodeConfig{})
	if err != nil {
		return nil, err
	}
	rightNode, err := workflow.NewAgentNode(right, workflow.NodeConfig{})
	if err != nil {
		return nil, err
	}
	gather := workflow.NewJoinNode("gather")
	merge := workflow.NewFunctionNode("combine", func(_ agent.Context, in map[string]any) (string, error) {
		return combine(in), nil
	}, workflow.NodeConfig{})
	return workflowagent.New(workflowagent.Config{
		Name: "fan_out_agents",
		Edges: workflow.NewEdgeBuilder().
			AddFanOut(workflow.Start, leftNode, rightNode).
			AddFanIn(gather, leftNode, rightNode).
			Add(gather, merge).
			Build(),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: fan_out_agents
    agent_class: Workflow
    nodes:
      - { id: ask_left, type: agent, agent: left }
      - { id: ask_right, type: agent, agent: right }
      - { id: gather, type: join }
      - { id: combine, type: function, function: combine }
    edges:
      - { from: START, to: ask_left }
      - { from: START, to: ask_right }
      - { from: ask_left, to: gather }
      - { from: ask_right, to: gather }
      - { from: gather, to: combine }
  - { name: left, agent_class: LlmAgent, model: history, mode: chat, instruction: Answer. }
  - { name: right, agent_class: LlmAgent, model: history, mode: chat, instruction: Answer. }
```

</td>
</tr>
<tr>
<td valign="top">

**Dynamic children sharing the conversation**

By default a dynamic node runs its children on its own branch, so the second child is shown what the first one said (saw=3 against saw=1).

```text
START → review (runs first, then second, shared)
```

Run: `"hi"` → `out:first saw=1, second saw=3`

</td>
<td valign="top">

```go
func sharedChildrenGo() (agent.Agent, error) { return reviewersGo("shared_children") }
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: shared_children
    agent_class: Workflow
    nodes:
      - { id: review, type: dynamic, function: review_both, children: [ask_first, ask_second] }
      - { id: ask_first, type: agent, agent: first }
      - { id: ask_second, type: agent, agent: second }
    edges:
      - { from: START, to: review }
  - { name: first, agent_class: LlmAgent, model: history, mode: chat, instruction: Answer. }
  - { name: second, agent_class: LlmAgent, model: history, mode: chat, instruction: Answer. }
```

</td>
</tr>
<tr>
<td valign="top">

**Dynamic children isolated from each other**

isolation: branch (WithUseSubBranch in Go) gives every child run its own branch, so the second child is back to saw=1 and no longer sees the first one's reply. Use isolation: scope (WithIsolationScopeFromNodePath) to hide ancestor events too.

```text
START → review (runs first, then second, each on its own branch)
```

Run: `"hi"` → `out:first saw=1, second saw=1`

</td>
<td valign="top">

```go
func isolatedChildrenGo() (agent.Agent, error) {
	return reviewersGo("isolated_children", workflow.WithUseSubBranch())
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: isolated_children
    agent_class: Workflow
    nodes:
      - { id: review, type: dynamic, function: review_both, children: [ask_first, ask_second], isolation: branch }
      - { id: ask_first, type: agent, agent: first }
      - { id: ask_second, type: agent, agent: second }
    edges:
      - { from: START, to: review }
  - { name: first, agent_class: LlmAgent, model: history, mode: chat, instruction: Answer. }
  - { name: second, agent_class: LlmAgent, model: history, mode: chat, instruction: Answer. }
```

</td>
</tr>
<tr>
<td valign="top">

**Agent as a tool (a session of its own)**

agent_tools (agenttool in Go) runs the agent in a new session, seeded with a copy of the caller's state, so it sees only the request it was given (saw=1) and parallel calls cannot interfere. A sub_agent, by contrast, shares the caller's session.

```text
caller ──call──▶ specialist (own session)
```

Run: `"question"` → `say:agent_as_tool:tool said: saw=1`

</td>
<td valign="top">

```go
func agentAsToolGo() (agent.Agent, error) {
	specialist, err := llmagent.New(llmagent.Config{
		Name: "specialist", Description: "Answers one question.", Instruction: "Answer.", Model: historyModel{},
	})
	if err != nil {
		return nil, err
	}
	return llmagent.New(llmagent.Config{
		Name:        "agent_as_tool",
		Instruction: "Delegate to the specialist.",
		Model:       callerModel{tool: "specialist"},
		Tools:       []tool.Tool{agenttool.New(specialist, nil)},
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: agent_as_tool
    agent_class: LlmAgent
    model: caller
    instruction: Delegate to the specialist.
    agent_tools: [specialist]
  - { name: specialist, agent_class: LlmAgent, model: history, description: Answers one question., instruction: Answer. }
```

</td>
</tr>
<tr>
<td valign="top">

**Sequential agents**

The classic ADK SequentialAgent runs its sub-agents in order; each sees the conversation so far, including what earlier agents said.

```text
draft → review
```

Run: `"topic"` → `say:review:[stub] For context:[draft] said: [stub] topic`

</td>
<td valign="top">

```go
func sequentialAgentGo() (agent.Agent, error) {
	draft, err := worker("draft", "Write a draft.")
	if err != nil {
		return nil, err
	}
	review, err := worker("review", "Review the draft.")
	if err != nil {
		return nil, err
	}
	return sequentialagent.New(sequentialagent.Config{AgentConfig: agent.Config{
		Name: "sequential_agent", SubAgents: []agent.Agent{draft, review},
	}})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: sequential_agent
    agent_class: SequentialAgent
    sub_agents: [draft, review]
  - { name: draft, agent_class: LlmAgent, model: stub, instruction: Write a draft. }
  - { name: review, agent_class: LlmAgent, model: stub, instruction: Review the draft. }
```

</td>
</tr>
<tr>
<td valign="top">

**Parallel agents (ensemble)**

The classic ADK ParallelAgent runs its sub-agents concurrently in isolated branches.

```text
optimist ∥ pessimist
```

Run: `"topic"` → `say:pessimist:[stub] topic`

</td>
<td valign="top">

```go
func parallelAgentGo() (agent.Agent, error) {
	optimist, err := worker("optimist", "Argue for it.")
	if err != nil {
		return nil, err
	}
	pessimist, err := worker("pessimist", "Argue against it.")
	if err != nil {
		return nil, err
	}
	return parallelagent.New(parallelagent.Config{AgentConfig: agent.Config{
		Name: "parallel_agent", SubAgents: []agent.Agent{optimist, pessimist},
	}})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: parallel_agent
    agent_class: ParallelAgent
    sub_agents: [optimist, pessimist]
  - { name: optimist, agent_class: LlmAgent, model: stub, instruction: Argue for it. }
  - { name: pessimist, agent_class: LlmAgent, model: stub, instruction: Argue against it. }
```

</td>
</tr>
<tr>
<td valign="top">

**Loop agent (bounded repetition)**

The classic ADK LoopAgent repeats its sub-agents until max_iterations or until one escalates.

```text
polish ×2
```

Run: `"topic"` → `say:polish:[stub] topic`

</td>
<td valign="top">

```go
func loopAgentGo() (agent.Agent, error) {
	polish, err := worker("polish", "Improve the text.")
	if err != nil {
		return nil, err
	}
	return loopagent.New(loopagent.Config{
		AgentConfig:   agent.Config{Name: "loop_agent", SubAgents: []agent.Agent{polish}},
		MaxIterations: 2,
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: loop_agent
    agent_class: LoopAgent
    max_iterations: 2
    sub_agents: [polish]
  - { name: polish, agent_class: LlmAgent, model: stub, instruction: Improve the text. }
```

</td>
</tr>
</tbody>
</table>

## Rejected graphs

These look reasonable and fail. Each failure is asserted for both versions, so the YAML validation does not hide a graph the Go API accepts, or the reverse.

<table>
<thead><tr><th>Anti-pattern</th><th>Go (native ADK)</th><th>YAML (declarative)</th></tr></thead>
<tbody>
<tr>
<td valign="top">

**Unconditional cycle**

An edge without a route that points back never ends. Put a routed edge in the cycle, as in the loop pattern.

Fails when built with:

```text
…unconditional cycle…
```

</td>
<td valign="top">

```go
func unconditionalCycleGo() (agent.Agent, error) {
	first, second := step("first", clean), step("second", upper)
	return workflowagent.New(workflowagent.Config{
		Name: "unconditional_cycle",
		Edges: workflow.Concat(
			workflow.Chain(workflow.Start, first, second),
			[]workflow.Edge{{From: second, To: first}},
		),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: unconditional_cycle
    agent_class: Workflow
    nodes:
      - { id: first, type: function, function: clean }
      - { id: second, type: function, function: upper }
    edges:
      - { from: START, to: first }
      - { from: first, to: second }
      - { from: second, to: first }
```

</td>
</tr>
<tr>
<td valign="top">

**Mutually exclusive routes converging on one node**

Only a join may receive several incoming edges. Give each route its own terminal, or end both with the same logic in separate nodes.

Fails when built with:

```text
…non-JoinNode fan-in…
```

</td>
<td valign="top">

```go
func convergingRoutesGo() (agent.Agent, error) {
	classifier := router("classify", classify)
	bug, billing, reply := step("bug", bugHandler), step("billing", billingHandler), step("reply", finalReport)
	return workflowagent.New(workflowagent.Config{
		Name: "converging_routes",
		Edges: workflow.Concat(
			workflow.Chain(workflow.Start, classifier),
			[]workflow.Edge{
				{From: classifier, To: bug, Route: workflow.StringRoute("BUG")},
				{From: classifier, To: billing, Route: workflow.StringRoute("BILLING")},
				{From: bug, To: reply},
				{From: billing, To: reply},
			},
		),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: converging_routes
    agent_class: Workflow
    nodes:
      - { id: classify, type: function, function: classify }
      - { id: bug, type: function, function: bug_handler }
      - { id: billing, type: function, function: billing_handler }
      - { id: reply, type: function, function: final_report }
    edges:
      - { from: START, to: classify }
      - { from: classify, to: bug, route: BUG }
      - { from: classify, to: billing, route: BILLING }
      - { from: bug, to: reply }
      - { from: billing, to: reply }
```

</td>
</tr>
<tr>
<td valign="top">

**Fan-out without a gather**

A workflow has one result. Two or more terminal nodes that produce output fail at run time; converge the branches with a join.

Fails when run with:

```text
…multiple terminal nodes produced output…
```

</td>
<td valign="top">

```go
func fanOutWithoutGatherGo() (agent.Agent, error) {
	shout, whisper, mirror := step("shout", upper), step("whisper", lower), step("mirror", reverse)
	return workflowagent.New(workflowagent.Config{
		Name:  "fan_out_without_gather",
		Edges: workflow.NewEdgeBuilder().AddFanOut(workflow.Start, shout, whisper, mirror).Build(),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: fan_out_without_gather
    agent_class: Workflow
    nodes:
      - { id: shout, type: function, function: upper }
      - { id: whisper, type: function, function: lower }
      - { id: mirror, type: function, function: reverse }
    edges:
      - { from: START, to: shout }
      - { from: START, to: whisper }
      - { from: START, to: mirror }
```

</td>
</tr>
<tr>
<td valign="top">

**Multicast routes without a gather**

A router that emits several routes starts several branches, which hits the same one-result rule. Only multicast to branches that finish without output, or end the branches in a join.

Fails when run with:

```text
…multiple terminal nodes produced output…
```

</td>
<td valign="top">

```go
func multicastWithoutGatherGo() (agent.Agent, error) {
	pickAudience := router("audience", audience)
	audit, notify := step("audit", auditor), step("notify", notifier)
	return workflowagent.New(workflowagent.Config{
		Name: "multicast_without_gather",
		Edges: workflow.Concat(
			workflow.Chain(workflow.Start, pickAudience),
			[]workflow.Edge{
				{From: pickAudience, To: audit, Route: workflow.StringRoute("audit")},
				{From: pickAudience, To: notify, Route: workflow.StringRoute("notify")},
			},
		),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: multicast_without_gather
    agent_class: Workflow
    nodes:
      - { id: audience, type: function, function: audience }
      - { id: audit, type: function, function: auditor }
      - { id: notify, type: function, function: notifier }
    edges:
      - { from: START, to: audience }
      - { from: audience, to: audit, route: audit }
      - { from: audience, to: notify, route: notify }
```

</td>
</tr>
<tr>
<td valign="top">

**Task-mode agent as a graph node**

A task-mode agent is multi-turn and cannot be a static graph node. Use single_turn, or run it dynamically.

Fails when built with:

```text
…task…
```

</td>
<td valign="top">

```go
func taskModeNodeGo() (agent.Agent, error) {
	task, err := llmagent.New(llmagent.Config{
		Name: "worker", Instruction: "Do the task.", Model: declarative.NewStubModel("stub"), Mode: llmagent.ModeTask,
	})
	if err != nil {
		return nil, err
	}
	node, err := workflow.NewAgentNode(task, workflow.NodeConfig{})
	if err != nil {
		return nil, err
	}
	return workflowagent.New(workflowagent.Config{
		Name:  "task_mode_node",
		Edges: workflow.Chain(workflow.Start, node),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: task_mode_node
    agent_class: Workflow
    nodes:
      - { id: work, type: agent, agent: worker }
    edges:
      - { from: START, to: work }
  - { name: worker, agent_class: LlmAgent, model: stub, mode: task, instruction: Do the task. }
```

</td>
</tr>
<tr>
<td valign="top">

**Schemas that disagree across an edge**

A step that promises a string cannot feed a step that requires an integer. The engine compares the schemas when it builds the graph, so the mistake never reaches a run.

Fails when built with:

```text
…schema mismatch…
```

</td>
<td valign="top">

```go
func schemaMismatchGo() (agent.Agent, error) {
	produce, err := workflow.NewFunctionNodeWithSchema(
		"produce", func(_ agent.Context, in string) (string, error) { return in, nil },
		nil, &jsonschema.Schema{Type: "string"}, workflow.NodeConfig{})
	if err != nil {
		return nil, err
	}
	consume, err := workflow.NewFunctionNodeWithSchema(
		"consume", func(_ agent.Context, in int) (string, error) { return fmt.Sprint(in), nil },
		&jsonschema.Schema{Type: "integer"}, nil, workflow.NodeConfig{})
	if err != nil {
		return nil, err
	}
	return workflowagent.New(workflowagent.Config{
		Name:  "schema_mismatch",
		Edges: workflow.Chain(workflow.Start, produce, consume),
	})
}
```

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: schema_mismatch
    agent_class: Workflow
    nodes:
      - { id: produce, type: function, function: clean, output_schema: { type: string } }
      - { id: consume, type: function, function: clean, input_schema: { type: integer } }
    edges:
      - { from: START, to: produce }
      - { from: produce, to: consume }
```

</td>
</tr>
<tr>
<td valign="top">

**Ending a branch on a tool node**

The ADK accepts it, but the tool's structured result is data that no A2A client displays. The declarative validation asks for a closing function node.

Fails when built with:

```text
…terminal tool node emits structured output…
```

</td>
<td valign="top">

_The ADK Go API accepts this graph; only the declarative validation refuses it._

</td>
<td valign="top">

```yaml
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: terminal_tool
    agent_class: Workflow
    nodes:
      - { id: args, type: function, function: customer_args }
      - { id: lookup, type: tool, tool: lookup_customer }
    edges:
      - { from: START, to: args }
      - { from: args, to: lookup }
```

</td>
</tr>
</tbody>
</table>

## Session isolation

Concurrent branches and subagents differ in how much of the session they can see. There are four levels, and each boundary has a default and a control. The first two rows were measured against ADK v2.6.0 by `isolation_test.go` in `pkg/declarative`; a change upstream fails those tests.

| Boundary | Sees the others' history by default | Control |
| --- | --- | --- |
| Static fan-out (several edges from one node) | no, own branch | none: always branch-isolated |
| `parallel_worker` items | no, own branch | none: always branch-isolated |
| `ParallelAgent` sub-agents | no, own branch | none: always branch-isolated |
| `SequentialAgent` / `LoopAgent` sub-agents | yes, by design | none that hides earlier agents; run them as `single_turn` agent nodes in a graph, or as `agent_tools` |
| Children of a `dynamic` node | yes | `isolation: shared | branch | scope` on the node |
| `sub_agents` of an `LlmAgent` (transfer) | yes, same session | none; call it as an `agent_tools` entry instead |
| `single_turn` agent node in a graph | no, sees only its predecessor's output | the mode is the control |
| `agent_tools` of an `LlmAgent` | no, a separate session | the agent is the control: use `agent_tools` instead of `sub_agents` |

- **shared**: the child runs on its parent's branch and sees the whole conversation.
- **branch**: the child has its own branch. It sees its ancestors' history, not its siblings'.
- **scope**: the child also gets a scope unique to the run, so it sees only its own events. The user's message and the input you hand it still arrive; earlier agents' replies do not.
- **session** (`agent_tools`): the agent runs in a new session seeded with a copy of the caller's state. It never sees the caller's history, parallel calls get one session each, and its state writes are not copied back.

The ADK graph pages do not describe branch or scope isolation, so this matrix is measured, not quoted. `include_contents: none` is not isolation. It keeps the current turn, and the replies of agents that ran earlier in that turn are part of it, so a sequential sub-agent still receives what the one before it said (and loses the user's message).

Isolation covers history, not state. Branches share one session state, so two concurrent agents that write the same `output_key` race; validation rejects that for `ParallelAgent`, for unconditional fan-out successors, and for `parallel_worker` agents. Remember that a tool agent starts from a copy of the caller's state, so do not keep secrets there that the tool agent should not see.

## Reading the table

- **One result.** A workflow ends in exactly one terminal output. The ADK graph pages do not state this, but the engine enforces it: branches that run concurrently must converge in a `join`, or all but one must finish without output.
- **Joins need outputs.** A join releases only after all its predecessors finish, and a predecessor that finishes without an output leaves the join no value for that branch. The failure shows up downstream, away from the node that caused it, so give every branch an output and a retry policy where it can fail.
- **Cycles are not bounded.** A routed back-edge re-activates a node on each pass and nothing stops it, so the exit condition must eventually hold. When you need a hard bound, use a `dynamic` node (`max_iters`) or a `LoopAgent` (`max_iterations`).
- **Agent nodes.** The ADK documents graph agents as `single_turn` or `task`. The engine also accepts `chat` for a node fed directly by START and for a dynamic child, and rejects a static `task` node; a task agent is for dynamic dispatch. The isolation patterns use `chat` on purpose, because a `single_turn` node already sees only its input.
- **Data between steps.** Pass data through node outputs, not session state. State is for values that tools, callbacks and instruction templates read, and it should stay small. Give a step an `input_schema` or `output_schema` when you want the engine to enforce the contract.
- **Routes are strings.** `BoolRoute(true)` and `IntRoute(1)` match the strings `"true"` and `"1"`, which is how YAML writes them. `DEFAULT` is the ADK `Default` route.
- **Plain steps are silent.** A function node emits data for its successors. The terminal result is also rendered as message text, so A2A clients display the answer and not every intermediate step.
- **Names.** A sub-workflow node cannot share the root agent's name, and an agent node is named after its agent.
- **Where YAML stops.** YAML names behavior; it cannot contain it. Functions, tools, and models come from a `Registry`, and a custom node type is Go code registered once.
