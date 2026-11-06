package declarative

import (
	"context"
	"fmt"
	"iter"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

// probeModel replies with a fixed marker and records what each call was shown.
// When after is set the call waits for that probe's first reply, so a reader
// that is not isolated would have the other agent's reply in its history.
type probeModel struct {
	name  string
	after *probeModel
	done  chan struct{}
	once  sync.Once

	mu   sync.Mutex
	seen []string
}

func newProbe(name string, after *probeModel) *probeModel {
	return &probeModel{name: name, after: after, done: make(chan struct{})}
}

func (p *probeModel) marker() string { return "REPLY-" + strings.ToUpper(p.name) }
func (p *probeModel) Name() string   { return p.name }

func (p *probeModel) GenerateContent(_ context.Context, req *adkmodel.LLMRequest, _ bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		if p.after != nil {
			select {
			case <-p.after.done:
			case <-time.After(2 * time.Second):
			}
		}
		p.mu.Lock()
		p.seen = append(p.seen, promptText(req))
		p.mu.Unlock()
		p.once.Do(func() { close(p.done) })
		yield(&adkmodel.LLMResponse{Content: genai.NewContentFromText(p.marker(), genai.RoleModel), TurnComplete: true}, nil)
	}
}

func promptText(req *adkmodel.LLMRequest) string {
	var sb strings.Builder
	for _, c := range req.Contents {
		for _, part := range c.Parts {
			if part != nil {
				sb.WriteString(part.Text + "\n")
			}
		}
	}
	return sb.String()
}

func (p *probeModel) calls() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.seen...)
}

// sawAnyOf reports whether any call was shown text containing marker.
func (p *probeModel) sawAnyOf(marker string) bool {
	for _, s := range p.calls() {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}

func buildWith(t *testing.T, doc string, setup func(*Registry), probes ...*probeModel) *System {
	t.Helper()
	reg := testRegistry(t)
	for _, p := range probes {
		require.NoError(t, reg.RegisterModel(p.name, p))
	}
	if setup != nil {
		setup(reg)
	}
	spec, err := reg.Parse([]byte(doc))
	require.NoError(t, err)
	sys, err := Build(t.Context(), spec, reg)
	require.NoError(t, err)
	return sys
}

func runOnce(t *testing.T, sys *System, input string) {
	t.Helper()
	svc, id := newSession(t)
	run(t, sys.Root(), userMessage(input), svc, id)
}

const (
	docHead = "apiVersion: adk.declarative/v0.2\nkind: AgentSystem\nagents:\n"
	agentA  = "  - { name: agent_a, agent_class: LlmAgent, model: probe_a, mode: chat, instruction: A }\n"
	agentB  = "  - { name: agent_b, agent_class: LlmAgent, model: probe_b, mode: chat, instruction: B }\n"
)

// TestIsolationLevels records, for each way of running two agents, whether the
// second one is shown the first one's reply. It documents the ADK defaults
// the isolation controls build on, so a change upstream fails here.
func TestIsolationLevels(t *testing.T) {
	dynamicDoc := func(isolation string) string {
		return docHead + `  - name: wf
    agent_class: Workflow
    nodes:
      - { id: orchestrate, type: dynamic, function: two_children, children: [child_a, child_b]` + isolation + ` }
      - { id: child_a, type: agent, agent: agent_a }
      - { id: child_b, type: agent, agent: agent_b }
      - { id: end, type: function, function: summarize_join }
    edges:
      - { from: START, to: orchestrate }
      - { from: orchestrate, to: end }
` + agentA + agentB
	}
	twoChildren := func(reg *Registry) {
		require.NoError(t, reg.RegisterDynamic("two_children", func(_ agent.Context, in any, env *DynamicEnv) (any, error) {
			if _, err := env.RunChild("child_a", in); err != nil {
				return nil, err
			}
			return env.RunChild("child_b", in)
		}))
	}

	tests := []struct {
		name   string
		doc    string
		setup  func(*Registry)
		bSeesA bool
	}{
		{
			name: "static fan-out is branch-isolated",
			doc: docHead + `  - name: wf
    agent_class: Workflow
    nodes:
      - { id: na, type: agent, agent: agent_a }
      - { id: nb, type: agent, agent: agent_b }
      - { id: gather, type: join }
      - { id: end, type: function, function: summarize_join }
    edges:
      - { from: START, to: na }
      - { from: START, to: nb }
      - { from: na, to: gather }
      - { from: nb, to: gather }
      - { from: gather, to: end }
` + agentA + agentB,
			bSeesA: false,
		},
		{
			name: "ParallelAgent is branch-isolated",
			doc: docHead + `  - { name: panel, agent_class: ParallelAgent, sub_agents: [agent_a, agent_b] }
` + strings.ReplaceAll(agentA+agentB, "mode: chat, ", ""),
			bSeesA: false,
		},
		{
			name: "SequentialAgent shares the conversation",
			doc: docHead + `  - { name: seq, agent_class: SequentialAgent, sub_agents: [agent_a, agent_b] }
` + strings.ReplaceAll(agentA+agentB, "mode: chat, ", ""),
			bSeesA: true,
		},
		{name: "dynamic children share the conversation by default", doc: dynamicDoc(""), setup: twoChildren, bSeesA: true},
		{name: "dynamic children with isolation shared", doc: dynamicDoc(", isolation: shared"), setup: twoChildren, bSeesA: true},
		{name: "dynamic children with isolation branch", doc: dynamicDoc(", isolation: branch"), setup: twoChildren, bSeesA: false},
		{name: "dynamic children with isolation scope", doc: dynamicDoc(", isolation: scope"), setup: twoChildren, bSeesA: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := newProbe("probe_a", nil)
			b := newProbe("probe_b", a)
			runOnce(t, buildWith(t, tt.doc, tt.setup, a, b), "start")
			require.NotEmpty(t, b.calls(), "the second agent must run")
			require.Equal(t, tt.bSeesA, b.sawAnyOf(a.marker()), "second agent saw: %q", b.calls())
		})
	}
}

