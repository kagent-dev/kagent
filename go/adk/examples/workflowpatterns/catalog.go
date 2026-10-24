package main

import (
	"context"
	"embed"
	"fmt"
	"path"
	"reflect"
	"runtime"
	"slices"
	"strings"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/memory"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"

	"github.com/kagent-dev/kagent/go/adk/pkg/declarative"
)

//go:embed patterns/*.yaml anti/*.yaml
var patternFiles embed.FS

// Pattern is one workflow design pattern written twice: with the ADK Go API
// and as a declarative YAML document.
type Pattern struct {
	Name    string
	Title   string
	Summary string
	// Shape sketches the graph.
	Shape string
	// Go builds the pattern with the ADK Go API.
	Go func() (agent.Agent, error)
	// Ordered patterns compare traces in event order; others as sorted sets,
	// because branches run concurrently.
	Ordered bool
	Cases   []Case
}

// Case is one run of a pattern.
type Case struct {
	Input string
	// Replies answer, in order, the human-input requests the run raises.
	Replies []string
	// Want lists trace lines that must appear; Absent lists ones that must not.
	Want   []string
	Absent []string
}

// YAML returns the pattern's declarative document.
func (p Pattern) YAML() (string, error) {
	data, err := patternFiles.ReadFile(path.Join("patterns", p.Name+".yaml"))
	return string(data), err
}

// GoFuncName is the name of the Go builder, used to quote its source.
func (p Pattern) GoFuncName() string {
	full := runtime.FuncForPC(reflect.ValueOf(p.Go).Pointer()).Name()
	return full[strings.LastIndex(full, ".")+1:]
}

// BuildYAML builds the pattern from its YAML document.
func (p Pattern) BuildYAML(ctx context.Context) (agent.Agent, error) {
	doc, err := p.YAML()
	if err != nil {
		return nil, err
	}
	reg, err := newRegistry()
	if err != nil {
		return nil, err
	}
	spec, err := reg.Parse([]byte(doc))
	if err != nil {
		return nil, fmt.Errorf("failed to parse %s.yaml: %w", p.Name, err)
	}
	system, err := declarative.Build(ctx, spec, reg)
	if err != nil {
		return nil, fmt.Errorf("failed to build %s.yaml: %w", p.Name, err)
	}
	return system.Root(), nil
}

// Run executes root once for input, answering human-input requests with
// replies, and returns its trace.
func Run(ctx context.Context, root agent.Agent, input string, replies []string) ([]string, error) {
	sessions := session.InMemoryService()
	created, err := sessions.Create(ctx, &session.CreateRequest{AppName: "patterns", UserID: "user"})
	if err != nil {
		return nil, err
	}
	r, err := runner.New(runner.Config{AppName: "patterns", Agent: root, SessionService: sessions, MemoryService: memory.InMemoryService()})
	if err != nil {
		return nil, err
	}

	var trace []string
	message := genai.NewContentFromText(input, genai.RoleUser)
	for {
		var pending *session.RequestInput
		for ev, err := range r.Run(ctx, "user", created.Session.ID(), message, agent.RunConfig{}) {
			if err != nil {
				return trace, err
			}
			if ev.RequestedInput != nil {
				pending = ev.RequestedInput
			}
			trace = append(trace, traceLine(ev))
		}
		if pending == nil {
			break
		}
		if len(replies) == 0 {
			return trace, fmt.Errorf("run is waiting for input %q and no reply is left", pending.Message)
		}
		message = &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
			ID: pending.InterruptID, Name: workflow.WorkflowInputFunctionCallName,
			Response: map[string]any{"response": replies[0]},
		}}}}
		replies = replies[1:]
	}
	return slices.DeleteFunc(trace, func(line string) bool { return line == "" }), nil
}

// traceLine summarizes an event as what a node produced or an agent said.
func traceLine(ev *session.Event) string {
	switch {
	case ev.RequestedInput != nil:
		return "ask:" + ev.RequestedInput.Message
	case ev.Output != nil:
		return "out:" + fmt.Sprint(ev.Output)
	case ev.Content != nil:
		var text strings.Builder
		for _, part := range ev.Content.Parts {
			if part != nil {
				text.WriteString(part.Text)
			}
		}
		if text.Len() > 0 {
			return "say:" + ev.Author + ":" + text.String()
		}
	}
	return ""
}

// Normalize orders a trace for comparison unless the pattern is ordered.
func (p Pattern) Normalize(trace []string) []string {
	trace = slices.Clone(trace)
	if !p.Ordered {
		slices.Sort(trace)
	}
	return trace
}

// Catalog lists every pattern in presentation order.
func Catalog() []Pattern { return catalog }

