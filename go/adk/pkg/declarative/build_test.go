package declarative

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"
)

func userMessage(text string) *genai.Content { return genai.NewContentFromText(text, genai.RoleUser) }

// outputs returns the non-nil event outputs in arrival order.
func outputs(events []*session.Event) []any {
	var out []any
	for _, ev := range events {
		if ev.Output != nil {
			out = append(out, ev.Output)
		}
	}
	return out
}

func requestedInput(events []*session.Event) *session.RequestInput {
	for _, ev := range events {
		if ev.RequestedInput != nil {
			return ev.RequestedInput
		}
	}
	return nil
}

func buildShowcase(t *testing.T) *System {
	t.Helper()
	sys, err := Build(t.Context(), loadShowcase(t), testRegistry(t))
	require.NoError(t, err)
	return sys
}

func TestShowcaseRoutesEachBranch(t *testing.T) {
	sys := buildShowcase(t)
	tests := []struct {
		name  string
		input string
		final any
	}{
		{"BUG runs parallel worker then nested workflow", "bug x y",
			"summary:exec(plan([item:bug item:x item:y]))"},
		{"BILLING runs the tool node", "bill me",
			"summary:map[customer:c-42 plan:pro]"},
		{"SUPPORT runs the agent node", "help me", "summary:[stub] help me"},
		{"unmatched route takes DEFAULT", "zzz", "item:zzz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, id := newSession(t)
			got := outputs(run(t, sys.Root(), userMessage(tt.input), svc, id))
			require.Equal(t, tt.final, got[len(got)-1])
		})
	}
}

func TestShowcaseFanOutJoinsEveryBranch(t *testing.T) {
	svc, id := newSession(t)
	got := outputs(run(t, buildShowcase(t).Root(), userMessage("zzz"), svc, id))
	require.Contains(t, got, map[string]any{"branch_a": "a", "branch_b": "b", "branch_c": "c"})
}

func TestShowcaseOtherPausesForApprovalAndResumes(t *testing.T) {
	sys := buildShowcase(t)
	svc, id := newSession(t)

	paused := run(t, sys.Root(), userMessage("other thing"), svc, id)
	req := requestedInput(paused)
	require.NotNil(t, req, "hitl node must request input")
	require.Equal(t, "Confirm final reply before send (yes/no)", req.Message)
	// The dynamic node spent exactly max_iters child runs before the pause.
	require.Contains(t, outputs(paused), "[stub] other thing after 2 runs")
	for _, o := range outputs(paused) {
		require.NotContains(t, fmt.Sprint(o), "summary:", "terminal must not run before approval")
	}

	reply := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
		ID: req.InterruptID, Name: workflow.WorkflowInputFunctionCallName,
		Response: map[string]any{"response": "yes"},
	}}}}
	resumed := run(t, sys.Root(), reply, svc, id)
	got := outputs(resumed)
	require.Equal(t, "summary:yes", got[len(got)-1])
}

func TestSystemAccessors(t *testing.T) {
	sys := buildShowcase(t)
	require.Equal(t, "multi_agent_root", sys.Root().Name())
	require.Equal(t, []string{"multi_agent_root", "specialist_helper", "code_expert", "learning_coordinator", "docs_expert"}, sys.Names())

	coordinator, ok := sys.Agent("learning_coordinator")
	require.True(t, ok)
	require.Equal(t, "docs_expert", coordinator.SubAgents()[0].Name())
	_, ok = sys.Agent("missing")
	require.False(t, ok)

	// An agent used by sub_agents and by an agent node is one shared instance.
	helper, _ := sys.Agent("specialist_helper")
	require.Same(t, helper, sys.Root().SubAgents()[0])
}

func TestBuildNilRegistryUsesDefaults(t *testing.T) {
	spec, err := Parse([]byte(`
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - { name: solo, agent_class: LlmAgent, model: stub, instruction: hi }
`))
	require.NoError(t, err)
	sys, err := Build(t.Context(), spec, nil)
	require.NoError(t, err)
	svc, id := newSession(t)
	require.Contains(t, outputsOrText(run(t, sys.Root(), userMessage("ping"), svc, id)), "[stub] ping")
}

