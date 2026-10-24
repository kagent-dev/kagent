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

// node dispatches on the node type. Registered types are consulted after the
// built-ins, which they cannot shadow. render is set for terminal nodes whose
// output must be readable as message text.
func (b *builder) node(bc *BuildContext, sc *scope, n *NodeSpec, render bool) (workflow.Node, error) {
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
		fn, err := b.function(n.Function)
		if err != nil {
			return nil, err
		}
		return workflow.NewFunctionNodeWithSchema[any, any](n.ID, functionBody(fn, render), in, out, cfg)
	case NodeJoin:
		return joinNode(n.ID, in)
	case NodeTool:
		if n.Server != "" {
			return b.mcpToolNode(n, in, out, cfg)
		}
		return b.toolNode(n.ID, n.Tool, cfg)
	case NodeAgent:
		return b.agentNode(n.Agent, in, cfg)
	case NodeLLM:
		return b.llmNode(n, in, cfg)
	case NodeWorkflow:
		return b.workflowNode(n, render)
	case NodeParallelWorker:
		return b.parallelWorker(n, cfg)
	case NodeDynamic:
		return b.dynamicNode(sc, n, cfg, render, in, out)
	case NodeHITL:
		return hitlNode(n, cfg, render), nil
	case NodeCEL:
		return celNode(n, in, out, cfg, render)
	}
	custom, ok := b.reg.nodeTypes[n.Type]
	if !ok {
		return nil, fmt.Errorf("node type %q is not registered", n.Type)
	}
	return custom.build(bc, n)
}

