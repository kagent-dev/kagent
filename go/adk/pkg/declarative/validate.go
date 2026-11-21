package declarative

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/google/jsonschema-go/jsonschema"

	adkoutputschema "github.com/kagent-dev/kagent/go/adk/pkg/outputschema"
)

// maxNesting bounds nested workflow nodes so that a document cannot drive the
// recursive builder arbitrarily deep.
const maxNesting = 8

// Issue is one validation failure located by a field path such as
// agents[0].nodes[2].function.
type Issue struct {
	Path    string
	Message string
}

func (i Issue) String() string { return i.Path + ": " + i.Message }

// ValidationError aggregates every issue found in a document so an author
// fixes them in one pass.
type ValidationError struct {
	Issues []Issue
}

func (e *ValidationError) Error() string {
	lines := make([]string, len(e.Issues))
	for i, issue := range e.Issues {
		lines[i] = issue.String()
	}
	return fmt.Sprintf("invalid %s document:\n  %s", KindAgentSystem, strings.Join(lines, "\n  "))
}

// IsValidation reports whether err is, or wraps, a *ValidationError.
func IsValidation(err error) bool {
	var ve *ValidationError
	return errors.As(err, &ve)
}

// identifier is the shape of agent names and node ids. It matches the names
// ADK accepts for agents and keeps ids usable in event paths.
var identifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// toolName allows the characters that function tool names use.
var toolName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.:-]*$`)

// Validate checks everything that can be decided from the document alone:
// field shapes, per-class and per-type field rules, and name references
// between agents and nodes. Whether the Go values a document names exist is
// checked by Registry.Validate; graph semantics (reachability, cycles, fan-in)
// are checked by Build, where the ADK workflow engine owns them.
func Validate(spec *Spec) error {
	return validate(spec, nil)
}

func validate(spec *Spec, refs *Registry) error {
	v := &validator{refs: refs}
	v.spec(spec)
	if len(v.issues) == 0 {
		return nil
	}
	return &ValidationError{Issues: v.issues}
}

type validator struct {
	// classes indexes the document's agent classes by name.
	classes map[string]AgentClass
	// agents indexes the document's agents by name.
	agents map[string]*AgentSpec
	// refs, when set, resolves the names a document uses to registrations.
	refs   *Registry
	issues []Issue
}

func (v *validator) addf(path, format string, args ...any) {
	v.issues = append(v.issues, Issue{Path: path, Message: fmt.Sprintf(format, args...)})
}

// checkFields reports required fields that are unset and set fields that the
// owner accepts neither as required nor as optional.
func (v *validator) checkFields(path, owner string, present map[string]bool, required, optional []string) {
	for _, field := range required {
		if !present[field] {
			v.addf(path+"."+field, "required for %s", owner)
		}
	}
	for _, field := range slices.Sorted(maps.Keys(present)) {
		if present[field] && !slices.Contains(required, field) && !slices.Contains(optional, field) {
			v.addf(path+"."+field, "not allowed on %s", owner)
		}
	}
}

func (v *validator) spec(spec *Spec) {
	if spec == nil {
		v.addf("$", "document is required")
		return
	}
	if spec.APIVersion != APIVersion {
		v.addf("apiVersion", "must be %q, got %q", APIVersion, spec.APIVersion)
	}
	if spec.Kind != KindAgentSystem {
		v.addf("kind", "must be %q, got %q", KindAgentSystem, spec.Kind)
	}
	if len(spec.Agents) == 0 {
		v.addf("agents", "at least one agent is required")
		return
	}

	v.classes = make(map[string]AgentClass, len(spec.Agents))
	v.agents = make(map[string]*AgentSpec, len(spec.Agents))
	for i, a := range spec.Agents {
		path := fmt.Sprintf("agents[%d]", i)
		switch {
		case !identifier.MatchString(a.Name):
			v.addf(path+".name", "%q must match %s", a.Name, identifier)
		case a.Name == "user":
			v.addf(path+".name", `"user" is reserved for end-user input`)
		case v.agents[a.Name] != nil:
			v.addf(path+".name", "duplicate agent name %q", a.Name)
		default:
			v.classes[a.Name] = a.AgentClass
			v.agents[a.Name] = &spec.Agents[i]
		}
	}
	for i := range spec.Agents {
		v.agent(fmt.Sprintf("agents[%d]", i), &spec.Agents[i])
	}
	v.agentTree(spec)
	v.agentReferences(spec)
}

// agentRule lists the AgentSpec fields a class requires and accepts beyond
// name and agent_class.
type agentRule struct {
	required, optional []string
}

// llmOptional are the LLMSpec fields that an LLM may set.
var llmOptional = []string{"model", "mode", "global_instruction", "tools", "output_key", "output_schema", "include_contents"}

var agentRules = map[AgentClass]agentRule{
	ClassWorkflow: {required: []string{"nodes"}, optional: []string{"description", "sub_agents", "entry", "edges"}},
	ClassLlmAgent: {
		required: []string{"instruction"},
		optional: append([]string{"description", "sub_agents", "disallow_transfer_to_parent", "disallow_transfer_to_peers"}, llmOptional...),
	},
}

func presentAgentFields(a *AgentSpec) map[string]bool {
	present := llmPresent(&a.LLMSpec)
	maps.Copy(present, map[string]bool{
		"description": a.Description != "", "sub_agents": len(a.SubAgents) > 0,
		"entry": a.Entry != "", "nodes": len(a.Nodes) > 0, "edges": len(a.Edges) > 0,
		"disallow_transfer_to_parent": a.DisallowTransferToParent,
		"disallow_transfer_to_peers":  a.DisallowTransferToPeers,
	})
	return present
}

func llmPresent(l *LLMSpec) map[string]bool {
	return map[string]bool{
		"model": l.Model != "", "mode": l.Mode != "",
		"instruction": l.Instruction != "", "global_instruction": l.GlobalInstruction != "",
		"tools": len(l.Tools) > 0, "output_key": l.OutputKey != "",
		"output_schema": len(l.OutputSchema) > 0, "include_contents": l.IncludeContents != "",
	}
}

func (v *validator) agent(path string, a *AgentSpec) {
	for i, name := range a.SubAgents {
		p := fmt.Sprintf("%s.sub_agents[%d]", path, i)
		if _, ok := v.classes[name]; !ok {
			v.addf(p, "unknown agent %q", name)
		} else if name == a.Name {
			v.addf(p, "an agent cannot be its own sub-agent")
		}
	}

	rule, ok := agentRules[a.AgentClass]
	if !ok {
		v.addf(path+".agent_class", "must be one of %s, %s, got %q", ClassLlmAgent, ClassWorkflow, a.AgentClass)
		return
	}
	v.checkFields(path, "agent_class "+string(a.AgentClass), presentAgentFields(a), rule.required, rule.optional)

	switch a.AgentClass {
	case ClassWorkflow:
		v.graph(path, a.Nodes, a.Edges, a.Entry, 0, true)
	case ClassLlmAgent:
		v.llm(path, &a.LLMSpec)
	}
}

// llm validates the fields shared by LlmAgent agents and llm nodes.
func (v *validator) llm(path string, l *LLMSpec) {
	switch l.Mode {
	case "", ModeChat, ModeTask, ModeSingleTurn:
	default:
		v.addf(path+".mode", "must be one of chat, task, single_turn, got %q", l.Mode)
	}
	switch l.IncludeContents {
	case "", IncludeContentsDefault, IncludeContentsNone:
	default:
		v.addf(path+".include_contents", "must be default or none, got %q", l.IncludeContents)
	}
	if len(l.OutputSchema) > 0 {
		if _, err := adkoutputschema.ToGenAISchema(l.OutputSchema); err != nil {
			v.addf(path+".output_schema", "%v", err)
		}
	}
	switch {
	case l.Model != "" && l.Model != StubModel:
		v.addf(path+".model", "%q is not accepted; %s", l.Model, modelHint)
	case l.Model == "" && v.refs != nil && v.refs.model == nil:
		v.addf(path+".model", "the registry has no model; %s", modelHint)
	}
	seen := map[string]bool{}
	for i, t := range l.Tools {
		v.toolRef(fmt.Sprintf("%s.tools[%d]", path, i), t)
		if seen[t] {
			v.addf(fmt.Sprintf("%s.tools[%d]", path, i), "duplicate tool %q", t)
		}
		seen[t] = true
	}
}

// nodeRule lists the NodeSpec fields a node type requires and accepts beyond
// the common ones (id, type, description). Field names are the JSON names.
type nodeRule struct {
	required, optional []string
}

var nodeRules = map[NodeType]nodeRule{
	NodeFunction: {required: []string{"function"}, optional: []string{"input_schema", "output_schema", "retry", "timeout"}},
	NodeJoin:     {optional: []string{"input_schema"}},
	NodeTool:     {required: []string{"tool"}, optional: []string{"retry", "timeout"}},
	NodeAgent:    {required: []string{"agent"}, optional: []string{"input_schema", "retry", "timeout"}},
	NodeLLM: {
		required: []string{"instruction"},
		optional: append([]string{"input_schema", "retry", "timeout"}, llmOptional...),
	},
	NodeWorkflow: {required: []string{"nodes", "edges"}},
}

// structuredTerminals are node types whose output is structured data that no
// A2A client displays; a branch must not end on one.
var structuredTerminals = []NodeType{NodeTool, NodeJoin}

// presentFields names the type-specific fields a node sets.
func presentFields(n *NodeSpec) map[string]bool {
	present := llmPresent(&n.LLMSpec)
	maps.Copy(present, map[string]bool{
		"function": n.Function != "", "tool": n.Tool != "", "agent": n.Agent != "",
		"input_schema": len(n.InputSchema) > 0,
		"nodes":        len(n.Nodes) > 0, "edges": len(n.Edges) > 0,
		"retry": n.Retry != nil, "timeout": n.Timeout != "",
	})
	return present
}

// graph validates one node/edge scope: the root of a Workflow agent or the
// body of a nested workflow node. terminal reports whether the scope's own
// terminal nodes end the run, which holds for the root scope and for nested
// scopes whose workflow node is itself terminal.
func (v *validator) graph(path string, nodes []NodeSpec, edges []EdgeSpec, entry string, depth int, terminal bool) {
	if depth > maxNesting {
		v.addf(path, "workflow nodes are nested deeper than %d levels", maxNesting)
		return
	}

	ids := make(map[string]bool, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		p := fmt.Sprintf("%s.nodes[%d]", path, i)
		switch {
		case !identifier.MatchString(n.ID):
			v.addf(p+".id", "%q must match %s", n.ID, identifier)
		case n.ID == Start:
			v.addf(p+".id", "%q is reserved", Start)
		case ids[n.ID]:
			v.addf(p+".id", "duplicate node id %q", n.ID)
		default:
			ids[n.ID] = true
		}
	}

	outgoing := make(map[string]bool, len(edges))
	for _, e := range edges {
		outgoing[e.From] = true
	}
	agentTargets := make(map[string]string)
	for i := range nodes {
		n := &nodes[i]
		p := fmt.Sprintf("%s.nodes[%d]", path, i)
		v.node(p, n, depth, terminal && !outgoing[n.ID])
		if n.Type != NodeAgent || n.Agent == "" {
			continue
		}
		if prev, dup := agentTargets[n.Agent]; dup {
			v.addf(p+".agent", "agent %q is already used by node %q in this graph; ADK names an agent node after its agent", n.Agent, prev)
		}
		agentTargets[n.Agent] = n.ID
		if ids[n.Agent] {
			v.addf(p+".agent", "agent %q collides with a node id in the same graph", n.Agent)
		}
	}

	v.edges(path, nodes, edges, entry, ids)
}

func (v *validator) edges(path string, nodes []NodeSpec, edges []EdgeSpec, entry string, ids map[string]bool) {
	if entry != "" && !ids[entry] {
		v.addf(path+".entry", "unknown node %q", entry)
	}
	hasStart := entry != ""
	connected := map[string]bool{entry: true}
	seen := map[EdgeSpec]bool{}
	defaults := map[string]bool{}
	for i, e := range edges {
		p := fmt.Sprintf("%s.edges[%d]", path, i)
		if e.From != Start && !ids[e.From] {
			v.addf(p+".from", "unknown node %q", e.From)
		}
		switch {
		case e.To == Start:
			v.addf(p+".to", "%q cannot be an edge target", Start)
		case e.To == "END" && !ids[e.To]:
			v.addf(p+".to", "there is no END node; a node without outgoing edges ends its branch")
		case !ids[e.To]:
			v.addf(p+".to", "unknown node %q", e.To)
		}
		connected[e.From], connected[e.To] = true, true
		if e.From == Start {
			hasStart = true
			if e.Route != "" {
				v.addf(p+".route", "%q does not emit routes", Start)
			}
		}
		if seen[e] {
			v.addf(p, "duplicate edge")
		}
		seen[e] = true
		if e.Route == RouteDefault {
			if defaults[e.From] {
				v.addf(p+".route", "node %q has more than one %s edge", e.From, RouteDefault)
			}
			defaults[e.From] = true
		}
	}
	v.fanOutOutputKeys(path, nodes, edges)
	for i, n := range nodes {
		if ids[n.ID] && !connected[n.ID] {
			v.addf(fmt.Sprintf("%s.nodes[%d]", path, i), "node %q is not connected by any edge", n.ID)
		}
	}
	if !hasStart && len(nodes) > 0 {
		v.addf(path+".edges", "no edge from %s and no entry: the graph has no starting node", Start)
	}
}

func (v *validator) node(path string, n *NodeSpec, depth int, terminal bool) {
	rule, ok := nodeRules[n.Type]
	if !ok {
		types := make([]string, 0, len(nodeRules))
		for t := range nodeRules {
			types = append(types, string(t))
		}
		slices.Sort(types)
		v.addf(path+".type", "unknown node type %q (known: %s)", n.Type, strings.Join(types, ", "))
		return
	}
	v.checkFields(path, "node type "+string(n.Type), presentFields(n), rule.required, rule.optional)
	v.nodeSchemas(path, n)

	if terminal && slices.Contains(structuredTerminals, n.Type) {
		v.addf(path, "terminal %s node emits structured output that A2A clients do not display; end the branch with a function node", n.Type)
	}

	switch n.Type {
	case NodeFunction:
		if n.Function != "" && v.refs != nil && !v.refs.hasFunction(n.Function) {
			v.addf(path+".function", "function %q is not registered", n.Function)
		}
	case NodeTool:
		if n.Tool != "" {
			v.toolRef(path+".tool", n.Tool)
		}
	case NodeLLM:
		v.llm(path, &n.LLMSpec)
		if n.Mode != "" && n.Mode != ModeSingleTurn {
			v.addf(path+".mode", "an inline llm node runs mid-graph and must be single_turn, got %q", n.Mode)
		}
	case NodeAgent:
		v.agentTarget(path+".agent", n.Agent)
	case NodeWorkflow:
		v.graph(path, n.Nodes, n.Edges, "", depth+1, terminal)
	}

	if n.Timeout != "" {
		if d, err := time.ParseDuration(n.Timeout); err != nil || d <= 0 {
			v.addf(path+".timeout", "must be a positive duration such as 30s, got %q", n.Timeout)
		}
	}
	if n.Retry != nil {
		v.retry(path+".retry", n.Retry)
	}
}

// nodeSchemas checks that a node's contracts are JSON Schemas the engine can
// use. An inline llm node's output_schema is the model's reply schema and is
// checked with the other model options.
func (v *validator) nodeSchemas(path string, n *NodeSpec) {
	if len(n.InputSchema) > 0 {
		if _, err := parseSchema(n.InputSchema); err != nil {
			v.addf(path+".input_schema", "%v", err)
		}
	}
	if n.Type != NodeLLM && len(n.OutputSchema) > 0 {
		if _, err := parseSchema(n.OutputSchema); err != nil {
			v.addf(path+".output_schema", "%v", err)
		}
	}
}

// parseSchema decodes and resolves a JSON Schema, so a malformed or
// unresolvable one fails validation instead of the first run.
func parseSchema(raw json.RawMessage) (*jsonschema.Schema, error) {
	var schema jsonschema.Schema
	if err := json.Unmarshal(raw, &schema); err != nil {
		return nil, fmt.Errorf("not a JSON Schema: %w", err)
	}
	if _, err := schema.Resolve(nil); err != nil {
		return nil, fmt.Errorf("cannot resolve the schema: %w", err)
	}
	return &schema, nil
}

func (v *validator) toolRef(path, name string) {
	switch {
	case !toolName.MatchString(name):
		v.addf(path, "%q must match %s", name, toolName)
	case v.refs != nil && !v.refs.hasTool(name):
		v.addf(path, "tool %q is not registered", name)
	}
}

func (v *validator) agentTarget(path, name string) {
	if name == "" {
		return
	}
	class, known := v.classes[name]
	switch {
	case !known:
		v.addf(path, "unknown agent %q", name)
	case class == ClassWorkflow:
		v.addf(path, "agent %q is a Workflow; use a workflow node to nest graphs", name)
	}
}

func (v *validator) retry(path string, r *RetrySpec) {
	if r.MaxAttempts < 0 {
		v.addf(path+".max_attempts", "must not be negative")
	}
	for _, f := range []struct{ field, value string }{{"initial_delay", r.InitialDelay}, {"max_delay", r.MaxDelay}} {
		if f.value == "" {
			continue
		}
		if d, err := time.ParseDuration(f.value); err != nil || d < 0 {
			v.addf(path+"."+f.field, "must be a non-negative duration such as 1s, got %q", f.value)
		}
	}
	if r.BackoffFactor != nil && *r.BackoffFactor <= 0 {
		v.addf(path+".backoff_factor", "must be positive")
	}
	if r.Jitter != nil && (*r.Jitter < 0 || *r.Jitter > 1) {
		v.addf(path+".jitter", "must be within [0, 1]")
	}
}

// agentTree checks that sub_agents form a forest: ADK names must be unique
// within an agent tree and a sub-agent has one parent.
func (v *validator) agentTree(spec *Spec) {
	parent := map[string]string{}
	for i, a := range spec.Agents {
		for j, sub := range a.SubAgents {
			if prev, taken := parent[sub]; taken {
				v.addf(fmt.Sprintf("agents[%d].sub_agents[%d]", i, j), "agent %q is already a sub-agent of %q; an agent has one parent", sub, prev)
				continue
			}
			parent[sub] = a.Name
		}
	}
	for i, a := range spec.Agents {
		seen := map[string]bool{a.Name: true}
		for cur, ok := parent[a.Name]; ok; cur, ok = parent[cur] {
			if seen[cur] {
				v.addf(fmt.Sprintf("agents[%d].sub_agents", i), "agent %q is part of a sub-agent cycle", a.Name)
				break
			}
			seen[cur] = true
		}
	}
}

// fanOutOutputKeys reports unconditional successors of one node, which the
// engine starts concurrently, that write the same session state key.
// Branches isolate history, not state: a shared key is written by whichever
// branch finishes last.
func (v *validator) fanOutOutputKeys(path string, nodes []NodeSpec, edges []EdgeSpec) {
	byID := make(map[string]*NodeSpec, len(nodes))
	for i := range nodes {
		byID[nodes[i].ID] = &nodes[i]
	}
	successors := map[string][]string{}
	for _, e := range edges {
		if e.Route == "" {
			successors[e.From] = append(successors[e.From], e.To)
		}
	}
	for _, from := range slices.Sorted(maps.Keys(successors)) {
		owner := map[string]string{}
		for _, id := range successors[from] {
			n := byID[id]
			if n == nil {
				continue
			}
			key := n.OutputKey
			if n.Type == NodeAgent && v.agents[n.Agent] != nil {
				key = v.agents[n.Agent].OutputKey
			}
			if key == "" {
				continue
			}
			if prev, taken := owner[key]; taken {
				v.addf(path+".edges", "nodes %q and %q run concurrently after %q and both write the state key %q; give each its own output_key", prev, id, from, key)
				continue
			}
			owner[key] = id
		}
	}
}

// agentReferences rejects cycles that pass through an agent node, such as a
// Workflow running an LlmAgent whose sub-agent is that Workflow. They would
// recurse without end when built. Pure sub_agents cycles are reported by
// agentTree.
func (v *validator) agentReferences(spec *Spec) {
	type edge struct {
		to     string
		byNode bool
	}
	uses := map[string][]edge{}
	for _, a := range spec.Agents {
		for _, s := range a.SubAgents {
			uses[a.Name] = append(uses[a.Name], edge{to: s})
		}
		for _, target := range nodeAgents(a.Nodes) {
			uses[a.Name] = append(uses[a.Name], edge{to: target, byNode: true})
		}
	}

	const (
		unvisited = iota
		onStack
		done
	)
	state := map[string]int{}
	reported := map[string]bool{}
	var stack []string
	var byNode []bool
	var visit func(name string)
	visit = func(name string) {
		state[name] = onStack
		stack = append(stack, name)
		for _, e := range uses[name] {
			switch state[e.to] {
			case unvisited:
				byNode = append(byNode, e.byNode)
				visit(e.to)
				byNode = byNode[:len(byNode)-1]
			case onStack:
				start := slices.Index(stack, e.to)
				throughNode := e.byNode || slices.Contains(byNode[start:], true)
				if throughNode && !reported[e.to] {
					members := append(slices.Clone(stack[start:]), e.to)
					for _, m := range members {
						reported[m] = true
					}
					v.addf("agents", "agents reference themselves: %s", strings.Join(members, " → "))
				}
			}
		}
		stack = stack[:len(stack)-1]
		state[name] = done
	}
	for _, a := range spec.Agents {
		if state[a.Name] == unvisited {
			visit(a.Name)
		}
	}
}

// nodeAgents lists the agents that nodes run, through nested workflows too.
func nodeAgents(nodes []NodeSpec) []string {
	var out []string
	for i := range nodes {
		if nodes[i].Type == NodeAgent && nodes[i].Agent != "" {
			out = append(out, nodes[i].Agent)
		}
		out = append(out, nodeAgents(nodes[i].Nodes)...)
	}
	return out
}