func outputsOrText(events []*session.Event) []string {
	var out []string
	for _, ev := range events {
		if ev.Content != nil {
			for _, p := range ev.Content.Parts {
				if p != nil && p.Text != "" {
					out = append(out, p.Text)
				}
			}
		}
	}
	return out
}

func TestRetryPolicyRerunsFailingFunction(t *testing.T) {
	var calls atomic.Int32
	reg := NewRegistry()
	require.NoError(t, reg.RegisterFunction("flaky", func(agent.Context, any) (Result, error) {
		if calls.Add(1) < 3 {
			return Result{}, errors.New("transient")
		}
		return Output("recovered"), nil
	}))
	spec, err := reg.Parse([]byte(`
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: wf
    agent_class: Workflow
    nodes:
      - id: only
        type: function
        function: flaky
        retry: { max_attempts: 3, initial_delay: 1ms, max_delay: 2ms, backoff_factor: 1, jitter: 0 }
    edges: [{ from: START, to: only }]
`))
	require.NoError(t, err)
	sys, err := Build(t.Context(), spec, reg)
	require.NoError(t, err)

	svc, id := newSession(t)
	require.Equal(t, []any{"recovered"}, outputs(run(t, sys.Root(), userMessage("go"), svc, id)))
	require.EqualValues(t, 3, calls.Load())
}