// Find returns the named pattern.
func Find(name string) (Pattern, bool) {
	for _, p := range catalog {
		if p.Name == name {
			return p, true
		}
	}
	return Pattern{}, false
}

var catalog = []Pattern{
	{
		Name: "sequence", Title: "Sequence (pipeline)", Go: sequenceGo, Ordered: true,
		Summary: "Run steps one after another; each step's output is the next step's input.",
		Shape:   "START → clean → upper → exclaim",
		Cases:   []Case{{Input: "  hello ", Want: []string{"out:HELLO!"}}},
	},
	{
		Name: "router", Title: "Router (content-based, with default)", Go: routerGo,
		Summary: "A classifier picks exactly one branch by route; DEFAULT catches everything else.",
		Shape:   "START → classify ─BUG→ bug\n                  ├BILLING→ billing\n                  └DEFAULT→ fallback",
		Cases: []Case{
			{Input: "bug: crash", Want: []string{"out:bug handled: bug: crash"}, Absent: []string{"out:fallback: bug: crash"}},
			{Input: "billing: invoice", Want: []string{"out:billing handled: billing: invoice"}},
			{Input: "hello", Want: []string{"out:fallback: hello"}},
		},
	},
	{
		Name: "conditional", Title: "Conditional (if / else)", Go: conditionalGo,
		Summary: "A boolean decision sends the message down one of two branches. Go uses BoolRoute; YAML uses the strings \"true\" and \"false\", which is what BoolRoute matches.",
		Shape:   "START → is_long ─true→ truncate\n                 └false→ keep",
		Cases: []Case{
			{Input: "short", Want: []string{"out:short"}, Absent: []string{"out:short..."}},
			{Input: "a very long message", Want: []string{"out:a very lon..."}},
		},
	},
	{
		Name: "fan_in", Title: "Fan-out and fan-in (scatter-gather)", Go: fanInGo,
		Summary: "Several unconditional edges leave one node and its branches run concurrently. A join node is the barrier that waits for all of them and hands their outputs, keyed by node, to one aggregator.",
		Shape:   "START → shout   ─┐\n      ├→ whisper ─┼→ gather → combine\n      └→ mirror  ─┘",
		Cases:   []Case{{Input: "Go", Want: []string{"out:mirror=oG|shout=GO|whisper=go"}}},
	},
	{
		Name: "map_reduce", Title: "Map-reduce (dynamic fan-out)", Go: mapReduceGo, Ordered: true,
		Summary: "A parallel worker runs one node per item of a list at run time, then a reducer combines the results. Fan-out width is data-dependent.",
		Shape:   "START → split → shout_all (×N items) → reduce",
		Cases:   []Case{{Input: "a b c", Want: []string{"out:A B C"}}},
	},
	{
		Name: "retry_loop", Title: "Loop until done (conditional cycle)", Go: retryLoopGo, Ordered: true,
		Summary: "A routed edge points back to an earlier node. A cycle is allowed as long as it contains a conditional edge, so it can end.",
		Shape:   "START → grow → check ─again→ grow\n                   └done→ finish",
		Cases:   []Case{{Input: "ab", Want: []string{"out:final: ab!!!"}}},
	},
	{
		Name: "human_approval", Title: "Human approval gate (pause / resume)", Go: humanApprovalGo, Ordered: true,
		Summary: "A hitl node pauses the run and asks a person; the reply resumes it and can drive a router.",
		Shape:   "START → draft → approve ⏸ → verdict ─APPROVED→ publish\n                             └REJECTED→ reject",
		Cases: []Case{
			{Input: "ship it", Replies: []string{"yes"}, Want: []string{"ask:Publish this draft?", "out:published after reply: yes"}, Absent: []string{"out:rejected after reply: yes"}},
			{Input: "ship it", Replies: []string{"no"}, Want: []string{"ask:Publish this draft?", "out:rejected after reply: no"}},
		},
	},
	{
		Name: "nested", Title: "Nested workflow (sub-workflow as a node)", Go: nestedGo, Ordered: true,
		Summary: "A whole graph with its own START is one node of an outer graph, so a sub-flow can be reused and reasoned about alone.",
		Shape:   "START → prep → subflow[ START → plan → execute ] → finish",
		Cases:   []Case{{Input: " x ", Want: []string{"out:exec(plan(x))!"}}},
	},
	{
		Name: "dynamic", Title: "Dynamic orchestrator (bounded child runs)", Go: dynamicGo, Ordered: true,
		Summary: "A dynamic node runs a child node as many times as its code decides, under a hard iteration budget. In YAML the budget is max_iters and the child is declared in children.",
		Shape:   "START → prep → refine (runs critic ≤ 3×) → finish",
		Cases:   []Case{{Input: " x ", Want: []string{"out:critic(critic(critic(x)))!"}}},
	},
	{
		Name: "llm_stage", Title: "LLM stage inside a graph", Go: llmStageGo, Ordered: true,
		Summary: "A model-backed agent is one step of a deterministic graph. The offline stub model echoes its input, so the run needs no credentials.",
		Shape:   "START → prep → answer (LLM) → finish",
		Cases:   []Case{{Input: "hi", Want: []string{"say:answer:[stub] hi", "out:[stub] hi!"}}},
	},
	{
		Name: "tool_stage", Title: "Tool stage inside a graph", Go: toolStageGo, Ordered: true,
		Summary: "A tool runs as a graph node: the previous output is its arguments and its result feeds the next step.",
		Shape:   "START → args → lookup (tool) → describe",
		Cases:   []Case{{Input: "c-7", Want: []string{"out:customer c-7 is on plan pro"}}},
	},
	{
		Name: "typed_contract", Title: "Typed contract between steps (JSON Schema)", Go: typedContractGo, Ordered: true,
		Summary: "A node can carry an input and an output JSON Schema. The engine validates what the node returns and what the next node receives, and compares the two schemas on every edge when it builds the graph.",
		Shape:   "START → parse (output: {name}) → greet (input: {name})",
		Cases:   []Case{{Input: "Ada", Want: []string{"out:hello Ada"}}},
	},
	{
		Name: "resilient", Title: "Resilient step (retry and timeout)", Go: resilientGo, Ordered: true,
		Summary: "A node carries a retry policy and a timeout. The step below fails twice, then succeeds on the third attempt.",
		Shape:   "START → unreliable (retry ×3, timeout 5s)",
		Cases:   []Case{{Input: "go", Want: []string{"out:recovered: go"}}},
	},
	{
		Name: "fan_out_agents", Title: "Fan-out of agents (isolated by default)", Go: fanOutAgentsGo,
		Summary: "Concurrent branches get their own branch of the session: neither agent is shown the other's reply, so both answer saw=1 (just the user's message). Sibling isolation is automatic for static fan-out, parallel workers and ParallelAgent.",
		Shape:   "START → left  ─┐\n      └→ right ─┴→ gather → combine",
		Cases:   []Case{{Input: "hi", Want: []string{"out:left=saw=1|right=saw=1"}}},
	},
	{
		Name: "shared_children", Title: "Dynamic children sharing the conversation", Go: sharedChildrenGo, Ordered: true,
		Summary: "By default a dynamic node runs its children on its own branch, so the second child is shown what the first one said (saw=3 against saw=1).",
		Shape:   "START → review (runs first, then second, shared)",
		Cases:   []Case{{Input: "hi", Want: []string{"out:first saw=1, second saw=3"}}},
	},
	{
		Name: "isolated_children", Title: "Dynamic children isolated from each other", Go: isolatedChildrenGo, Ordered: true,
		Summary: "isolation: branch (WithUseSubBranch in Go) gives every child run its own branch, so the second child is back to saw=1 and no longer sees the first one's reply. Use isolation: scope (WithIsolationScopeFromNodePath) to hide ancestor events too.",
		Shape:   "START → review (runs first, then second, each on its own branch)",
		Cases:   []Case{{Input: "hi", Want: []string{"out:first saw=1, second saw=1"}}},
	},
	{
		Name: "agent_as_tool", Title: "Agent as a tool (a session of its own)", Go: agentAsToolGo, Ordered: true,
		Summary: "agent_tools (agenttool in Go) runs the agent in a new session, seeded with a copy of the caller's state, so it sees only the request it was given (saw=1) and parallel calls cannot interfere. A sub_agent, by contrast, shares the caller's session.",
		Shape:   "caller ──call──▶ specialist (own session)",
		Cases:   []Case{{Input: "question", Want: []string{"say:agent_as_tool:tool said: saw=1"}}},
	},
	{
		Name: "sequential_agent", Title: "Sequential agents", Go: sequentialAgentGo, Ordered: true,
		Summary: "The classic ADK SequentialAgent runs its sub-agents in order; each sees the conversation so far, including what earlier agents said.",
		Shape:   "draft → review",
		Cases:   []Case{{Input: "topic", Want: []string{"say:draft:[stub] topic", "say:review:[stub] For context:[draft] said: [stub] topic"}}},
	},
	{
		Name: "parallel_agent", Title: "Parallel agents (ensemble)", Go: parallelAgentGo,
		Summary: "The classic ADK ParallelAgent runs its sub-agents concurrently in isolated branches.",
		Shape:   "optimist ∥ pessimist",
		Cases:   []Case{{Input: "topic", Want: []string{"say:optimist:[stub] topic", "say:pessimist:[stub] topic"}}},
	},
	{
		Name: "loop_agent", Title: "Loop agent (bounded repetition)", Go: loopAgentGo, Ordered: true,
		Summary: "The classic ADK LoopAgent repeats its sub-agents until max_iterations or until one escalates.",
		Shape:   "polish ×2",
		Cases:   []Case{{Input: "topic", Want: []string{"say:polish:[stub] topic", "say:polish:[stub] topic"}}},
	},
}

