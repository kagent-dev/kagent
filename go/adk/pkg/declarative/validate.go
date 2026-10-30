package declarative

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
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

// modelName additionally allows the separators provider model ids use.
var modelName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:/-]*$`)

// toolName allows the characters that MCP and function tool names use.
var toolName = regexp.MustCompile(`^[A-Za-z0-9_][A-Za-z0-9_.:-]*$`)

// Validate checks everything that can be decided from the document alone:
// field shapes, per-class and per-type field rules, and name references
// between agents and nodes. Whether the Go values a document names exist is
// checked by Registry.Validate; graph semantics (reachability, cycles, fan-in)
// are checked by Build, where the ADK workflow engine owns them.
func Validate(spec *Spec) error {
	return validate(spec, nil, nil)
}

// ValidateWithRules is Validate for documents that use node types added with
// Registry.RegisterNodeType. extra rules take precedence over the built-ins.
func ValidateWithRules(spec *Spec, extra map[NodeType]NodeRule) error {
	return validate(spec, extra, nil)
}

func validate(spec *Spec, extra map[NodeType]NodeRule, refs *Registry) error {
	rules := maps.Clone(builtinNodeRules)
	maps.Copy(rules, extra)
	v := &validator{rules: rules, refs: refs}
	v.spec(spec)
	if len(v.issues) == 0 {
		return nil
	}
	return &ValidationError{Issues: v.issues}
}

type validator struct {
	// agents indexes the document's agents by name.
	agents map[string]*AgentSpec
	rules  map[NodeType]NodeRule
	// refs, when set, resolves the names a document uses to registrations.
	refs *Registry
	// servers indexes the document's MCP servers by name.
	servers map[string]*MCPServerSpec
	issues  []Issue
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
	v.mcpServers(spec.MCPServers)
	if len(spec.Agents) == 0 {
		v.addf("agents", "at least one agent is required")
		return
	}

	classes := make(map[string]AgentClass, len(spec.Agents))
	v.agents = make(map[string]*AgentSpec, len(spec.Agents))
	for i, a := range spec.Agents {
		path := fmt.Sprintf("agents[%d]", i)
		switch {
		case !identifier.MatchString(a.Name):
			v.addf(path+".name", "%q must match %s", a.Name, identifier)
		case a.Name == "user":
			v.addf(path+".name", `"user" is reserved for end-user input`)
		default:
			if _, dup := classes[a.Name]; dup {
				v.addf(path+".name", "duplicate agent name %q", a.Name)
			}
			classes[a.Name] = a.AgentClass
			v.agents[a.Name] = &spec.Agents[i]
		}
	}
	for i := range spec.Agents {
		v.agent(fmt.Sprintf("agents[%d]", i), &spec.Agents[i], classes)
	}
	v.agentTree(spec)
	v.agentReferences(spec)
}

// agentRule lists the AgentSpec fields a class requires and accepts beyond
// name and agent_class.
type agentRule struct {
	required, optional []string
}

var llmAgentFields = []string{"mode", "global_instruction", "instruction_from", "global_instruction_from", "callbacks", "tools", "output_key", "output_schema", "include_contents"}

var agentRules = map[AgentClass]agentRule{
	ClassWorkflow: {required: []string{"nodes"}, optional: []string{"description", "sub_agents", "entry", "edges", "max_concurrency"}},
	ClassLlmAgent: {
		required: []string{"model", "instruction"},
		optional: append([]string{"description", "sub_agents", "agent_tools", "disallow_transfer_to_parent", "disallow_transfer_to_peers"}, llmAgentFields...),
	},
	ClassSequential: {required: []string{"sub_agents"}, optional: []string{"description"}},
	ClassParallel:   {required: []string{"sub_agents"}, optional: []string{"description"}},
	ClassLoop:       {required: []string{"sub_agents"}, optional: []string{"description", "max_iterations"}},
}

func presentAgentFields(a *AgentSpec) map[string]bool {
	present := llmPresent(&a.LLMSpec)
	maps.Copy(present, map[string]bool{
		"description": a.Description != "", "sub_agents": len(a.SubAgents) > 0,
		"entry": a.Entry != "", "nodes": len(a.Nodes) > 0, "edges": len(a.Edges) > 0,
		"agent_tools":                 len(a.AgentTools) > 0,
		"disallow_transfer_to_parent": a.DisallowTransferToParent,
		"disallow_transfer_to_peers":  a.DisallowTransferToPeers,
		"max_iterations":              a.MaxIterations != 0,
		"max_concurrency":             a.MaxConcurrency != 0,
	})
	return present
}

func llmPresent(l *LLMSpec) map[string]bool {
	return map[string]bool{
		"model": l.Model != "", "mode": l.Mode != "",
		// A sourced instruction satisfies the instruction requirement.
		"instruction":        l.Instruction != "" || l.InstructionFrom != nil,
		"instruction_from":   l.InstructionFrom != nil,
		"global_instruction": l.GlobalInstruction != "", "global_instruction_from": l.GlobalInstructionFrom != nil,
		"callbacks": l.Callbacks != nil, "tools": len(l.Tools) > 0,
		"output_key": l.OutputKey != "", "output_schema": len(l.OutputSchema) > 0,
		"include_contents": l.IncludeContents != "",
	}
}

func (v *validator) agent(path string, a *AgentSpec, classes map[string]AgentClass) {
	for i, name := range a.SubAgents {
		p := fmt.Sprintf("%s.sub_agents[%d]", path, i)
		if _, ok := classes[name]; !ok {
			v.addf(p, "unknown agent %q", name)
		} else if name == a.Name {
			v.addf(p, "an agent cannot be its own sub-agent")
		}
	}

	seenTools := map[string]bool{}
	for i, name := range a.AgentTools {
		p := fmt.Sprintf("%s.agent_tools[%d]", path, i)
		switch {
		case classes[name] == "":
			v.addf(p, "unknown agent %q", name)
		case name == a.Name:
			v.addf(p, "an agent cannot be its own tool")
		case seenTools[name]:
			v.addf(p, "duplicate agent tool %q", name)
		}
		seenTools[name] = true
	}
	if a.AgentClass == ClassParallel {
		v.concurrentOutputKeys(path+".sub_agents", a.SubAgents, "sub-agents of a ParallelAgent")
	}

	rule, ok := agentRules[a.AgentClass]
	if !ok {
		v.addf(path+".agent_class", "must be one of %s, got %q", strings.Join(agentClassNames(), ", "), a.AgentClass)
		return
	}
	v.checkFields(path, "agent_class "+string(a.AgentClass), presentAgentFields(a), rule.required, rule.optional)

	switch a.AgentClass {
	case ClassWorkflow:
		if a.MaxConcurrency < 0 {
			v.addf(path+".max_concurrency", "must not be negative")
		}
		v.graph(path, a.Nodes, a.Edges, a.Entry, classes, 0, true)
	case ClassLlmAgent:
		v.llm(path, &a.LLMSpec)
	}
}

func agentClassNames() []string {
	names := make([]string, 0, len(agentRules))
	for c := range agentRules {
		names = append(names, string(c))
	}
	slices.Sort(names)
	return names
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
	if l.Model != "" {
		switch {
		case !modelName.MatchString(l.Model):
			v.addf(path+".model", "%q must match %s", l.Model, modelName)
		case v.refs != nil && !v.refs.hasModel(l.Model):
			v.addf(path+".model", "model %q is not registered", l.Model)
		}
	}
	if l.Instruction != "" && l.InstructionFrom != nil {
		v.addf(path+".instruction_from", "instruction and instruction_from are mutually exclusive")
	}
	if l.GlobalInstruction != "" && l.GlobalInstructionFrom != nil {
		v.addf(path+".global_instruction_from", "global_instruction and global_instruction_from are mutually exclusive")
	}
	v.textSource(path+".instruction_from", l.InstructionFrom)
	v.textSource(path+".global_instruction_from", l.GlobalInstructionFrom)
	v.callbacks(path+".callbacks", l.Callbacks)
	seen := map[string]bool{}
	for i, t := range l.Tools {
		p := fmt.Sprintf("%s.tools[%d]", path, i)
		switch {
		case seen[t]:
			v.addf(p, "duplicate tool %q", t)
		case v.servers[t] != nil:
		case v.refs != nil && !v.refs.hasToolOrToolset(t):
			v.addf(p, "tool %q is not registered or declared in mcp_servers", t)
		}
		seen[t] = true
	}
}

// NodeRule lists the NodeSpec fields a node type requires and accepts beyond
// the common ones (id, type, description). Field names are the JSON names.
type NodeRule struct {
	Required []string
	Optional []string
}

var builtinNodeRules = map[NodeType]NodeRule{
	NodeFunction: {Required: []string{"function"}, Optional: []string{"input_schema", "output_schema", "retry", "timeout"}},
	NodeJoin:     {Optional: []string{"input_schema"}},
	NodeTool:     {Required: []string{"tool"}, Optional: []string{"server", "retry", "timeout"}},
	NodeAgent:    {Required: []string{"agent"}, Optional: []string{"input_schema", "retry", "timeout"}},
	NodeLLM: {
		Required: []string{"model", "instruction"},
		Optional: []string{"mode", "global_instruction", "instruction_from", "global_instruction_from", "callbacks", "tools", "output_key", "input_schema", "output_schema", "include_contents", "retry", "timeout"},
	},
	NodeWorkflow:       {Required: []string{"nodes", "edges"}},
	NodeParallelWorker: {Optional: []string{"function", "tool", "agent", "max_concurrency", "retry", "timeout"}},
	NodeDynamic:        {Required: []string{"function"}, Optional: []string{"max_iters", "children", "isolation", "input_schema", "output_schema", "retry", "timeout"}},
	NodeHITL:           {Required: []string{"message"}},
	NodeCEL:            {Optional: []string{"vars", "output", "route", "routes", "state_delta", "input_schema", "output_schema", "retry", "timeout"}},
}

// structuredTerminals are node types whose output is structured data that no
// A2A client displays; a branch must not end on one.
var structuredTerminals = []NodeType{NodeTool, NodeJoin, NodeParallelWorker}

// presentFields names the type-specific fields a node sets.
func presentFields(n *NodeSpec) map[string]bool {
	present := llmPresent(&n.LLMSpec)
	maps.Copy(present, map[string]bool{
		"function": n.Function != "", "tool": n.Tool != "", "agent": n.Agent != "", "server": n.Server != "",
		"max_concurrency": n.MaxConcurrency != 0, "max_iters": n.MaxIters != 0,
		"children": len(n.Children) > 0, "isolation": n.Isolation != "", "message": n.Message != "",
		"input_schema": len(n.InputSchema) > 0,
		"nodes":        len(n.Nodes) > 0, "edges": len(n.Edges) > 0,
		"retry": n.Retry != nil, "timeout": n.Timeout != "",
		"vars": len(n.Vars) > 0, "output": n.Output != "", "route": n.Route != "",
		"routes": len(n.Routes) > 0, "state_delta": len(n.StateDelta) > 0,
	})
	return present
}

// graph validates one node/edge scope: the root of a Workflow agent or the
// body of a nested workflow node. terminal reports whether the scope's own
// terminal nodes end the run, which holds for the root scope and for nested
// scopes whose workflow node is itself terminal.
func (v *validator) graph(path string, nodes []NodeSpec, edges []EdgeSpec, entry string, classes map[string]AgentClass, depth int, terminal bool) {
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
	children := v.children(path, nodes, ids)

	agentTargets := make(map[string]string)
	for i := range nodes {
		n := &nodes[i]
		p := fmt.Sprintf("%s.nodes[%d]", path, i)
		v.node(p, n, classes, depth, terminal && !outgoing[n.ID] && !children[n.ID])
		if n.Agent != "" && (n.Type == NodeAgent || n.Type == NodeParallelWorker) {
			if prev, dup := agentTargets[n.Agent]; dup {
				v.addf(p+".agent", "agent %q is already used by node %q in this graph; ADK names an agent node after its agent", n.Agent, prev)
			}
			agentTargets[n.Agent] = n.ID
		}
	}
	for _, n := range nodes {
		if n.Type == NodeAgent && ids[n.Agent] {
			v.addf(path+".nodes", "agent %q collides with a node id in the same graph", n.Agent)
		}
	}

	v.edges(path, nodes, edges, entry, ids, children)
}

// children validates the children lists of dynamic nodes and returns the set
// of ids that some dynamic node owns.
func (v *validator) children(path string, nodes []NodeSpec, ids map[string]bool) map[string]bool {
	owned := map[string]bool{}
	for i := range nodes {
		n := &nodes[i]
		for j, child := range n.Children {
			p := fmt.Sprintf("%s.nodes[%d].children[%d]", path, i, j)
			switch {
			case !ids[child]:
				v.addf(p, "unknown node %q", child)
			case child == n.ID:
				v.addf(p, "a node cannot be its own child")
			default:
				owned[child] = true
			}
		}
	}
	return owned
}

func (v *validator) edges(path string, nodes []NodeSpec, edges []EdgeSpec, entry string, ids, children map[string]bool) {
	if entry != "" && !ids[entry] {
		v.addf(path+".entry", "unknown node %q", entry)
	}
	hasStart := entry != ""
	connected := map[string]bool{entry: true}
	for child := range children {
		connected[child] = true
	}
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
		for _, end := range []string{e.From, e.To} {
			if children[end] {
				v.addf(p, "node %q is a dynamic child and cannot have edges", end)
			}
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

func (v *validator) node(path string, n *NodeSpec, classes map[string]AgentClass, depth int, terminal bool) {
	rule, ok := v.rules[n.Type]
	if !ok {
		types := make([]string, 0, len(v.rules))
		for t := range v.rules {
			types = append(types, string(t))
		}
		slices.Sort(types)
		v.addf(path+".type", "unknown node type %q (known: %s)", n.Type, strings.Join(types, ", "))
		return
	}
	v.checkFields(path, "node type "+string(n.Type), presentFields(n), rule.Required, rule.Optional)

	v.nodeSchemas(path, n)

	if terminal && slices.Contains(structuredTerminals, n.Type) {
		v.addf(path, "terminal %s node emits structured output that A2A clients do not display; end the branch with a function node", n.Type)
	}

	switch n.Type {
	case NodeFunction:
		v.function(path, n.Function)
	case NodeTool:
		if n.Server != "" {
			v.mcpToolNode(path, n)
		} else {
			v.toolNode(path, n.Tool)
		}
	case NodeParallelWorker:
		v.parallelWorker(path, n, classes)
	case NodeDynamic:
		if n.MaxIters < 0 {
			v.addf(path+".max_iters", "must not be negative")
		}
		switch n.Isolation {
		case "", IsolationShared, IsolationBranch, IsolationScope:
		default:
			v.addf(path+".isolation", "must be one of shared, branch, scope, got %q", n.Isolation)
		}
		if n.Function != "" && v.refs != nil && !v.refs.hasDynamic(n.Function) {
			v.addf(path+".function", "dynamic function %q is not registered", n.Function)
		}
	case NodeLLM:
		v.llm(path, &n.LLMSpec)
		if n.Mode != "" && n.Mode != ModeSingleTurn {
			v.addf(path+".mode", "an inline llm node runs mid-graph and must be single_turn, got %q", n.Mode)
		}
	case NodeAgent:
		v.agentTarget(path+".agent", n.Agent, classes)
	case NodeWorkflow:
		v.graph(path, n.Nodes, n.Edges, "", classes, depth+1, terminal)
	case NodeCEL:
		v.celNode(path, n)
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

func (v *validator) function(path, name string) {
	if name != "" && v.refs != nil && !v.refs.hasFunction(name) {
		v.addf(path+".function", "function %q is not registered", name)
	}
}

func (v *validator) toolNode(path, name string) {
	switch {
	case name == "":
	case !toolName.MatchString(name):
		v.addf(path+".tool", "%q must match %s", name, toolName)
	case v.refs != nil && !v.refs.hasTool(name):
		v.addf(path+".tool", "tool %q is not registered", name)
	}
}

func (v *validator) agentTarget(path, name string, classes map[string]AgentClass) {
	if name == "" {
		return
	}
	class, known := classes[name]
	switch {
	case !known:
		v.addf(path, "unknown agent %q", name)
	case class == ClassWorkflow:
		v.addf(path, "agent %q is a Workflow; use a workflow node to nest graphs", name)
	}
}

func (v *validator) parallelWorker(path string, n *NodeSpec, classes map[string]AgentClass) {
	targets := 0
	for _, set := range []bool{n.Function != "", n.Tool != "", n.Agent != ""} {
		if set {
			targets++
		}
	}
	if targets != 1 {
		v.addf(path, "parallel_worker needs exactly one of function, tool or agent, got %d", targets)
	}
	if n.MaxConcurrency < 0 {
		v.addf(path+".max_concurrency", "must not be negative")
	}
	v.function(path, n.Function)
	v.toolNode(path, n.Tool)
	v.agentTarget(path+".agent", n.Agent, classes)
	if target := v.agents[n.Agent]; target != nil && target.OutputKey != "" {
		v.addf(path+".agent", "every concurrent item writes the state key %q of agent %q; drop its output_key and read the worker's output", target.OutputKey, n.Agent)
	}
}

func (v *validator) retry(path string, r *RetrySpec) {
	if r.MaxAttempts < 0 {
		v.addf(path+".max_attempts", "must not be negative")
	}
	for field, value := range map[string]string{"initial_delay": r.InitialDelay, "max_delay": r.MaxDelay} {
		if value == "" {
			continue
		}
		if d, err := time.ParseDuration(value); err != nil || d < 0 {
			v.addf(path+"."+field, "must be a non-negative duration such as 1s, got %q", value)
		}
	}
	if r.BackoffFactor != nil && *r.BackoffFactor <= 0 {
		v.addf(path+".backoff_factor", "must be positive")
	}
	if r.Jitter != nil && (*r.Jitter < 0 || *r.Jitter > 1) {
		v.addf(path+".jitter", "must be within [0, 1]")
	}
	if r.When != "" {
		if _, err := compileCEL(scopeRetry, r.When, resultBool); err != nil {
			v.addf(path+".when", "%v", err)
		}
	}
}

// agentTree checks that sub_agents form a forest: ADK names must be unique
// within an agent tree and a sub-agent has one parent.
func (v *validator) agentTree(spec *Spec) {
	parent := map[string]string{}
	for i, a := range spec.Agents {
		for j, sub := range a.SubAgents {
			p := fmt.Sprintf("agents[%d].sub_agents[%d]", i, j)
			if prev, taken := parent[sub]; taken {
				v.addf(p, "agent %q is already a sub-agent of %q; an agent has one parent", sub, prev)
				continue
			}
			parent[sub] = a.Name
		}
	}
	for i, a := range spec.Agents {
		for j, tool := range a.AgentTools {
			if owner, isSub := parent[tool]; isSub {
				v.addf(fmt.Sprintf("agents[%d].agent_tools[%d]", i, j), "agent %q is a sub-agent of %q; an agent is either a sub-agent (shared session) or an agent tool (its own session)", tool, owner)
			}
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

// concurrentOutputKeys reports agents that run side by side and write the same
// session state key. Branches isolate history, not state: a shared key is
// written by whichever branch finishes last.
func (v *validator) concurrentOutputKeys(path string, agentNames []string, what string) {
	owner := map[string]string{}
	for i, name := range agentNames {
		target := v.agents[name]
		if target == nil || target.OutputKey == "" {
			continue
		}
		if prev, taken := owner[target.OutputKey]; taken {
			v.addf(fmt.Sprintf("%s[%d]", path, i), "%s %q and %q both write the state key %q; give each its own output_key", what, prev, name, target.OutputKey)
			continue
		}
		owner[target.OutputKey] = name
	}
}

// fanOutOutputKeys applies the same rule to the unconditional successors of
// one node, which the engine starts concurrently.
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
		targets := successors[from]
		if len(targets) < 2 {
			continue
		}
		owner := map[string]string{}
		for _, id := range targets {
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

// agentReferences rejects cycles through agent references. sub_agents cycles
// have their own message; this reports the ones that also pass through an
// agent tool or an agent node, which would recurse without end when built.
func (v *validator) agentReferences(spec *Spec) {
	type edge struct{ to, kind string }
	uses := map[string][]edge{}
	for _, a := range spec.Agents {
		for _, s := range a.SubAgents {
			uses[a.Name] = append(uses[a.Name], edge{s, "sub_agents"})
		}
		for _, t := range a.AgentTools {
			uses[a.Name] = append(uses[a.Name], edge{t, "agent_tools"})
		}
		for _, target := range nodeAgents(a.Nodes) {
			uses[a.Name] = append(uses[a.Name], edge{target, "an agent node"})
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
	var kinds []string
	var visit func(name string)
	visit = func(name string) {
		state[name] = onStack
		stack = append(stack, name)
		for _, e := range uses[name] {
			switch state[e.to] {
			case unvisited:
				kinds = append(kinds, e.kind)
				visit(e.to)
				kinds = kinds[:len(kinds)-1]
			case onStack:
				start := slices.Index(stack, e.to)
				cycleKinds := append(slices.Clone(kinds[start:]), e.kind)
				members := append(slices.Clone(stack[start:]), e.to)
				if slices.ContainsFunc(cycleKinds, func(k string) bool { return k != "sub_agents" }) && !reported[e.to] {
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
		n := &nodes[i]
		if n.Agent != "" {
			out = append(out, n.Agent)
		}
		out = append(out, nodeAgents(n.Nodes)...)
	}
	return out
}

var (
	dnsLabel = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?(\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*$`)
	dataKey  = regexp.MustCompile(`^[-._a-zA-Z0-9]+$`)
	envName  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

