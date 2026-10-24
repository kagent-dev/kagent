package declarative

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"google.golang.org/adk/v2/agent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/loadmemorytool"
	"google.golang.org/adk/v2/tool/preloadmemorytool"
	"google.golang.org/adk/v2/workflow"
)

// StubModel is the name of the built-in offline model.
const StubModel = "stub"

// Result is what a Func hands back to the workflow engine.
type Result struct {
	// Output is passed to the successor nodes as their input.
	Output any
	// Routes selects the outgoing edges whose route matches. An empty slice
	// takes every unconditional edge.
	Routes []string
}

// Output returns a Result that only carries a value.
func Output(v any) Result { return Result{Output: v} }

// Route returns a Result that carries a value and selects routes.
func Route(v any, routes ...string) Result { return Result{Output: v, Routes: routes} }

// Func is the behavior behind a function node. Keep it free of I/O that the
// caller cannot retry: a node with a retry policy may run it again.
type Func func(ctx agent.Context, input any) (Result, error)

// ErrIterationBudget is returned by DynamicEnv.Run once max_iters child runs
// were spent.
var ErrIterationBudget = errors.New("dynamic node exhausted max_iters")

// DynamicFunc is the orchestrator behind a dynamic node. It decides at run
// time which children to run and in which order.
type DynamicFunc func(ctx agent.Context, input any, env *DynamicEnv) (any, error)

// DynamicEnv is the capability set handed to a DynamicFunc.
type DynamicEnv struct {
	// MaxIters is the node's max_iters; zero means unlimited.
	MaxIters int
	// Emit publishes an intermediate event such as a state delta.
	Emit func(*session.Event) error

	ctx       agent.Context
	children  map[string]workflow.Node
	isolation Isolation
	runs      int
}

// Run executes child as part of this node and returns its output, isolated as
// the node's isolation says. Each call spends one iteration of the max_iters
// budget.
func (e *DynamicEnv) Run(child workflow.Node, input any) (any, error) {
	if e.MaxIters > 0 && e.runs >= e.MaxIters {
		return nil, ErrIterationBudget
	}
	e.runs++
	return workflow.RunNode[any](e.ctx, child, input, e.runOptions()...)
}

// runOptions translates the node's isolation into engine options. Shared adds
// none: the child runs on the dynamic node's own branch.
func (e *DynamicEnv) runOptions() []workflow.RunNodeOption {
	switch e.isolation {
	case IsolationBranch:
		return []workflow.RunNodeOption{workflow.WithUseSubBranch()}
	case IsolationScope:
		return []workflow.RunNodeOption{workflow.WithUseSubBranch(), workflow.WithIsolationScopeFromNodePath()}
	}
	return nil
}

// RunChild executes the child node declared under id in the node's children
// list. It spends one iteration like Run.
func (e *DynamicEnv) RunChild(id string, input any) (any, error) {
	child, ok := e.children[id]
	if !ok {
		return nil, fmt.Errorf("dynamic node has no child %q", id)
	}
	return e.Run(child, input)
}

// Runs reports how many iterations were spent so far.
func (e *DynamicEnv) Runs() int { return e.runs }

// ModelResolver resolves model names that are not registered by value, for
// example by delegating to the provider factories of the kagent runtime.
type ModelResolver func(ctx context.Context, name string) (adkmodel.LLM, error)

// BuildContext is what a NodeBuilder may use to construct its node.
type BuildContext struct {
	Context  context.Context
	Registry *Registry
	// Agent returns the built agent of the document, building it first when
	// needed. It is the only way to obtain a sibling agent.
	Agent func(name string) (agent.Agent, error)
}

// NodeBuilder builds the workflow node for one NodeSpec of a registered type.
type NodeBuilder func(bc *BuildContext, spec *NodeSpec) (workflow.Node, error)

type nodeType struct {
	rule  NodeRule
	build NodeBuilder
}

// Registry maps the names a document uses to Go values. It is populated
// before Build and read-only afterwards; it is not safe for concurrent
// registration.
type Registry struct {
	funcs     map[string]Func
	dynamics  map[string]DynamicFunc
	tools     map[string]tool.Tool
	toolsets  map[string]tool.Toolset
	models    map[string]adkmodel.LLM
	resolver  ModelResolver
	nodeTypes map[NodeType]nodeType
}

// NewRegistry returns a Registry preloaded with the offline stub model and
// the ADK memory tools (preload_memory, load_memory).
func NewRegistry() *Registry {
	r := &Registry{
		funcs:     map[string]Func{},
		dynamics:  map[string]DynamicFunc{},
		tools:     map[string]tool.Tool{},
		toolsets:  map[string]tool.Toolset{},
		models:    map[string]adkmodel.LLM{},
		nodeTypes: map[NodeType]nodeType{},
	}
	r.models[StubModel] = NewStubModel(StubModel)
	for _, t := range []tool.Tool{preloadmemorytool.New(), loadmemorytool.New()} {
		r.tools[t.Name()] = t
	}
	return r
}

// register adds v under name after checking the name's shape, uniqueness, and
// that no registration kept in another table already uses it.
func register[V any](kind, name string, valid *regexp.Regexp, m map[string]V, v V, taken ...func(string) bool) error {
	if !valid.MatchString(name) {
		return fmt.Errorf("failed to register %s %q: name must match %s", kind, name, valid)
	}
	if _, dup := m[name]; dup {
		return fmt.Errorf("failed to register %s %q: already registered", kind, name)
	}
	for _, isTaken := range taken {
		if isTaken(name) {
			return fmt.Errorf("failed to register %s %q: name is used by another registration", kind, name)
		}
	}
	m[name] = v
	return nil
}