func (b *builder) function(name string) (Func, error) {
	fn, ok := b.reg.funcs[name]
	if !ok {
		return nil, fmt.Errorf("function %q is not registered", name)
	}
	return fn, nil
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

// finish hands the result of an emitting node (dynamic, hitl) to the engine.
// Plain results return normally and the engine emits them; a result that
// must be rendered is emitted here as one event and the node returns nil so
// the engine adds no second one.
func finish(ctx agent.Context, emit func(*session.Event) error, out any, render bool) (any, error) {
	if _, isText := out.(string); !render || out == nil || isText {
		return out, nil
	}
	return nil, emit(resultEvent(ctx, out, nil, true))
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

func (b *builder) toolNode(id, toolName string, cfg workflow.NodeConfig) (workflow.Node, error) {
	t, ok := b.reg.tools[toolName]
	if !ok {
		return nil, fmt.Errorf("tool %q is not registered", toolName)
	}
	return workflow.NewNamedToolNode(id, t, cfg)
}

// agentNode wraps a document agent. ADK names an agent node after its agent,
// so the document id of such a node is not visible to the engine.
func (b *builder) agentNode(agentName string, in *jsonschema.Schema, cfg workflow.NodeConfig) (workflow.Node, error) {
	a, err := b.agent(agentName)
	if err != nil {
		return nil, err
	}
	return workflow.NewAgentNodeWithSchemas(a, in, nil, cfg)
}

// nodeSchemas parses the node's contracts. Validate already resolved them.
// An inline llm node's output_schema is the model's reply schema, handled when
// the agent is built, so it is not a node contract here.
func nodeSchemas(n *NodeSpec) (in, out *jsonschema.Schema, err error) {
	if len(n.InputSchema) > 0 {
		if in, err = parseSchema(n.InputSchema); err != nil {
			return nil, nil, fmt.Errorf("input_schema: %w", err)
		}
	}
	if n.Type != NodeLLM && len(n.OutputSchema) > 0 {
		if out, err = parseSchema(n.OutputSchema); err != nil {
			return nil, nil, fmt.Errorf("output_schema: %w", err)
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
		return nil, fmt.Errorf("input_schema: %w", err)
	}
	return workflow.NewJoinNodeWithSchema(id, resolved), nil
}

// llmNode builds an inline llm node as an anonymous single-turn agent named
// after the node id.
func (b *builder) llmNode(n *NodeSpec, in *jsonschema.Schema, cfg workflow.NodeConfig) (workflow.Node, error) {
	mode := n.Mode
	if mode == "" {
		mode = ModeSingleTurn
	}
	a, err := b.newLLM(n.ID, n.Description, &n.LLMSpec, mode, nil, nil, false, false)
	if err != nil {
		return nil, err
	}
	return workflow.NewAgentNodeWithSchemas(a, in, nil, cfg)
}

func (b *builder) workflowNode(n *NodeSpec, render bool) (workflow.Node, error) {
	nodes, err := b.nodes(n.Nodes, n.Edges, render)
	if err != nil {
		return nil, err
	}
	return workflow.NewWorkflowNode(n.ID, toEdges(nodes, n.Edges, ""))
}

// parallelWorker fans the input list out over one wrapped node. The wrapped
// node carries no retry policy: ADK applies the worker's own policy per item.
func (b *builder) parallelWorker(n *NodeSpec, cfg workflow.NodeConfig) (workflow.Node, error) {
	var wrapped workflow.Node
	var err error
	switch {
	case n.Function != "":
		var fn Func
		if fn, err = b.function(n.Function); err == nil {
			wrapped = workflow.NewFunctionNode(n.ID+"_item", plainFunc(fn), workflow.NodeConfig{})
		}
	case n.Tool != "":
		wrapped, err = b.toolNode(n.ID+"_item", n.Tool, workflow.NodeConfig{})
	default:
		wrapped, err = b.agentNode(n.Agent, nil, workflow.NodeConfig{})
	}
	if err != nil {
		return nil, err
	}
	return workflow.NewParallelWorker(n.ID, wrapped, n.MaxConcurrency, cfg)
}

// plainFunc adapts a Func whose routes are ignored: a worker item has no
// outgoing edges to route over.
func plainFunc(fn Func) func(agent.Context, any) (any, error) {
	return func(ctx agent.Context, in any) (any, error) {
		res, err := fn(ctx, in)
		return res.Output, err
	}
}

func (b *builder) dynamicNode(sc *scope, n *NodeSpec, cfg workflow.NodeConfig, render bool, in, out *jsonschema.Schema) (workflow.Node, error) {
	fn, ok := b.reg.dynamics[n.Function]
	if !ok {
		return nil, fmt.Errorf("dynamic function %q is not registered", n.Function)
	}
	maxIters, childIDs, isolation := n.MaxIters, n.Children, n.Isolation
	return workflow.NewDynamicNodeWithSchema[any, any](n.ID, func(ctx agent.Context, input any, emit func(*session.Event) error) (any, error) {
		children := make(map[string]workflow.Node, len(childIDs))
		for _, id := range childIDs {
			children[id] = sc.nodes[id]
		}
		result, err := fn(ctx, input, &DynamicEnv{MaxIters: maxIters, Emit: emit, ctx: ctx, children: children, isolation: isolation})
		if err != nil {
			return nil, err
		}
		return finish(ctx, emit, result, render)
	}, in, out, cfg)
}

// hitlNode pauses the workflow until a human answers and forwards the answer.
// The node re-runs on resume, so the interrupt id must be stable within a run
// and unique across runs; the invocation id gives both.
func hitlNode(n *NodeSpec, cfg workflow.NodeConfig, render bool) workflow.Node {
	rerun := true
	cfg.RerunOnResume = &rerun
	return workflow.NewEmittingFunctionNode(n.ID, func(ctx agent.Context, _ any, emit func(*session.Event) error) (any, error) {
		reply, err := workflow.ResumeOrRequestInput(ctx, emit, session.RequestInput{
			InterruptID: n.ID + "-" + ctx.InvocationID(),
			Message:     n.Message,
		})
		if err != nil {
			return nil, err
		}
		return finish(ctx, emit, reply, render)
	}, cfg)
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
	if n.Retry != nil {
		rc := workflow.DefaultRetryConfig()
		{
			r := n.Retry
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
		}
		cfg.RetryConfig = rc
	}
	return cfg, nil
}