// textSource checks a prompt source. The runtime only reads env sources:
// config_map and secret are Kubernetes references that the kagent controller
// resolves before delivery, so a registry-backed validation rejects them.
func (v *validator) textSource(path string, src *TextSource) {
	if src == nil {
		return
	}
	set := 0
	for _, ref := range []struct {
		name string
		key  *KeyRef
	}{{"config_map", src.ConfigMap}, {"secret", src.Secret}} {
		if ref.key == nil {
			continue
		}
		set++
		p := path + "." + ref.name
		if len(ref.key.Name) > 253 || !dnsLabel.MatchString(ref.key.Name) {
			v.addf(p+".name", "%q must be a DNS-1123 subdomain", ref.key.Name)
		}
		if len(ref.key.Key) > 253 || !dataKey.MatchString(ref.key.Key) {
			v.addf(p+".key", "%q must match %s", ref.key.Key, dataKey)
		}
		if v.refs != nil {
			v.addf(p, "must be resolved by the kagent controller; the runtime reads only env sources")
		}
	}
	if src.Env != "" {
		set++
		if !envName.MatchString(src.Env) {
			v.addf(path+".env", "%q must match %s", src.Env, envName)
		}
	}
	if set != 1 {
		v.addf(path, "must set exactly one of config_map, secret, env")
	}
}

