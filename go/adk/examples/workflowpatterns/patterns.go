package main

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/agent/workflowagent"
	"google.golang.org/adk/v2/agent/workflowagents/loopagent"
	"google.golang.org/adk/v2/agent/workflowagents/parallelagent"
	"google.golang.org/adk/v2/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/agenttool"
	"google.golang.org/adk/v2/tool/mcptoolset"
	"google.golang.org/adk/v2/workflow"

	"github.com/kagent-dev/kagent/go/adk/pkg/declarative"
)

// Each function below builds one pattern with the ADK Go API directly. The
// YAML twin lives in patterns/<name>.yaml and builds the same graph through
// pkg/declarative; the tests require both to behave identically.

func sequenceGo() (agent.Agent, error) {
	return workflowagent.New(workflowagent.Config{
		Name:  "sequence",
		Edges: workflow.Chain(workflow.Start, step("clean", clean), step("upper", upper), step("exclaim", exclaim)),
	})
}

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

func fanInGo() (agent.Agent, error) {
	shout, whisper, mirror := step("shout", upper), step("whisper", lower), step("mirror", reverse)
	gather := workflow.NewJoinNode("gather")
	merge := workflow.NewFunctionNode("combine", func(_ agent.Context, in map[string]any) (string, error) {
		return combine(in), nil
	}, workflow.NodeConfig{})
	// workflowagent.Config takes no engine options, so cap the graph with
	// workflow.New and run it from a plain agent.
	wf, err := workflow.New("fan_in", workflow.NewEdgeBuilder().
		AddFanOut(workflow.Start, shout, whisper, mirror).
		AddFanIn(gather, shout, whisper, mirror).
		Add(gather, merge).
		Build(), workflow.WithMaxConcurrency(2))
	if err != nil {
		return nil, err
	}
	return agent.New(agent.Config{Name: "fan_in", Run: wf.Run})
}

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

// personSchema is the contract between the typed_contract steps.
var personSchema = &jsonschema.Schema{
	Type:       "object",
	Properties: map[string]*jsonschema.Schema{"name": {Type: "string"}},
	Required:   []string{"name"},
}

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

func resilientGo() (agent.Agent, error) {
	retry := workflow.DefaultRetryConfig()
	retry.MaxAttempts = 3
	retry.InitialDelay, retry.MaxDelay = time.Millisecond, 2*time.Millisecond
	retry.BackoffFactor, retry.Jitter = 1, 0
	retry.ShouldRetry = func(err error) bool {
		return errors.Is(err, context.DeadlineExceeded) || strings.Contains(err.Error(), "transient")
	}
	flaky := newFlaky(2)
	unreliable := workflow.NewFunctionNode("unreliable", func(_ agent.Context, in string) (string, error) {
		return flaky(in)
	}, workflow.NodeConfig{RetryConfig: retry, Timeout: 5 * time.Second})
	return workflowagent.New(workflowagent.Config{
		Name:  "resilient",
		Edges: workflow.Chain(workflow.Start, unreliable),
	})
}

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

// Anti-patterns: graphs the engine or the declarative validation rejects.

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

func fanOutWithoutGatherGo() (agent.Agent, error) {
	shout, whisper, mirror := step("shout", upper), step("whisper", lower), step("mirror", reverse)
	return workflowagent.New(workflowagent.Config{
		Name:  "fan_out_without_gather",
		Edges: workflow.NewEdgeBuilder().AddFanOut(workflow.Start, shout, whisper, mirror).Build(),
	})
}

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

// Session isolation patterns. The history model answers "saw=N", the number of
// conversation text parts it was shown, so the trace shows what each agent
// could see.

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

// reviewersGo builds a dynamic node that runs two chat agents in turn. opts
// decide how much of the session the second one is shown.
func reviewersGo(name string, opts ...workflow.RunNodeOption) (agent.Agent, error) {
	first, err := chatAgent("first")
	if err != nil {
		return nil, err
	}
	second, err := chatAgent("second")
	if err != nil {
		return nil, err
	}
	firstNode, err := workflow.NewAgentNode(first, workflow.NodeConfig{})
	if err != nil {
		return nil, err
	}
	secondNode, err := workflow.NewAgentNode(second, workflow.NodeConfig{})
	if err != nil {
		return nil, err
	}
	review := workflow.NewDynamicNode(name, func(ctx agent.Context, in any, _ func(*session.Event) error) (any, error) {
		a, err := workflow.RunNode[any](ctx, firstNode, in, opts...)
		if err != nil {
			return nil, err
		}
		b, err := workflow.RunNode[any](ctx, secondNode, in, opts...)
		if err != nil {
			return nil, err
		}
		return fmt.Sprintf("first %v, second %v", a, b), nil
	}, workflow.NodeConfig{})
	return workflowagent.New(workflowagent.Config{
		Name:  name,
		Edges: workflow.Chain(workflow.Start, review),
	})
}

