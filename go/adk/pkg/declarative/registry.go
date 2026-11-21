package declarative

import (
	"errors"
	"fmt"
	"regexp"

	"google.golang.org/adk/v2/agent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
)

// StubModel is the name of the built-in offline model, the only value the
// model field accepts.
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

// Registry maps the names a document uses to Go values and holds the model
// the workflow runs on. It is populated before Build and read-only
// afterwards; it is not safe for concurrent registration.
type Registry struct {
	model adkmodel.LLM
	stub  adkmodel.LLM
	funcs map[string]Func
	tools map[string]tool.Tool
}

// NewRegistry returns a Registry whose LLMs run on model. model may be nil
// for documents whose LLMs all use the stub model, or that have none.
func NewRegistry(model adkmodel.LLM) *Registry {
	return &Registry{
		model: model,
		stub:  NewStubModel(StubModel),
		funcs: map[string]Func{},
		tools: map[string]tool.Tool{},
	}
}

// register adds v under name after checking the name's shape and uniqueness.
func register[V any](kind, name string, valid *regexp.Regexp, m map[string]V, v V) error {
	if !valid.MatchString(name) {
		return fmt.Errorf("failed to register %s %q: name must match %s", kind, name, valid)
	}
	if _, dup := m[name]; dup {
		return fmt.Errorf("failed to register %s %q: already registered", kind, name)
	}
	m[name] = v
	return nil
}

// RegisterFunction makes fn available to function nodes.
func (r *Registry) RegisterFunction(name string, fn Func) error {
	if fn == nil {
		return fmt.Errorf("failed to register function %q: nil func", name)
	}
	return register("function", name, identifier, r.funcs, fn)
}

// RegisterTool makes t available to tool nodes and to the tools list of
// LLMs under t.Name(), the name the model sees.
func (r *Registry) RegisterTool(t tool.Tool) error {
	if t == nil {
		return errors.New("failed to register tool: nil tool")
	}
	return register("tool", t.Name(), toolName, r.tools, t)
}

// Validate is the package-level Validate extended with a check that every
// function, tool and model a document names resolves against r. All issues
// are reported together with their field paths.
func (r *Registry) Validate(spec *Spec) error {
	return validate(spec, r)
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

func (r *Registry) hasFunction(name string) bool { _, ok := r.funcs[name]; return ok }
func (r *Registry) hasTool(name string) bool     { _, ok := r.tools[name]; return ok }

// resolveModel returns the model an LLM runs on. Validation has already
// rejected every name except StubModel and an unset model without a
// registry model.
func (r *Registry) resolveModel(name string) (adkmodel.LLM, error) {
	switch {
	case name == StubModel:
		return r.stub, nil
	case name == "" && r.model != nil:
		return r.model, nil
	}
	return nil, fmt.Errorf("failed to resolve model %q: %s", name, modelHint)
}

// modelHint explains the single-model rule in validation and build errors.
const modelHint = `omit model to use the registry's model, or set "stub" for the offline test model`
