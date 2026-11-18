package declarative

import (
	"fmt"
	"slices"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"
)

// HookPoint names an ADK callback point that hooks can guard.
type HookPoint string

const (
	HookBeforeModel HookPoint = "before_model"
	HookAfterModel  HookPoint = "after_model"
	HookBeforeTool  HookPoint = "before_tool"
	HookAfterTool   HookPoint = "after_tool"
)

// Verdict is the outcome of one hook rule.
type Verdict int

const (
	// Continue defers to the next rule. A chain where every rule continues
	// lets the call run.
	Continue Verdict = iota
	// Accept lets the call run and skips the remaining rules.
	Accept
	// Reject blocks the call with Decision.Message.
	Reject
)

// Decision is what a Hook returns.
type Decision struct {
	Verdict Verdict
	// Message explains a rejection to the model or the user.
	Message string
}

// HookEvent is the call a Hook decides on. Only the fields of its Point are
// set.
type HookEvent struct {
	Point HookPoint
	// Request is the model request (before_model).
	Request *adkmodel.LLMRequest
	// Response and ModelError are the model result (after_model).
	Response   *adkmodel.LLMResponse
	ModelError error
	// Tool and Args describe a tool call (before_tool, after_tool).
	Tool tool.Tool
	Args map[string]any
	// Result and ToolError are the tool result (after_tool).
	Result    map[string]any
	ToolError error
}

// Hook decides in Go whether a model or tool call may proceed. Hooks run on
// the agent's hot path; keep them fast and free of side effects that a retry
// would repeat.
type Hook func(ctx agent.Context, ev *HookEvent) (Decision, error)

// hookMetadataKey marks responses and tool results produced by a rejection,
// so clients and tests can tell them from real model or tool output.
const hookMetadataKey = "kagent_hook"

// hookRule is one compiled HookSpec.
type hookRule struct {
	when    *expression
	action  HookAction
	message string
	fn      Hook
}

// hookChain evaluates rules in document order.
type hookChain []hookRule

// decide returns the first accept or reject. vars binds the CEL variables of
// the point and is called at most once.
func (c hookChain) decide(ctx agent.Context, ev *HookEvent, vars func() map[string]any) (Decision, error) {
	var bound map[string]any
	for _, r := range c {
		if r.when != nil {
			if bound == nil {
				bound = vars()
			}
			ok, err := r.when.evalBool(bound)
			if err != nil {
				return Decision{}, fmt.Errorf("%s hook: %w", ev.Point, err)
			}
			if !ok {
				continue
			}
		}
		if r.fn != nil {
			d, err := r.fn(ctx, ev)
			if err != nil {
				return Decision{}, fmt.Errorf("%s hook: %w", ev.Point, err)
			}
			if d.Verdict == Continue {
				continue
			}
			return d, nil
		}
		if r.action == HookReject {
			return Decision{Verdict: Reject, Message: r.message}, nil
		}
		return Decision{Verdict: Accept}, nil
	}
	return Decision{Verdict: Continue}, nil
}

// compileHooks compiles the rules of one point. Validate already checked
// them, so errors here mean the registry changed after validation.
func (r *Registry) compileHooks(scope celScope, specs []HookSpec) (hookChain, error) {
	chain := make(hookChain, 0, len(specs))
	for i, s := range specs {
		rule := hookRule{action: s.Action, message: s.Message}
		if s.When != "" {
			when, err := compileCEL(scope, s.When, resultBool)
			if err != nil {
				return nil, fmt.Errorf("%s[%d].when: %w", scope, i, err)
			}
			rule.when = when
		}
		if s.Function != "" {
			fn, ok := r.hooks[s.Function]
			if !ok {
				return nil, fmt.Errorf("%s[%d]: hook %q is not registered", scope, i, s.Function)
			}
			rule.fn = fn
		}
		chain = append(chain, rule)
	}
	return chain, nil
}

// applyCallbacks installs one ADK callback per declared point.
func (r *Registry) applyCallbacks(cfg *llmagent.Config, cb *CallbacksSpec) error {
	if cb == nil {
		return nil
	}
	if len(cb.BeforeModel) > 0 {
		chain, err := r.compileHooks(scopeBeforeModel, cb.BeforeModel)
		if err != nil {
			return err
		}
		cfg.BeforeModelCallbacks = append(cfg.BeforeModelCallbacks, beforeModel(chain))
	}
	if len(cb.AfterModel) > 0 {
		chain, err := r.compileHooks(scopeAfterModel, cb.AfterModel)
		if err != nil {
			return err
		}
		cfg.AfterModelCallbacks = append(cfg.AfterModelCallbacks, afterModel(chain))
	}
	if len(cb.BeforeTool) > 0 {
		chain, err := r.compileHooks(scopeBeforeTool, cb.BeforeTool)
		if err != nil {
			return err
		}
		cfg.BeforeToolCallbacks = append(cfg.BeforeToolCallbacks, beforeTool(chain))
	}
	if len(cb.AfterTool) > 0 {
		chain, err := r.compileHooks(scopeAfterTool, cb.AfterTool)
		if err != nil {
			return err
		}
		cfg.AfterToolCallbacks = append(cfg.AfterToolCallbacks, afterTool(chain))
	}
	return nil
}