func (v *validator) mcpEnvSource(path string, src *TextSource) {
	if src == nil {
		return
	}
	if src.Env == "" || src.ConfigMap != nil || src.Secret != nil {
		v.addf(path, "must use env; config_map and secret are not supported for MCP sources")
		return
	}
	if !envName.MatchString(src.Env) {
		v.addf(path+".env", "%q must match %s", src.Env, envName)
	}
}

func (v *validator) callbacks(path string, cb *CallbacksSpec) {
	if cb == nil {
		return
	}
	for _, point := range []struct {
		scope celScope
		rules []HookSpec
	}{
		{scopeBeforeModel, cb.BeforeModel}, {scopeAfterModel, cb.AfterModel},
		{scopeBeforeTool, cb.BeforeTool}, {scopeAfterTool, cb.AfterTool},
	} {
		for i, h := range point.rules {
			v.hook(fmt.Sprintf("%s.%s[%d]", path, point.scope, i), point.scope, &h)
		}
	}
}

func (v *validator) hook(path string, scope celScope, h *HookSpec) {
	switch {
	case h.Action != "" && h.Function != "":
		v.addf(path, "action and function are mutually exclusive")
	case h.Action == "" && h.Function == "":
		v.addf(path, "must set one of action or function")
	}
	switch h.Action {
	case "", HookAccept:
		if h.Message != "" {
			v.addf(path+".message", "only a reject action carries a message")
		}
	case HookReject:
		if h.Message == "" {
			v.addf(path+".message", "a reject action requires a message")
		}
	default:
		v.addf(path+".action", "must be accept or reject, got %q", h.Action)
	}
	if h.Function != "" {
		switch {
		case !identifier.MatchString(h.Function):
			v.addf(path+".function", "%q must match %s", h.Function, identifier)
		case v.refs != nil && v.refs.hooks[h.Function] == nil:
			v.addf(path+".function", "hook %q is not registered", h.Function)
		}
	}
	if h.When != "" {
		if _, err := compileCEL(scope, h.When, resultBool); err != nil {
			v.addf(path+".when", "%v", err)
		}
	}
}

