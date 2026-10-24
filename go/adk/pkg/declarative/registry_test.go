package declarative

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/adk/v2/agent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/adk/v2/workflow"
)

func noopFunc(agent.Context, any) (Result, error) { return Result{}, nil }

func TestRegistryRejectsBadRegistrations(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.RegisterFunction("f", noopFunc))
	require.ErrorContains(t, reg.RegisterFunction("f", noopFunc), "already registered")
	require.ErrorContains(t, reg.RegisterFunction("bad name", noopFunc), "must match")
	require.ErrorContains(t, reg.RegisterFunction("nilfn", nil), "nil func")

	require.ErrorContains(t, reg.RegisterDynamic("d", nil), "nil func")
	require.ErrorContains(t, reg.RegisterModel("stub", NewStubModel("stub")), "already registered")
	require.ErrorContains(t, reg.RegisterModel("m", nil), "nil model")
	require.ErrorContains(t, reg.RegisterModel("not valid!", NewStubModel("x")), "must match")
	require.NoError(t, reg.RegisterModel("openai/gpt-4o:latest", NewStubModel("gpt")))

	require.ErrorContains(t, reg.RegisterTool(nil), "nil tool")
	require.ErrorContains(t, reg.RegisterToolset(nil), "nil toolset")
	require.ErrorContains(t, reg.RegisterTool(reg.tools["load_memory"]), "already registered")
}

func TestRegistryToolAndToolsetNamesAreDisjoint(t *testing.T) {
	reg := NewRegistry()
	ts := namedToolset("shared")
	require.NoError(t, reg.RegisterToolset(ts))
	clash, err := functiontool.New(functiontool.Config{Name: "shared", Description: "d"},
		func(agent.Context, struct{}) (string, error) { return "", nil })
	require.NoError(t, err)
	require.ErrorContains(t, reg.RegisterTool(clash), "used by another registration")
}

type namedToolset string

func (n namedToolset) Name() string                                     { return string(n) }
func (n namedToolset) Tools(agent.ReadonlyContext) ([]tool.Tool, error) { return nil, nil }

func TestRegistryBuiltinTools(t *testing.T) {
	reg := NewRegistry()
	tools, toolsets, err := reg.resolveTools([]string{"preload_memory", "load_memory"})
	require.NoError(t, err)
	require.Len(t, tools, 2)
	require.Empty(t, toolsets)
	_, _, err = reg.resolveTools([]string{"ghost"})
	require.ErrorContains(t, err, `tool "ghost" is not registered`)
}

func TestRegistryToolsetsResolve(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.RegisterToolset(namedToolset("mcp_server")))
	tools, toolsets, err := reg.resolveTools([]string{"mcp_server", "load_memory"})
	require.NoError(t, err)
	require.Len(t, tools, 1)
	require.Len(t, toolsets, 1)
}

func TestRegisterNodeTypeRules(t *testing.T) {
	reg := NewRegistry()
	build := func(*BuildContext, *NodeSpec) (workflow.Node, error) { return nil, nil }
	require.ErrorContains(t, reg.RegisterNodeType(NodeJoin, NodeRule{}, build), "built-in types cannot be replaced")
	require.ErrorContains(t, reg.RegisterNodeType("bad-name", NodeRule{}, build), "must match")
	require.ErrorContains(t, reg.RegisterNodeType("custom", NodeRule{}, nil), "nil builder")
	require.NoError(t, reg.RegisterNodeType("custom", NodeRule{}, build))
	require.ErrorContains(t, reg.RegisterNodeType("custom", NodeRule{}, build), "already registered")
}

func TestDynamicEnvBudget(t *testing.T) {
	child := workflow.NewFunctionNode("c", func(agent.Context, any) (string, error) { return "ok", nil }, workflow.NodeConfig{})
	bounded := &DynamicEnv{MaxIters: 1, runs: 1}
	_, err := bounded.Run(child, nil)
	require.ErrorIs(t, err, ErrIterationBudget)
	require.Equal(t, 1, bounded.Runs(), "a refused run is not counted")
}

func TestDynamicEnvRunChildRejectsUnknownIDs(t *testing.T) {
	env := &DynamicEnv{}
	_, err := env.RunChild("ghost", nil)
	require.ErrorContains(t, err, `no child "ghost"`)
	require.Zero(t, env.Runs())
}

func TestRegistryValidateReportsUnresolvedReferencesTogether(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.RegisterFunction("known", noopFunc))
	_, err := reg.Parse([]byte(`
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - { name: a, agent_class: LlmAgent, model: gpt-nope, instruction: hi, tools: [load_memory, ghost_tool] }
  - name: wf
    agent_class: Workflow
    nodes:
      - { id: f, type: function, function: nope }
      - { id: k, type: function, function: known }
      - { id: t, type: tool, tool: ghost_tool }
      - { id: d, type: dynamic, function: nope_dyn }
      - { id: p, type: parallel_worker, function: nope_too }
      - { id: l, type: llm, model: gpt-nope, instruction: hi, tools: [ghost_tool] }
      - { id: end, type: function, function: known }
    edges:
      - { from: START, to: f }
      - { from: f, to: k }
      - { from: k, to: t }
      - { from: t, to: d }
      - { from: d, to: p }
      - { from: p, to: l }
      - { from: l, to: end }
`))
	require.True(t, IsValidation(err))
	for _, want := range []string{
		`agents[0].model: model "gpt-nope" is not registered`,
		`agents[0].tools[1]: tool "ghost_tool" is not registered`,
		`agents[1].nodes[0].function: function "nope" is not registered`,
		`agents[1].nodes[2].tool: tool "ghost_tool" is not registered`,
		`agents[1].nodes[3].function: dynamic function "nope_dyn" is not registered`,
		`agents[1].nodes[4].function: function "nope_too" is not registered`,
		`agents[1].nodes[5].model: model "gpt-nope" is not registered`,
		`agents[1].nodes[5].tools[0]: tool "ghost_tool" is not registered`,
	} {
		require.Contains(t, err.Error(), want)
	}
	require.NotContains(t, err.Error(), "load_memory", "built-in tools resolve")
	require.NotContains(t, err.Error(), `function "known"`)
}

func TestToolsetsResolveInLLMToolListsButNotAsToolNodes(t *testing.T) {
	reg := NewRegistry()
	require.NoError(t, reg.RegisterToolset(namedToolset("mcp-server")))
	_, err := reg.Parse([]byte(`
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - { name: a, agent_class: LlmAgent, model: stub, instruction: hi, tools: [mcp-server] }
  - name: wf
    agent_class: Workflow
    nodes: [{ id: t, type: tool, tool: mcp-server }, { id: f, type: function, function: f }]
    edges: [{ from: START, to: t }, { from: t, to: f }]
`))
	require.ErrorContains(t, err, `agents[1].nodes[0].tool: tool "mcp-server" is not registered`)
	require.NotContains(t, err.Error(), "agents[0]")
}

func TestModelResolverSatisfiesReferenceCheck(t *testing.T) {
	reg := NewRegistry()
	reg.SetModelResolver(func(context.Context, string) (adkmodel.LLM, error) { return NewStubModel("x"), nil })
	_, err := reg.Parse([]byte(`
apiVersion: adk.declarative/v0.2
kind: AgentSystem
agents:
  - { name: a, agent_class: LlmAgent, model: any-model, instruction: hi }
`))
	require.NoError(t, err)
}

func TestResultHelpers(t *testing.T) {
	require.Equal(t, Result{Output: 1}, Output(1))
	require.Equal(t, Result{Output: 1, Routes: []string{"a", "b"}}, Route(1, "a", "b"))
}