func sharedChildrenGo() (agent.Agent, error) { return reviewersGo("shared_children") }

func isolatedChildrenGo() (agent.Agent, error) {
	return reviewersGo("isolated_children", workflow.WithUseSubBranch())
}

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

var (
	bugWords     = regexp.MustCompile(`\b(crash|error|bug)\b`)
	billingWords = regexp.MustCompile(`\b(invoice|refund)\b`)
	customerIDs  = regexp.MustCompile(`\bc-[0-9]+\b`)
)

func celRouterGo() (agent.Agent, error) {
	classify := workflow.NewEmittingFunctionNode("classify", func(ctx agent.Context, in string, emit func(*session.Event) error) (any, error) {
		ev := session.NewEvent(ctx, ctx.InvocationID())
		switch t := strings.ToLower(in); {
		case bugWords.MatchString(t):
			ev.Routes = []string{"BUG"}
		case billingWords.MatchString(t):
			ev.Routes = []string{"BILLING"}
		default:
			ev.Routes = []string{"OTHER"}
		}
		ev.Actions.StateDelta = map[string]any{"ids": customerIDs.FindAllString(in, -1)}
		ev.Output = in
		return nil, emit(ev)
	}, workflow.NodeConfig{})
	bug := step("bug", func(s string) string { return "bug: " + strings.TrimSpace(s) })
	other := step("other", func(s string) string { return "other: " + strings.TrimSpace(s) })
	billing := workflow.NewFunctionNode("billing", func(ctx agent.Context, _ string) (string, error) {
		ids, _ := ctx.State().Get("ids")
		list, _ := ids.([]string)
		if len(list) == 0 {
			return "billing for unknown customer", nil
		}
		return "billing for " + strings.Join(list, ","), nil
	}, workflow.NodeConfig{})
	return workflowagent.New(workflowagent.Config{
		Name: "cel_router",
		Edges: workflow.Concat(
			workflow.Chain(workflow.Start, classify),
			[]workflow.Edge{
				{From: classify, To: bug, Route: workflow.StringRoute("BUG")},
				{From: classify, To: billing, Route: workflow.StringRoute("BILLING")},
				{From: classify, To: other, Route: workflow.Default},
			},
		),
	})
}

func mcpToolGo() (agent.Agent, error) {
	url, err := crmURL()
	if err != nil {
		return nil, err
	}
	toolset, err := mcptoolset.New(mcptoolset.Config{Transport: &mcpsdk.StreamableClientTransport{Endpoint: url}})
	if err != nil {
		return nil, err
	}
	crm := tool.FilterToolset(toolset, tool.StringPredicate([]string{"lookup_customer"}))
	args := workflow.NewFunctionNode("args", func(_ agent.Context, in string) (map[string]any, error) {
		return map[string]any{"customer_id": strings.TrimSpace(in)}, nil
	}, workflow.NodeConfig{})
	lookup := workflow.NewFunctionNode("lookup", func(ctx agent.Context, in map[string]any) (map[string]any, error) {
		tools, err := crm.Tools(ctx)
		if err != nil || len(tools) != 1 {
			return nil, fmt.Errorf("lookup_customer is not available: %v", err)
		}
		runnable, ok := tools[0].(interface {
			Run(agent.Context, any) (map[string]any, error)
		})
		if !ok {
			return nil, errors.New("lookup_customer cannot run outside a model turn")
		}
		return runnable.Run(ctx, in)
	}, workflow.NodeConfig{})
	describe := workflow.NewFunctionNode("describe", func(_ agent.Context, in map[string]any) (string, error) {
		c, _ := in["output"].(map[string]any)
		return fmt.Sprintf("customer %v is on plan %v", c["id"], c["plan"]), nil
	}, workflow.NodeConfig{})
	return workflowagent.New(workflowagent.Config{
		Name:  "mcp_tool",
		Edges: workflow.Chain(workflow.Start, args, lookup, describe),
	})
}