func TestParallelWorkerItemsAreBranchIsolated(t *testing.T) {
	a := newProbe("probe_a", nil)
	sys := buildWith(t, docHead+`  - name: wf
    agent_class: Workflow
    nodes:
      - { id: items, type: function, function: two_items }
      - { id: each, type: parallel_worker, agent: agent_a, max_concurrency: 1 }
      - { id: end, type: function, function: summarize_join }
    edges:
      - { from: START, to: items }
      - { from: items, to: each }
      - { from: each, to: end }
`+agentA, func(reg *Registry) {
		require.NoError(t, reg.RegisterFunction("two_items", func(agent.Context, any) (Result, error) {
			return Output([]any{"one", "two"}), nil
		}))
	}, a)
	runOnce(t, sys, "start")
	calls := a.calls()
	require.Len(t, calls, 2)
	require.NotContains(t, calls[1], a.marker(), "an item must not see the previous item's reply")
}

// scriptedCaller is a parent model that calls the named tool once per request
// and then answers with what the tool returned.
type scriptedCaller struct {
	tool     string
	requests []string
}

func (m *scriptedCaller) Name() string { return "scripted_caller" }

func (m *scriptedCaller) GenerateContent(_ context.Context, req *adkmodel.LLMRequest, _ bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		var results []string
		for _, c := range req.Contents {
			for _, part := range c.Parts {
				if part != nil && part.FunctionResponse != nil {
					results = append(results, fmt.Sprint(part.FunctionResponse.Response))
				}
			}
		}
		if len(results) > 0 {
			yield(&adkmodel.LLMResponse{Content: genai.NewContentFromText("tool results: "+strings.Join(results, "; "), genai.RoleModel), TurnComplete: true}, nil)
			return
		}
		parts := make([]*genai.Part, len(m.requests))
		for i, request := range m.requests {
			parts[i] = &genai.Part{FunctionCall: &genai.FunctionCall{
				ID: fmt.Sprintf("call-%d", i), Name: m.tool, Args: map[string]any{"request": request},
			}}
		}
		yield(&adkmodel.LLMResponse{Content: &genai.Content{Role: genai.RoleModel, Parts: parts}, TurnComplete: true}, nil)
	}
}

const agentToolDoc = docHead + `  - name: parent
    agent_class: LlmAgent
    model: scripted_caller
    instruction: Delegate.
    agent_tools: [child]
  - { name: child, agent_class: LlmAgent, model: probe_child, instruction: Do the task. }
`

func TestAgentToolRunsInASessionOfItsOwn(t *testing.T) {
	child := newProbe("probe_child", nil)
	caller := &scriptedCaller{tool: "child", requests: []string{"task-1"}}
	sys := buildWith(t, agentToolDoc, func(reg *Registry) { require.NoError(t, reg.RegisterModel("scripted_caller", caller)) }, child)
	runOnce(t, sys, "the parent's private conversation")

	calls := child.calls()
	require.Len(t, calls, 1)
	require.Contains(t, calls[0], "task-1")
	require.NotContains(t, calls[0], "private conversation", "the tool must not be shown the caller's history")
}

func TestParallelAgentToolCallsDoNotShareASession(t *testing.T) {
	child := newProbe("probe_child", nil)
	caller := &scriptedCaller{tool: "child", requests: []string{"task-1", "task-2", "task-3"}}
	sys := buildWith(t, agentToolDoc, func(reg *Registry) { require.NoError(t, reg.RegisterModel("scripted_caller", caller)) }, child)
	runOnce(t, sys, "go")

	calls := child.calls()
	require.Len(t, calls, 3, "one session per call")
	for _, call := range calls {
		shown := 0
		for _, task := range caller.requests {
			if strings.Contains(call, task) {
				shown++
			}
		}
		require.Equal(t, 1, shown, "a call saw another call's request: %q", call)
		require.NotContains(t, call, child.marker(), "a call saw another call's reply")
	}
}