func (v *validator) celNode(path string, n *NodeSpec) {
	if n.Output == "" && n.Route == "" && len(n.Routes) == 0 && len(n.StateDelta) == 0 {
		v.addf(path, "a cel node must set at least one of output, route, routes, state_delta")
	}
	names := map[string]bool{}
	for i, cv := range n.Vars {
		p := fmt.Sprintf("%s.vars[%d]", path, i)
		// A var reads only the vars declared before it.
		v.celExpr(p+".expr", cv.Expr, resultAny, names)
		switch {
		case !celIdent.MatchString(cv.Name):
			v.addf(p+".name", "must be an identifier, got %q", cv.Name)
		case names[cv.Name]:
			v.addf(p+".name", "duplicate var %q", cv.Name)
		}
		names[cv.Name] = true
	}
	if n.Route != "" && len(n.Routes) > 0 {
		v.addf(path, "route and routes are mutually exclusive")
	}
	for i, r := range n.Routes {
		p := fmt.Sprintf("%s.routes[%d]", path, i)
		if r.Route == "" {
			v.addf(p+".route", "required")
		}
		if r.When == "" {
			if i != len(n.Routes)-1 {
				v.addf(p+".when", "only the last rule may omit when; later rules are unreachable")
			}
			continue
		}
		v.celExpr(p+".when", r.When, resultBool, names)
	}
	if n.Output != "" {
		v.celExpr(path+".output", n.Output, resultAny, names)
	}
	if n.Route != "" {
		v.celExpr(path+".route", n.Route, resultRoutes, names)
	}
	for _, k := range slices.Sorted(maps.Keys(n.StateDelta)) {
		p := path + ".state_delta." + k
		if k == "" {
			v.addf(p, "state keys must not be empty")
			continue
		}
		v.celExpr(p, n.StateDelta[k], resultAny, names)
	}
}