// beforeModel answers a rejected request with the rejection message instead
// of calling the model.
func beforeModel(chain hookChain) llmagent.BeforeModelCallback {
	return func(ctx agent.Context, req *adkmodel.LLMRequest) (*adkmodel.LLMResponse, error) {
		ev := &HookEvent{Point: HookBeforeModel, Request: req}
		d, err := chain.decide(ctx, ev, func() map[string]any {
			vars := contextVars(ctx)
			vars[celRequest] = requestVars(req)
			return vars
		})
		if err != nil || d.Verdict != Reject {
			return nil, err
		}
		return rejectedResponse(HookBeforeModel, d.Message), nil
	}
}

// afterModel replaces a rejected response with the rejection message.
func afterModel(chain hookChain) llmagent.AfterModelCallback {
	return func(ctx agent.Context, resp *adkmodel.LLMResponse, respErr error) (*adkmodel.LLMResponse, error) {
		ev := &HookEvent{Point: HookAfterModel, Response: resp, ModelError: respErr}
		d, err := chain.decide(ctx, ev, func() map[string]any {
			vars := contextVars(ctx)
			vars[celResponse] = responseVars(resp, respErr)
			return vars
		})
		if err != nil || d.Verdict != Reject {
			return nil, err
		}
		return rejectedResponse(HookAfterModel, d.Message), nil
	}
}

// beforeTool skips a rejected tool call and returns the rejection to the
// model as the tool result, so the model can recover.
func beforeTool(chain hookChain) llmagent.BeforeToolCallback {
	return func(ctx agent.Context, t tool.Tool, args map[string]any) (map[string]any, error) {
		ev := &HookEvent{Point: HookBeforeTool, Tool: t, Args: args}
		d, err := chain.decide(ctx, ev, func() map[string]any {
			vars := contextVars(ctx)
			vars[celTool] = toolVars(t, args, nil, nil)
			return vars
		})
		if err != nil || d.Verdict != Reject {
			return nil, err
		}
		return rejectedToolResult(HookBeforeTool, d.Message), nil
	}
}

// afterTool replaces a rejected tool result before the model sees it.
func afterTool(chain hookChain) llmagent.AfterToolCallback {
	return func(ctx agent.Context, t tool.Tool, args, result map[string]any, toolErr error) (map[string]any, error) {
		ev := &HookEvent{Point: HookAfterTool, Tool: t, Args: args, Result: result, ToolError: toolErr}
		d, err := chain.decide(ctx, ev, func() map[string]any {
			vars := contextVars(ctx)
			vars[celTool] = toolVars(t, args, result, toolErr)
			return vars
		})
		if err != nil || d.Verdict != Reject {
			return nil, err
		}
		return rejectedToolResult(HookAfterTool, d.Message), nil
	}
}

func rejectedResponse(point HookPoint, message string) *adkmodel.LLMResponse {
	return &adkmodel.LLMResponse{
		Content:        genai.NewContentFromText(message, genai.RoleModel),
		TurnComplete:   true,
		CustomMetadata: map[string]any{hookMetadataKey: map[string]any{"point": string(point), "verdict": "reject"}},
	}
}

func rejectedToolResult(point HookPoint, message string) map[string]any {
	return map[string]any{"error": message, hookMetadataKey: map[string]any{"point": string(point), "verdict": "reject"}}
}

func requestVars(req *adkmodel.LLMRequest) map[string]any {
	out := map[string]any{"model": "", "text": "", "contents": []any{}, "system_instruction": "", "tools": []any{}}
	if req == nil {
		return out
	}
	out["model"] = req.Model
	out["text"] = lastUserText(req)
	contents := make([]any, 0, len(req.Contents))
	for _, c := range req.Contents {
		if c != nil {
			contents = append(contents, map[string]any{"role": c.Role, "text": contentText(c)})
		}
	}
	out["contents"] = contents
	if req.Config != nil {
		out["system_instruction"] = contentText(req.Config.SystemInstruction)
	}
	names := make([]string, 0, len(req.Tools))
	for name := range req.Tools {
		names = append(names, name)
	}
	slices.Sort(names)
	tools := make([]any, len(names))
	for i, n := range names {
		tools[i] = n
	}
	out["tools"] = tools
	return out
}

func responseVars(resp *adkmodel.LLMResponse, respErr error) map[string]any {
	out := map[string]any{"text": "", "partial": false, "turn_complete": false, "error": "", "finish_reason": "", "function_calls": []any{}}
	if respErr != nil {
		out["error"] = respErr.Error()
	}
	if resp == nil {
		return out
	}
	out["text"] = contentText(resp.Content)
	out["partial"] = resp.Partial
	out["turn_complete"] = resp.TurnComplete
	out["finish_reason"] = string(resp.FinishReason)
	if resp.ErrorMessage != "" && respErr == nil {
		out["error"] = resp.ErrorMessage
	}
	calls := []any{}
	if resp.Content != nil {
		for _, p := range resp.Content.Parts {
			if p != nil && p.FunctionCall != nil {
				calls = append(calls, p.FunctionCall.Name)
			}
		}
	}
	out["function_calls"] = calls
	return out
}

func toolVars(t tool.Tool, args, result map[string]any, toolErr error) map[string]any {
	out := map[string]any{"name": "", "args": plainValue(nonNilMap(args)), "result": plainValue(nonNilMap(result)), "error": ""}
	if t != nil {
		out["name"] = t.Name()
	}
	if toolErr != nil {
		out["error"] = toolErr.Error()
	}
	return out
}

func nonNilMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
