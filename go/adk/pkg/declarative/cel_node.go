package declarative

import (
	"fmt"
	"slices"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/workflow"
)

// celProgram is a compiled cel node.
type celProgram struct {
	output *expression
	route  *expression
	delta  map[string]*expression
	keys   []string
}

func compileCELNode(n *NodeSpec) (*celProgram, error) {
	p := &celProgram{delta: map[string]*expression{}}
	var err error
	if n.Output != "" {
		if p.output, err = compileCEL(scopeNode, n.Output, resultAny); err != nil {
			return nil, fmt.Errorf("output: %w", err)
		}
	}
	if n.Route != "" {
		if p.route, err = compileCEL(scopeNode, n.Route, resultRoutes); err != nil {
			return nil, fmt.Errorf("route: %w", err)
		}
	}
	for key, src := range n.StateDelta {
		if p.delta[key], err = compileCEL(scopeNode, src, resultAny); err != nil {
			return nil, fmt.Errorf("state_delta.%s: %w", key, err)
		}
		p.keys = append(p.keys, key)
	}
	slices.Sort(p.keys)
	return p, nil
}

// celNode evaluates expressions over the node input and the ADK context. Its
// output defaults to its input, so a node that only routes or writes state
// passes data through unchanged. All expressions see the state as it was
// before the node ran.
func celNode(n *NodeSpec, in, out *jsonschema.Schema, cfg workflow.NodeConfig, render bool) (workflow.Node, error) {
	p, err := compileCELNode(n)
	if err != nil {
		return nil, err
	}
	body := func(ctx agent.Context, input any) (any, error) {
		vars := contextVars(ctx)
		vars[celInput] = plainValue(input)
		result := input
		if p.output != nil {
			if result, err = p.output.eval(vars); err != nil {
				return nil, err
			}
		}
		var routes []string
		if p.route != nil {
			if routes, err = p.route.evalRoutes(vars); err != nil {
				return nil, err
			}
		}
		var delta map[string]any
		if len(p.keys) > 0 {
			delta = make(map[string]any, len(p.keys))
			for _, k := range p.keys {
				if delta[k], err = p.delta[k].eval(vars); err != nil {
					return nil, fmt.Errorf("state_delta.%s: %w", k, err)
				}
			}
		}
		if len(routes) == 0 && delta == nil && (!render || result == nil) {
			return result, nil
		}
		ev := resultEvent(ctx, result, routes, render)
		ev.Actions.StateDelta = delta
		return ev, nil
	}
	return workflow.NewFunctionNodeWithSchema[any, any](n.ID, body, in, out, cfg)
}