func TestAgentToolSessionStateIsACopy(t *testing.T) {
	child := newProbe("probe_child", nil)
	caller := &scriptedCaller{tool: "child", requests: []string{"task-1"}}
	// The child writes its reply under its own output_key; the parent must not see it.
	doc := strings.Replace(agentToolDoc, "instruction: Do the task. }", "instruction: Do the task., output_key: child_result }", 1)
	sys := buildWith(t, doc, func(reg *Registry) { require.NoError(t, reg.RegisterModel("scripted_caller", caller)) }, child)

	svc, id := newSession(t)
	run(t, sys.Root(), userMessage("go"), svc, id)
	stored, err := svc.Get(context.Background(), &session.GetRequest{AppName: "test", UserID: "user", SessionID: id})
	require.NoError(t, err)
	_, leaked := stored.Session.State().Get("child_result")
	require.Error(t, leaked, "the tool's state writes stay in its own session")
}

// TestDynamicIsolationScopeHidesAncestorEvents separates scope from branch: a
// child on its own branch still sees what ran before the dynamic node, while a
// scoped child sees only its own events.
func TestDynamicIsolationScopeHidesAncestorEvents(t *testing.T) {
	doc := func(isolation string) string {
		return docHead + `  - name: wf
    agent_class: Workflow
    nodes:
      - { id: upstream, type: agent, agent: agent_up }
      - { id: orchestrate, type: dynamic, function: one_child, children: [child_b], isolation: ` + isolation + ` }
      - { id: child_b, type: agent, agent: agent_b }
      - { id: end, type: function, function: summarize_join }
    edges:
      - { from: START, to: upstream }
      - { from: upstream, to: orchestrate }
      - { from: orchestrate, to: end }
  - { name: agent_up, agent_class: LlmAgent, model: probe_up, mode: chat, instruction: Up }
` + agentB
	}
	for isolation, wantUpstream := range map[string]bool{"shared": true, "branch": true, "scope": false} {
		t.Run(isolation, func(t *testing.T) {
			up := newProbe("probe_up", nil)
			b := newProbe("probe_b", up)
			sys := buildWith(t, doc(isolation), func(reg *Registry) {
				require.NoError(t, reg.RegisterDynamic("one_child", func(_ agent.Context, in any, env *DynamicEnv) (any, error) {
					// The child's own input is fixed, so what it sees of the upstream
					// reply can only come from session history.
					return env.RunChild("child_b", "the task")
				}))
			}, up, b)
			runOnce(t, sys, "start")
			require.NotEmpty(t, b.calls())
			require.Equal(t, wantUpstream, b.sawAnyOf(up.marker()), "child saw: %q", b.calls())
		})
	}
}

// TestIncludeContentsNoneIsNotIsolation pins a trap: include_contents none
// keeps only the current turn, and the earlier agents' replies are part of it.
// It narrows what a sub-agent sees of the user's history; it does not hide
// what the agents before it said.
func TestIncludeContentsNoneIsNotIsolation(t *testing.T) {
	a := newProbe("probe_a", nil)
	b := newProbe("probe_b", nil)
	doc := docHead + "  - { name: seq, agent_class: SequentialAgent, sub_agents: [agent_a, agent_b] }\n" +
		strings.ReplaceAll(agentA, "mode: chat, ", "") +
		strings.Replace(strings.ReplaceAll(agentB, "mode: chat, ", ""), "instruction: B }", "instruction: B, include_contents: none }", 1)
	runOnce(t, buildWith(t, doc, nil, a, b), "start")
	require.True(t, b.sawAnyOf(a.marker()), "the earlier agent's reply still reaches the second agent: %q", b.calls())
}

// TestSingleTurnAgentNodesSeeOnlyTheirInput is the in-graph way to run agents
// one after another without sharing the conversation: a single_turn agent node
// is shown its predecessor's output and nothing else.
func TestSingleTurnAgentNodesSeeOnlyTheirInput(t *testing.T) {
	a := newProbe("probe_a", nil)
	b := newProbe("probe_b", nil)
	doc := docHead + `  - name: wf
    agent_class: Workflow
    nodes:
      - { id: first, type: agent, agent: agent_a }
      - { id: second, type: agent, agent: agent_b }
      - { id: end, type: function, function: summarize_join }
    edges: [{ from: START, to: first }, { from: first, to: second }, { from: second, to: end }]
` + strings.ReplaceAll(agentA, "mode: chat", "mode: single_turn") + strings.ReplaceAll(agentB, "mode: chat", "mode: single_turn")
	runOnce(t, buildWith(t, doc, nil, a, b), "the user's private message")
	calls := b.calls()
	require.Len(t, calls, 1)
	require.Contains(t, calls[0], a.marker(), "it receives its predecessor's output as input")
	require.NotContains(t, calls[0], "private message", "and not the user's conversation")
}