// AntiPattern is a graph that looks reasonable but is rejected. The Go and
// YAML versions must fail the same way.
type AntiPattern struct {
	Name  string
	Title string
	Why   string
	Go    func() (agent.Agent, error)
	// GoRejects is false when the ADK Go API accepts the graph and only the
	// declarative validation refuses it.
	GoRejects bool
	// AtRun is true when the failure only shows when the graph runs.
	AtRun   bool
	Input   string
	WantErr string
}

// YAML returns the anti-pattern's declarative document.
func (a AntiPattern) YAML() (string, error) {
	data, err := patternFiles.ReadFile(path.Join("anti", a.Name+".yaml"))
	return string(data), err
}

// GoFuncName is the name of the Go builder, used to quote its source.
func (a AntiPattern) GoFuncName() string {
	if a.Go == nil {
		return ""
	}
	full := runtime.FuncForPC(reflect.ValueOf(a.Go).Pointer()).Name()
	return full[strings.LastIndex(full, ".")+1:]
}

// BuildYAML builds the document, which may be rejected.
func (a AntiPattern) BuildYAML(ctx context.Context) (agent.Agent, error) {
	doc, err := a.YAML()
	if err != nil {
		return nil, err
	}
	reg, err := newRegistry()
	if err != nil {
		return nil, err
	}
	spec, err := reg.Parse([]byte(doc))
	if err != nil {
		return nil, err
	}
	system, err := declarative.Build(ctx, spec, reg)
	if err != nil {
		return nil, err
	}
	return system.Root(), nil
}

