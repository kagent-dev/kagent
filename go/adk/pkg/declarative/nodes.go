package declarative

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/jsonschema-go/jsonschema"
	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"
)

// node dispatches on the node type. render is set for terminal nodes whose
// output must be readable as message text.
func (b *builder) node(n *NodeSpec, render bool) (workflow.Node, error) {
	cfg, err := nodeConfig(n)
	if err != nil {
		return nil, err
	}
	in, out, err := nodeSchemas(n)
	if err != nil {
		return nil, err
	}
	switch n.Type {
	case NodeFunction:
		return workflow.NewFunctionNodeWithSchema[any, any](n.ID, functionBody(b.reg.funcs[n.Function], render), in, out, cfg)
	case NodeJoin:
		return joinNode(n.ID, in)
	case NodeTool:
		return workflow.NewNamedToolNode(n.ID, b.reg.tools[n.Tool], cfg)
	case NodeAgent:
		a, err := b.agent(n.Agent)
		if err != nil {
			return nil, err
		}
		// ADK names an agent node after its agent, so the document id of
		// such a node is not visible to the engine.
		return workflow.NewAgentNodeWithSchemas(a, in, nil, cfg)
	case NodeLLM:
		mode := n.Mode
		if mode == "" {
			mode = ModeSingleTurn
		}
		a, err := b.newLLM(n.ID, n.Description, &n.LLMSpec, mode, nil, false, false)
		if err != nil {
			return nil, err
		}
		return workflow.NewAgentNodeWithSchemas(a, in, nil, cfg)
	case NodeWorkflow:
		nodes, err := b.nodes(n.Nodes, n.Edges, render)
		if err != nil {
			return nil, err
		}
		return workflow.NewWorkflowNode(n.ID, toEdges(nodes, n.Edges, ""))
	}
	return nil, fmt.Errorf("node type %q is not supported", n.Type)
}

// functionBody adapts a Func to a plain function node, as a native
// workflow.NewFunctionNode would be written. A plain node emits data only, so
// intermediate steps stay out of the chat; a result that carries routes, or
// that ends the run, is returned as an event the engine yields unchanged.
func functionBody(fn Func, render bool) func(agent.Context, any) (any, error) {
	return func(ctx agent.Context, in any) (any, error) {
		res, err := fn(ctx, in)
		if err != nil {
			return nil, err
		}
		if len(res.Routes) == 0 && (!render || res.Output == nil) {
			return res.Output, nil
		}
		return resultEvent(ctx, res.Output, res.Routes, render), nil
	}
}

// resultEvent builds one event carrying a node's output and routes. A2A
// clients display only Event.Content while successor nodes read Event.Output,
// so a terminal result is also rendered as message text: strings as they are,
// structured values as JSON.
func resultEvent(ctx agent.Context, out any, routes []string, render bool) *session.Event {
	ev := session.NewEvent(ctx, ctx.InvocationID())
	ev.Routes = routes
	ev.Output = out
	if render && out != nil {
		ev.Content = &genai.Content{Parts: []*genai.Part{{Text: outputText(out)}}}
	}
	return ev
}

// outputText renders a structured output for display.
func outputText(out any) string {
	if s, ok := out.(string); ok {
		return s
	}
	if data, err := json.Marshal(out); err == nil {
		return string(data)
	}
	return fmt.Sprint(out)
}

// nodeSchemas parses the node's contracts. Validate already resolved them.
// An inline llm node's output_schema is the model's reply schema, handled when
// the agent is built, so it is not a node contract here.
func nodeSchemas(n *NodeSpec) (in, out *jsonschema.Schema, err error) {
	if len(n.InputSchema) > 0 {
		if in, err = parseSchema(n.InputSchema); err != nil {
			return nil, nil, fmt.Errorf("failed to parse input_schema: %w", err)
		}
	}
	if n.Type != NodeLLM && len(n.OutputSchema) > 0 {
		if out, err = parseSchema(n.OutputSchema); err != nil {
			return nil, nil, fmt.Errorf("failed to parse output_schema: %w", err)
		}
	}
	return in, out, nil
}

// joinNode applies the schema to each predecessor's output, as the engine does.
func joinNode(id string, in *jsonschema.Schema) (workflow.Node, error) {
	if in == nil {
		return workflow.NewJoinNode(id), nil
	}
	resolved, err := in.Resolve(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve input_schema: %w", err)
	}
	return workflow.NewJoinNodeWithSchema(id, resolved), nil
}

// nodeConfig translates the common retry and timeout fields. Validate has
// already checked the syntax of every duration.
func nodeConfig(n *NodeSpec) (workflow.NodeConfig, error) {
	var cfg workflow.NodeConfig
	if n.Timeout != "" {
		d, err := time.ParseDuration(n.Timeout)
		if err != nil {
			return cfg, fmt.Errorf("failed to parse timeout: %w", err)
		}
		cfg.Timeout = d
	}
	if n.Retry == nil {
		return cfg, nil
	}
	r := n.Retry
	rc := workflow.DefaultRetryConfig()
	if r.MaxAttempts != 0 {
		rc.MaxAttempts = r.MaxAttempts
	}
	for _, f := range []struct {
		in  string
		out *time.Duration
	}{{r.InitialDelay, &rc.InitialDelay}, {r.MaxDelay, &rc.MaxDelay}} {
		if f.in == "" {
			continue
		}
		d, err := time.ParseDuration(f.in)
		if err != nil {
			return cfg, fmt.Errorf("failed to parse retry delay: %w", err)
		}
		*f.out = d
	}
	if r.BackoffFactor != nil {
		rc.BackoffFactor = *r.BackoffFactor
	}
	if r.Jitter != nil {
		rc.Jitter = *r.Jitter
	}
	cfg.RetryConfig = rc
	return cfg, nil
}