// RegisterFunction makes fn available to function and parallel_worker nodes.
func (r *Registry) RegisterFunction(name string, fn Func) error {
	if fn == nil {
		return fmt.Errorf("failed to register function %q: nil func", name)
	}
	return register("function", name, identifier, r.funcs, fn)
}

// RegisterDynamic makes fn available to dynamic nodes.
func (r *Registry) RegisterDynamic(name string, fn DynamicFunc) error {
	if fn == nil {
		return fmt.Errorf("failed to register dynamic function %q: nil func", name)
	}
	return register("dynamic function", name, identifier, r.dynamics, fn)
}

// RegisterTool makes t available to tool nodes and to the tools list of
// LLM agents under t.Name(), the name the model sees.
func (r *Registry) RegisterTool(t tool.Tool) error {
	if t == nil {
		return errors.New("failed to register tool: nil tool")
	}
	return register("tool", t.Name(), toolName, r.tools, t, func(n string) bool { _, ok := r.toolsets[n]; return ok })
}

// RegisterToolset makes ts, for example an MCP toolset, available to the
// tools list of LLM agents under ts.Name().
func (r *Registry) RegisterToolset(ts tool.Toolset) error {
	if ts == nil {
		return errors.New("failed to register toolset: nil toolset")
	}
	return register("toolset", ts.Name(), toolName, r.toolsets, ts, func(n string) bool { _, ok := r.tools[n]; return ok })
}

// RegisterModel makes m available under name.
func (r *Registry) RegisterModel(name string, m adkmodel.LLM) error {
	if m == nil {
		return fmt.Errorf("failed to register model %q: nil model", name)
	}
	if !modelName.MatchString(name) {
		return fmt.Errorf("failed to register model %q: name must match %s", name, modelName)
	}
	if _, dup := r.models[name]; dup {
		return fmt.Errorf("failed to register model %q: already registered", name)
	}
	r.models[name] = m
	return nil
}

// SetModelResolver installs the fallback used for model names that were not
// registered by value. Registered models always win.
func (r *Registry) SetModelResolver(resolve ModelResolver) { r.resolver = resolve }

// RegisterNodeType adds a node type to the vocabulary of documents. rule
// declares which NodeSpec fields it takes; build constructs the node. Built-in
// types cannot be replaced.
func (r *Registry) RegisterNodeType(t NodeType, rule NodeRule, build NodeBuilder) error {
	if _, builtin := builtinNodeRules[t]; builtin {
		return fmt.Errorf("failed to register node type %q: built-in types cannot be replaced", t)
	}
	if !identifier.MatchString(string(t)) {
		return fmt.Errorf("failed to register node type %q: name must match %s", t, identifier)
	}
	if build == nil {
		return fmt.Errorf("failed to register node type %q: nil builder", t)
	}
	if _, dup := r.nodeTypes[t]; dup {
		return fmt.Errorf("failed to register node type %q: already registered", t)
	}
	r.nodeTypes[t] = nodeType{rule: rule, build: build}
	return nil
}

// Validate is the package-level Validate extended with the vocabulary of r:
// its node types, and a check that every function, tool and model a document
// names is registered. All issues are reported together with their field
// paths.
func (r *Registry) Validate(spec *Spec) error {
	return validate(spec, r.rules(), r)
}

// Parse decodes a document and validates it against r.
func (r *Registry) Parse(data []byte) (*Spec, error) {
	spec, err := decode(data)
	if err != nil {
		return nil, err
	}
	if err := r.Validate(spec); err != nil {
		return nil, err
	}
	return spec, nil
}

func (r *Registry) rules() map[NodeType]NodeRule {
	rules := make(map[NodeType]NodeRule, len(r.nodeTypes))
	for t, nt := range r.nodeTypes {
		rules[t] = nt.rule
	}
	return rules
}

func (r *Registry) hasFunction(name string) bool { _, ok := r.funcs[name]; return ok }
func (r *Registry) hasDynamic(name string) bool  { _, ok := r.dynamics[name]; return ok }
func (r *Registry) hasTool(name string) bool     { _, ok := r.tools[name]; return ok }

func (r *Registry) hasToolOrToolset(name string) bool {
	_, ok := r.toolsets[name]
	return ok || r.hasTool(name)
}

// hasModel reports whether name can be resolved. With a resolver installed any
// name may resolve, and the resolver's own error surfaces from Build.
func (r *Registry) hasModel(name string) bool {
	_, ok := r.models[name]
	return ok || r.resolver != nil
}

func (r *Registry) resolveModel(ctx context.Context, name string) (adkmodel.LLM, error) {
	if m, ok := r.models[name]; ok {
		return m, nil
	}
	if r.resolver != nil {
		m, err := r.resolver(ctx, name)
		if err != nil {
			return nil, fmt.Errorf("failed to resolve model %q: %w", name, err)
		}
		return m, nil
	}
	return nil, fmt.Errorf("model %q is not registered", name)
}

// resolveTools expands tool names into tools and toolsets.
func (r *Registry) resolveTools(names []string) ([]tool.Tool, []tool.Toolset, error) {
	var tools []tool.Tool
	var toolsets []tool.Toolset
	for _, name := range names {
		if t, ok := r.tools[name]; ok {
			tools = append(tools, t)
		} else if ts, ok := r.toolsets[name]; ok {
			toolsets = append(toolsets, ts)
		} else {
			return nil, nil, fmt.Errorf("tool %q is not registered", name)
		}
	}
	return tools, toolsets, nil
}
