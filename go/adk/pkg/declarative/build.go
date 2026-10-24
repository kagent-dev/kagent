package declarative

import (
	"context"
	"fmt"
	"slices"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/agent/workflowagent"
	"google.golang.org/adk/v2/agent/workflowagents/loopagent"
	"google.golang.org/adk/v2/agent/workflowagents/parallelagent"
	"google.golang.org/adk/v2/agent/workflowagents/sequentialagent"
	"google.golang.org/adk/v2/tool/agenttool"
	"google.golang.org/adk/v2/workflow"

	adkoutputschema "github.com/kagent-dev/kagent/go/adk/pkg/outputschema"
)

// System is the set of agents built from one document.
type System struct {
	root   agent.Agent
	agents map[string]agent.Agent
	names  []string
}

// Root is the first agent of the document.
func (s *System) Root() agent.Agent { return s.root }

// Agent returns the agent declared under name.
func (s *System) Agent(name string) (agent.Agent, bool) {
	a, ok := s.agents[name]
	return a, ok
}

// Names lists the declared agents in document order.
func (s *System) Names() []string { return slices.Clone(s.names) }

// Loader exposes every declared agent, root first, to ADK launchers and
// servers that select an agent by name.
func (s *System) Loader() (agent.Loader, error) {
	others := make([]agent.Agent, 0, len(s.names)-1)
	for _, name := range s.names[1:] {
		others = append(others, s.agents[name])
	}
	return agent.NewMultiLoader(s.root, others...)
}

// Build validates spec against reg and constructs every declared agent. An
// agent that is reachable from several places (sub_agents, agent nodes) is
// built once and shared. Graph semantics such as reachability, unconditional
// cycles and fan-in are enforced by the ADK workflow engine and reported with
// the agent they belong to. A model resolver's own failures also surface here,
// because only it can tell whether a name resolves.
func Build(ctx context.Context, spec *Spec, reg *Registry) (*System, error) {
	if reg == nil {
		reg = NewRegistry()
	}
	if err := reg.Validate(spec); err != nil {
		return nil, err
	}
	b := &builder{
		ctx:   ctx,
		reg:   reg,
		specs: make(map[string]*AgentSpec, len(spec.Agents)),
		built: make(map[string]agent.Agent, len(spec.Agents)),
	}
	sys := &System{agents: b.built}
	for i := range spec.Agents {
		b.specs[spec.Agents[i].Name] = &spec.Agents[i]
		sys.names = append(sys.names, spec.Agents[i].Name)
	}
	for _, name := range sys.names {
		if _, err := b.agent(name); err != nil {
			return nil, err
		}
	}
	sys.root = b.built[sys.names[0]]
	return sys, nil
}

// builder memoizes agents by name. Validation guarantees the references it
// follows are acyclic: sub_agents form a forest and agent nodes only target
// LlmAgents, which never reference a Workflow.
type builder struct {
	ctx   context.Context
	reg   *Registry
	specs map[string]*AgentSpec
	built map[string]agent.Agent
}

func (b *builder) agent(name string) (agent.Agent, error) {
	if a, ok := b.built[name]; ok {
		return a, nil
	}
	spec := b.specs[name]
	var (
		a   agent.Agent
		err error
	)
	switch spec.AgentClass {
	case ClassWorkflow:
		a, err = b.workflowAgent(spec)
	case ClassLlmAgent:
		a, err = b.llmAgent(spec)
	case ClassSequential, ClassParallel, ClassLoop:
		a, err = b.composite(spec)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to build agent %q: %w", name, err)
	}
	b.built[name] = a
	return a, nil
}

func (b *builder) subAgents(names []string) ([]agent.Agent, error) {
	subs := make([]agent.Agent, 0, len(names))
	for _, n := range names {
		a, err := b.agent(n)
		if err != nil {
			return nil, err
		}
		subs = append(subs, a)
	}
	return subs, nil
}

func (b *builder) llmAgent(spec *AgentSpec) (agent.Agent, error) {
	return b.newLLM(spec.Name, spec.Description, &spec.LLMSpec, spec.Mode, spec.SubAgents, spec.AgentTools, spec.DisallowTransferToParent, spec.DisallowTransferToPeers)
}