func TestBuildFailures(t *testing.T) {
	const header = "apiVersion: adk.declarative/v0.2\nkind: AgentSystem\nagents:\n"
	tests := []struct {
		name string
		doc  string
		want string
	}{
		{"unconditional cycle is rejected by the engine", header + `
  - name: wf
    agent_class: Workflow
    nodes:
      - { id: a, type: function, function: prefix }
      - { id: b, type: function, function: prefix }
    edges:
      - { from: START, to: a }
      - { from: a, to: b }
      - { from: b, to: a }`, `agent "wf"`},
		{"fan-in to a non-join node is rejected by the engine", header + `
  - name: wf
    agent_class: Workflow
    nodes:
      - { id: a, type: function, function: prefix }
      - { id: b, type: function, function: prefix }
      - { id: c, type: function, function: prefix }
    edges:
      - { from: START, to: a }
      - { from: START, to: b }
      - { from: a, to: c }
      - { from: b, to: c }`, `agent "wf"`},
		{"task mode agent cannot be a graph node", header + `
  - { name: worker, agent_class: LlmAgent, model: stub, instruction: hi, mode: task }
  - name: wf
    agent_class: Workflow
    nodes: [{ id: n, type: agent, agent: worker }]
    edges: [{ from: START, to: n }]`, `agent "wf"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reg := testRegistry(t)
			spec, err := reg.Parse([]byte(tt.doc))
			require.NoError(t, err, "document must be valid; the failure belongs to Build")
			_, err = Build(t.Context(), spec, reg)
			require.ErrorContains(t, err, tt.want)
			require.False(t, IsValidation(err), "build failures are not document validation failures")
		})
	}
}

func TestBuildRejectsInvalidDocuments(t *testing.T) {
	_, err := Build(t.Context(), &Spec{}, NewRegistry())
	require.True(t, IsValidation(err))
}

func TestModelResolverIsConsultedAfterRegisteredModels(t *testing.T) {
	reg := NewRegistry()
	var asked []string
	reg.SetModelResolver(func(_ context.Context, name string) (adkmodel.LLM, error) {
		asked = append(asked, name)
		if name == "forbidden" {
			return nil, errors.New("denied")
		}
		return NewStubModel(name), nil
	})
	build := func(model string) error {
		spec, err := reg.Parse([]byte(fmt.Sprintf(`
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - { name: a, agent_class: LlmAgent, model: %q, instruction: hi }
`, model)))
		require.NoError(t, err)
		_, err = Build(t.Context(), spec, reg)
		return err
	}
	require.NoError(t, build("stub"))
	require.Empty(t, asked, "registered models win over the resolver")
	require.NoError(t, build("gpt-4o"))
	require.ErrorContains(t, build("forbidden"), "denied")
	require.Equal(t, []string{"gpt-4o", "forbidden"}, asked)
}

func TestCustomNodeType(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.RegisterNodeType("shout", NodeRule{Required: []string{"message"}},
		func(_ *BuildContext, spec *NodeSpec) (workflow.Node, error) {
			return workflow.NewFunctionNode(spec.ID, func(_ agent.Context, in string) (string, error) {
				return in + spec.Message, nil
			}, workflow.NodeConfig{}), nil
		}))
	doc := []byte(`
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: wf
    agent_class: Workflow
    nodes: [{ id: s, type: shout, message: "!" }]
    edges: [{ from: START, to: s }]
`)
	_, err := Parse(doc)
	require.ErrorContains(t, err, `unknown node type "shout"`, "plain Parse does not know custom types")

	spec, err := reg.Parse(doc)
	require.NoError(t, err)
	sys, err := Build(t.Context(), spec, reg)
	require.NoError(t, err)
	svc, id := newSession(t)
	require.Equal(t, []any{"hey!"}, outputs(run(t, sys.Root(), userMessage("hey"), svc, id)))

	bad, err := reg.Parse([]byte(`
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - { name: wf, agent_class: Workflow, nodes: [{ id: s, type: shout }], edges: [{ from: START, to: s }] }
`))
	require.Nil(t, bad)
	require.ErrorContains(t, err, "agents[0].nodes[0].message: required for node type shout")
}

func TestParallelWorkerWrapsTool(t *testing.T) {
	reg := testRegistry(t)
	require.NoError(t, reg.RegisterFunction("split_customers", func(agent.Context, any) (Result, error) {
		return Output([]any{map[string]any{"customer_id": "c-1"}, map[string]any{"customer_id": "c-2"}}), nil
	}))
	spec, err := reg.Parse([]byte(`
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: fanout
    agent_class: Workflow
    nodes:
      - { id: split, type: function, function: split_customers }
      - { id: lookups, type: parallel_worker, tool: lookup_customer }
      - { id: finish, type: function, function: summarize_join }
    edges:
      - { from: START, to: split }
      - { from: split, to: lookups }
      - { from: lookups, to: finish }
`))
	require.NoError(t, err)
	sys, err := Build(t.Context(), spec, reg)
	require.NoError(t, err)
	svc, id := newSession(t)
	got := outputs(run(t, sys.Root(), userMessage("go"), svc, id))
	require.Equal(t, "summary:[map[customer:c-1 plan:pro] map[customer:c-2 plan:pro]]", got[len(got)-1])
}

func TestParallelWorkerWrapsAgent(t *testing.T) {
	reg := testRegistry(t)
	require.NoError(t, reg.RegisterFunction("topics", func(agent.Context, any) (Result, error) {
		return Output([]any{"go", "k8s"}), nil
	}))
	spec, err := reg.Parse([]byte(`
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: fanout
    agent_class: Workflow
    nodes:
      - { id: split, type: function, function: topics }
      - { id: explain, type: parallel_worker, agent: explainer, max_concurrency: 1 }
      - { id: finish, type: function, function: summarize_join }
    edges:
      - { from: START, to: split }
      - { from: split, to: explain }
      - { from: explain, to: finish }
  - { name: explainer, agent_class: LlmAgent, model: stub, mode: single_turn, instruction: Explain it. }
`))
	require.NoError(t, err)
	sys, err := Build(t.Context(), spec, reg)
	require.NoError(t, err)
	svc, id := newSession(t)
	got := outputs(run(t, sys.Root(), userMessage("go"), svc, id))
	require.Equal(t, "summary:[[stub] go [stub] k8s]", got[len(got)-1])
}

func TestCompositeAgents(t *testing.T) {
	data, err := os.ReadFile("testdata/composites.yaml")
	require.NoError(t, err)
	reg := NewRegistry()
	spec, err := reg.Parse(data)
	require.NoError(t, err)
	sys, err := Build(t.Context(), spec, reg)
	require.NoError(t, err)

	authors := func(name string) []string {
		a, ok := sys.Agent(name)
		require.True(t, ok)
		svc, id := newSession(t)
		var out []string
		for _, ev := range run(t, a, userMessage("topic"), svc, id) {
			if ev.Content != nil && ev.Author != "" {
				out = append(out, ev.Author)
			}
		}
		return out
	}
	require.Equal(t, []string{"draft", "review"}, authors("pipeline"), "sequential keeps declaration order")
	require.ElementsMatch(t, []string{"optimist", "pessimist"}, authors("panel"))
	require.Equal(t, []string{"polish", "polish"}, authors("refiner"), "loop stops at max_iterations")
}

func TestTerminalStructuredOutputIsRenderedAsText(t *testing.T) {
	reg := testRegistry(t)
	require.NoError(t, reg.RegisterFunction("report", func(agent.Context, any) (Result, error) {
		return Output(map[string]any{"status": "ok", "count": 2}), nil
	}))
	require.NoError(t, reg.RegisterFunction("pass", func(_ agent.Context, in any) (Result, error) {
		return Output(map[string]any{"mid": in}), nil
	}))
	spec, err := reg.Parse([]byte(`
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: wf
    agent_class: Workflow
    nodes:
      - { id: first, type: function, function: pass }
      - id: nested
        type: workflow
        nodes: [{ id: inner, type: function, function: report }]
        edges: [{ from: START, to: inner }]
    edges:
      - { from: START, to: first }
      - { from: first, to: nested }
`))
	require.NoError(t, err)
	sys, err := Build(t.Context(), spec, reg)
	require.NoError(t, err)

	svc, id := newSession(t)
	var texts []string
	for _, ev := range run(t, sys.Root(), userMessage("go"), svc, id) {
		if ev.Content != nil {
			for _, p := range ev.Content.Parts {
				texts = append(texts, p.Text)
			}
		}
	}
	require.Equal(t, []string{`{"count":2,"status":"ok"}`}, texts,
		"only the terminal node's structured output is shown; the intermediate one stays data")
}

func TestIntermediateStringOutputsStayOutOfTheChat(t *testing.T) {
	reg := testRegistry(t)
	spec, err := reg.Parse([]byte(`
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: wf
    agent_class: Workflow
    nodes:
      - { id: first, type: function, function: prefix }
      - { id: second, type: function, function: prefix }
    edges: [{ from: START, to: first }, { from: first, to: second }]
`))
	require.NoError(t, err)
	sys, err := Build(t.Context(), spec, reg)
	require.NoError(t, err)

	svc, id := newSession(t)
	var texts []string
	for _, ev := range run(t, sys.Root(), userMessage("go"), svc, id) {
		if ev.Content != nil {
			for _, p := range ev.Content.Parts {
				texts = append(texts, p.Text)
			}
		}
	}
	require.Equal(t, []string{"item:item:go"}, texts, "only the terminal result is shown, as with native function nodes")
}

func TestChatModeAgentNodeKeepsItsDeclaredMode(t *testing.T) {
	reg := NewRegistry()
	spec, err := reg.Parse([]byte(`
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: wf
    agent_class: Workflow
    nodes: [{ id: ask, type: agent, agent: coordinator }]
    edges: [{ from: START, to: ask }]
  - { name: coordinator, agent_class: LlmAgent, model: stub, mode: chat, instruction: Coordinate. }
`))
	require.NoError(t, err)
	sys, err := Build(t.Context(), spec, reg)
	require.NoError(t, err)
	svc, id := newSession(t)
	require.Contains(t, outputsOrText(run(t, sys.Root(), userMessage("hi"), svc, id)), "[stub] hi")
}

func TestNodeTimeoutCancelsSlowFunction(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.RegisterFunction("slow", func(ctx agent.Context, _ any) (Result, error) {
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		case <-time.After(5 * time.Second):
			return Output("too late"), nil
		}
	}))
	spec, err := reg.Parse([]byte(`
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: wf
    agent_class: Workflow
    nodes: [{ id: only, type: function, function: slow, timeout: 20ms }]
    edges: [{ from: START, to: only }]
`))
	require.NoError(t, err)
	sys, err := Build(t.Context(), spec, reg)
	require.NoError(t, err)

	svc, id := newSession(t)
	r, err := runner.New(runner.Config{AppName: "test", Agent: sys.Root(), SessionService: svc})
	require.NoError(t, err)
	var runErr error
	for _, err := range r.Run(t.Context(), "user", id, userMessage("go"), agent.RunConfig{}) {
		if err != nil {
			runErr = err
		}
	}
	require.ErrorIs(t, runErr, context.DeadlineExceeded)
}

// capturingModel records the requests it receives.
type capturingModel struct {
	adkmodel.LLM
	requests chan *adkmodel.LLMRequest
}

func (m capturingModel) GenerateContent(ctx context.Context, req *adkmodel.LLMRequest, stream bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	m.requests <- req
	return m.LLM.GenerateContent(ctx, req, stream)
}

func TestLLMOptionsReachTheModel(t *testing.T) {
	reg := NewRegistry()
	model := capturingModel{LLM: NewStubModel("spy"), requests: make(chan *adkmodel.LLMRequest, 4)}
	require.NoError(t, reg.RegisterModel("spy", model))
	spec, err := reg.Parse([]byte(`
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - name: solo
    agent_class: LlmAgent
    model: spy
    instruction: Answer plainly.
    global_instruction: Always be brief.
    output_key: reply
    output_schema:
      type: object
      properties:
        answer: { type: string }
      required: [answer]
`))
	require.NoError(t, err)
	sys, err := Build(t.Context(), spec, reg)
	require.NoError(t, err)

	svc, id := newSession(t)
	run(t, sys.Root(), userMessage("hi"), svc, id)
	req := <-model.requests
	instruction := req.Config.SystemInstruction.Parts[0].Text
	require.Contains(t, instruction, "Always be brief.")
	require.Contains(t, instruction, "Answer plainly.")
	require.NotNil(t, req.Config.ResponseSchema, "output_schema must reach the model request")
	require.Equal(t, "application/json", req.Config.ResponseMIMEType)
	require.Contains(t, req.Config.ResponseSchema.Required, "answer")
}

func TestSystemLoader(t *testing.T) {
	sys := buildShowcase(t)
	loader, err := sys.Loader()
	require.NoError(t, err)
	require.Equal(t, "multi_agent_root", loader.RootAgent().Name())
	require.ElementsMatch(t, sys.Names(), loader.ListAgents())
	a, err := loader.LoadAgent("learning_coordinator")
	require.NoError(t, err)
	require.Equal(t, "learning_coordinator", a.Name())
}

func TestStubModel(t *testing.T) {
	m := NewStubModel("stub-x")
	require.Equal(t, "stub-x", m.Name())
	next, stop := iter.Pull2(m.GenerateContent(t.Context(), &adkmodel.LLMRequest{Contents: []*genai.Content{
		genai.NewContentFromText("first", genai.RoleUser),
		genai.NewContentFromText("reply", genai.RoleModel),
		genai.NewContentFromText("last", genai.RoleUser),
	}}, false))
	defer stop()
	resp, err, ok := next()
	require.True(t, ok)
	require.NoError(t, err)
	require.Equal(t, "[stub-x] last", resp.Content.Parts[0].Text)
	require.True(t, resp.TurnComplete)

	next2, stop2 := iter.Pull2(m.GenerateContent(t.Context(), nil, false))
	defer stop2()
	resp, _, _ = next2()
	require.Equal(t, "[stub-x] ", resp.Content.Parts[0].Text)
}

func TestOutputTextFallsBackWhenJSONFails(t *testing.T) {
	require.Equal(t, "plain", outputText("plain"))
	require.Equal(t, `{"a":1}`, outputText(map[string]any{"a": 1}))
	require.Equal(t, fmt.Sprint(make(chan int))[:2], outputText(make(chan int))[:2], "unmarshalable values degrade to fmt")
}