// celExpr compiles a cel node expression and checks that every vars.<name>
// it reads is in known.
func (v *validator) celExpr(path, src string, want celResult, known map[string]bool) {
	e, err := compileCEL(scopeNode, src, want)
	if err != nil {
		v.addf(path, "%v", err)
		return
	}
	for _, name := range e.varRefs {
		if !known[name] {
			v.addf(path, "vars.%s is not declared before this expression", name)
		}
	}
}

var celIdent = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var headerName = regexp.MustCompile("^[!#$%&'*+.^_`|~0-9A-Za-z-]+$")

func (v *validator) mcpServers(servers []MCPServerSpec) {
	v.servers = make(map[string]*MCPServerSpec, len(servers))
	for i := range servers {
		s := &servers[i]
		path := fmt.Sprintf("mcp_servers[%d]", i)
		switch {
		case !identifier.MatchString(s.Name):
			v.addf(path+".name", "%q must match %s", s.Name, identifier)
		case v.servers[s.Name] != nil:
			v.addf(path+".name", "duplicate mcp server %q", s.Name)
		case v.refs != nil && v.refs.hasToolOrToolset(s.Name):
			v.addf(path+".name", "%q is already a registered tool or toolset", s.Name)
		default:
			v.servers[s.Name] = s
		}
		switch {
		case (s.URL == "") == (s.URLFrom == nil):
			v.addf(path, "must set exactly one of url, url_from")
		case s.URL != "":
			if u, err := url.Parse(s.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				v.addf(path+".url", "%q must be an absolute http or https URL", s.URL)
			}
		}
		v.mcpEnvSource(path+".url_from", s.URLFrom)
		seen := map[string]bool{}
		for j, h := range s.Headers {
			p := fmt.Sprintf("%s.headers[%d]", path, j)
			key := strings.ToLower(h.Name)
			switch {
			case !headerName.MatchString(h.Name):
				v.addf(p+".name", "%q is not an HTTP header name", h.Name)
			case seen[key]:
				v.addf(p+".name", "duplicate header %q", h.Name)
			}
			seen[key] = true
			if (h.Value == "") == (h.ValueFrom == nil) {
				v.addf(p, "must set exactly one of value, value_from")
			}
			v.mcpEnvSource(p+".value_from", h.ValueFrom)
		}
		tools := map[string]bool{}
		for j, t := range s.Tools {
			p := fmt.Sprintf("%s.tools[%d]", path, j)
			switch {
			case !toolName.MatchString(t):
				v.addf(p, "%q must match %s", t, toolName)
			case tools[t]:
				v.addf(p, "duplicate tool %q", t)
			}
			tools[t] = true
		}
	}
}

// mcpToolNode checks a tool node that calls a tool of a server. Whether the
// server offers the tool is only known when it runs, unless the server
// declares an allowlist.
func (v *validator) mcpToolNode(path string, n *NodeSpec) {
	if n.Tool != "" && !toolName.MatchString(n.Tool) {
		v.addf(path+".tool", "%q must match %s", n.Tool, toolName)
	}
	if s := v.servers[n.Server]; s != nil {
		if len(s.Tools) > 0 && !slices.Contains(s.Tools, n.Tool) {
			v.addf(path+".tool", "tool %q is not in the tools of mcp server %q", n.Tool, n.Server)
		}
		return
	}
	if v.refs == nil || !v.refs.hasToolset(n.Server) {
		v.addf(path+".server", "server %q is not declared in mcp_servers", n.Server)
	}
}