// newLLM is shared by LlmAgent agents and inline llm nodes, which pass their
// own effective mode.
func (b *builder) newLLM(name, description string, l *LLMSpec, mode Mode, subAgents, agentTools []string, noParent, noPeers bool) (agent.Agent, error) {
	llm, err := b.reg.resolveModel(b.ctx, l.Model)
	if err != nil {
		return nil, err
	}
	toolList, toolsets, err := b.reg.resolveTools(l.Tools)
	if err != nil {
		return nil, err
	}
	subs, err := b.subAgents(subAgents)
	if err != nil {
		return nil, err
	}
	// An agent tool runs in a session of its own, so it never joins the tree.
	for _, name := range agentTools {
		target, err := b.agent(name)
		if err != nil {
			return nil, err
		}
		toolList = append(toolList, agenttool.New(target, nil))
	}
	cfg := llmagent.Config{
		Name:                     name,
		Description:              description,
		Model:                    llm,
		Instruction:              l.Instruction,
		GlobalInstruction:        l.GlobalInstruction,
		Tools:                    toolList,
		Toolsets:                 toolsets,
		SubAgents:                subs,
		OutputKey:                l.OutputKey,
		Mode:                     llmagent.Mode(mode),
		IncludeContents:          llmagent.IncludeContents(l.IncludeContents),
		DisallowTransferToParent: noParent,
		DisallowTransferToPeers:  noPeers,
	}
	if len(l.OutputSchema) > 0 {
		if cfg.OutputSchema, err = adkoutputschema.ToGenAISchema(l.OutputSchema); err != nil {
			return nil, fmt.Errorf("failed to convert output_schema: %w", err)
		}
	}
	return llmagent.New(cfg)
}

// composite builds the classic ADK workflow agents, which orchestrate their
// sub-agents without a graph.
func (b *builder) composite(spec *AgentSpec) (agent.Agent, error) {
	subs, err := b.subAgents(spec.SubAgents)
	if err != nil {
		return nil, err
	}
	cfg := agent.Config{Name: spec.Name, Description: spec.Description, SubAgents: subs}
	switch spec.AgentClass {
	case ClassSequential:
		return sequentialagent.New(sequentialagent.Config{AgentConfig: cfg})
	case ClassParallel:
		return parallelagent.New(parallelagent.Config{AgentConfig: cfg})
	default:
		return loopagent.New(loopagent.Config{AgentConfig: cfg, MaxIterations: spec.MaxIterations})
	}
}

func (b *builder) workflowAgent(spec *AgentSpec) (agent.Agent, error) {
	nodes, err := b.nodes(spec.Nodes, spec.Edges, true)
	if err != nil {
		return nil, err
	}
	edges := toEdges(nodes, spec.Edges, spec.Entry)
	subs, err := b.subAgents(spec.SubAgents)
	if err != nil {
		return nil, err
	}
	return workflowagent.New(workflowagent.Config{
		Name:        spec.Name,
		Description: spec.Description,
		SubAgents:   subs,
		Edges:       edges,
	})
}

// scope holds the nodes of one graph by document id. Dynamic nodes resolve
// their children from it when they run, after every node has been built.
type scope struct {
	nodes map[string]workflow.Node
}

// nodes builds every node of one graph scope. terminal reports whether the
// scope's terminal nodes end the run; those nodes render their output for
// clients that only display message text.
func (b *builder) nodes(specs []NodeSpec, edges []EdgeSpec, terminal bool) (map[string]workflow.Node, error) {
	bc := &BuildContext{Context: b.ctx, Registry: b.reg, Agent: b.agent}
	sc := &scope{nodes: make(map[string]workflow.Node, len(specs))}
	outgoing := make(map[string]bool, len(edges))
	for _, e := range edges {
		outgoing[e.From] = true
	}
	children := map[string]bool{}
	for i := range specs {
		for _, c := range specs[i].Children {
			children[c] = true
		}
	}
	for i := range specs {
		n := &specs[i]
		node, err := b.node(bc, sc, n, terminal && !outgoing[n.ID] && !children[n.ID])
		if err != nil {
			return nil, fmt.Errorf("failed to build node %q: %w", n.ID, err)
		}
		sc.nodes[n.ID] = node
	}
	return sc.nodes, nil
}

// toEdges resolves document edges against built nodes. entry is sugar for a
// START edge and is skipped when the edge already exists.
func toEdges(nodes map[string]workflow.Node, specs []EdgeSpec, entry string) []workflow.Edge {
	resolve := func(id string) workflow.Node {
		if id == Start {
			return workflow.Start
		}
		return nodes[id]
	}
	edges := make([]workflow.Edge, 0, len(specs)+1)
	hasEntry := false
	for _, e := range specs {
		edge := workflow.Edge{From: resolve(e.From), To: resolve(e.To)}
		switch e.Route {
		case "":
		case RouteDefault:
			edge.Route = workflow.Default
		default:
			edge.Route = workflow.StringRoute(e.Route)
		}
		hasEntry = hasEntry || (e.From == Start && e.To == entry && e.Route == "")
		edges = append(edges, edge)
	}
	if entry != "" && !hasEntry {
		edges = append([]workflow.Edge{{From: workflow.Start, To: nodes[entry]}}, edges...)
	}
	return edges
}