// AntiPatterns lists the rejected graphs in presentation order.
func AntiPatterns() []AntiPattern { return antiPatterns }

var antiPatterns = []AntiPattern{
	{
		Name: "unconditional_cycle", Title: "Unconditional cycle", Go: unconditionalCycleGo, GoRejects: true,
		Why:     "An edge without a route that points back never ends. Put a routed edge in the cycle, as in the loop pattern.",
		WantErr: "unconditional cycle",
	},
	{
		Name: "converging_routes", Title: "Mutually exclusive routes converging on one node", Go: convergingRoutesGo, GoRejects: true,
		Why:     "Only a join may receive several incoming edges. Give each route its own terminal, or end both with the same logic in separate nodes.",
		WantErr: "non-JoinNode fan-in",
	},
	{
		Name: "fan_out_without_gather", Title: "Fan-out without a gather", Go: fanOutWithoutGatherGo, GoRejects: true, AtRun: true,
		Why:     "A workflow has one result. Two or more terminal nodes that produce output fail at run time; converge the branches with a join.",
		Input:   "Go",
		WantErr: "multiple terminal nodes produced output",
	},
	{
		Name: "multicast_without_gather", Title: "Multicast routes without a gather", Go: multicastWithoutGatherGo, GoRejects: true, AtRun: true,
		Why:     "A router that emits several routes starts several branches, which hits the same one-result rule. Only multicast to branches that finish without output, or end the branches in a join.",
		Input:   "urgent: x",
		WantErr: "multiple terminal nodes produced output",
	},
	{
		Name: "task_mode_node", Title: "Task-mode agent as a graph node", Go: taskModeNodeGo, GoRejects: true,
		Why:     "A task-mode agent is multi-turn and cannot be a static graph node. Use single_turn, or run it dynamically.",
		WantErr: "task",
	},
	{
		Name: "schema_mismatch", Title: "Schemas that disagree across an edge", Go: schemaMismatchGo, GoRejects: true,
		Why:     "A step that promises a string cannot feed a step that requires an integer. The engine compares the schemas when it builds the graph, so the mistake never reaches a run.",
		WantErr: "schema mismatch",
	},
	{
		Name: "terminal_tool", Title: "Ending a branch on a tool node", Go: nil,
		Why:     "The ADK accepts it, but the tool's structured result is data that no A2A client displays. The declarative validation asks for a closing function node.",
		WantErr: "terminal tool node emits structured output",
	},
}
