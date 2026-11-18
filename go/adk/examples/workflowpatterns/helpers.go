package main

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"slices"
	"strconv"
	"strings"
	"sync/atomic"

	"google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/adk/v2/workflow"
	"google.golang.org/genai"

	"github.com/kagent-dev/kagent/go/adk/pkg/declarative"
)

// Text behavior shared by the Go and YAML versions of every pattern.

func clean(s string) string   { return strings.TrimSpace(s) }
func upper(s string) string   { return strings.ToUpper(s) }
func lower(s string) string   { return strings.ToLower(s) }
func exclaim(s string) string { return s + "!" }
func reverse(s string) string { r := []rune(s); slices.Reverse(r); return string(r) }
func truncate(s string) string {
	if len(s) > 10 {
		return s[:10] + "..."
	}
	return s
}
func critic(s string) string  { return "critic(" + s + ")" }
func plan(s string) string    { return "plan(" + s + ")" }
func execute(s string) string { return "exec(" + s + ")" }

// tag builds a handler that prefixes its input.
func tag(prefix string) func(string) string { return func(s string) string { return prefix + s } }

var (
	bugHandler     = tag("bug handled: ")
	billingHandler = tag("billing handled: ")
	fallbackReply  = tag("fallback: ")
	auditor        = tag("audited: ")
	notifier       = tag("notified: ")
	publisher      = tag("published after reply: ")
	rejecter       = tag("rejected after reply: ")
	finalReport    = tag("final: ")
	draftText      = tag("draft: ")
)

// Routing decisions: each returns the routes a router node emits.

func classify(s string) []string {
	switch {
	case strings.HasPrefix(s, "bug"):
		return []string{"BUG"}
	case strings.HasPrefix(s, "billing"):
		return []string{"BILLING"}
	}
	return []string{"OTHER"}
}

func isLong(s string) []string { return []string{strconv.FormatBool(len(s) > 10)} }

// audience always audits and also notifies on urgent messages.
func audience(s string) []string {
	if strings.HasPrefix(s, "urgent") {
		return []string{"audit", "notify"}
	}
	return []string{"audit"}
}

func longEnough(s string) []string {
	if len(s) >= 5 {
		return []string{"done"}
	}
	return []string{"again"}
}

func verdict(s string) []string {
	if s == "yes" {
		return []string{"APPROVED"}
	}
	return []string{"REJECTED"}
}

// Structured data between nodes.

func words(s string) []any {
	var out []any
	for w := range strings.FieldsSeq(s) {
		out = append(out, w)
	}
	return out
}

func joinWords(items []any) string {
	parts := make([]string, len(items))
	for i, item := range items {
		parts[i] = fmt.Sprint(item)
	}
	return strings.Join(parts, " ")
}

// combine renders the join map with sorted keys, so it is deterministic.
func combine(branches map[string]any) string {
	keys := make([]string, 0, len(branches))
	for k := range branches {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = k + "=" + fmt.Sprint(branches[k])
	}
	return strings.Join(parts, "|")
}

func makePerson(s string) map[string]any { return map[string]any{"name": s} }

func greetPerson(person map[string]any) string { return fmt.Sprintf("hello %v", person["name"]) }

func customerArgs(s string) map[string]any { return map[string]any{"customer_id": s} }

func describeCustomer(c map[string]any) string {
	return fmt.Sprintf("customer %v is on plan %v", c["customer"], c["plan"])
}

type lookupArgs struct {
	CustomerID string `json:"customer_id"`
}

func newLookupTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{Name: "lookup_customer", Description: "Looks up a customer."},
		func(_ agent.Context, a lookupArgs) (map[string]any, error) {
			return map[string]any{"customer": a.CustomerID, "plan": "pro"}, nil
		})
}

var errTransient = errors.New("transient failure")

// newFlaky returns a function that fails its first n calls, then succeeds.
func newFlaky(n int32) func(string) (string, error) {
	var calls atomic.Int32
	return func(s string) (string, error) {
		if calls.Add(1) <= n {
			return "", errTransient
		}
		return "recovered: " + s, nil
	}
}

// Go-side node constructors.

// step wraps a text function as a graph node.
func step(name string, f func(string) string) workflow.Node {
	return workflow.NewFunctionNode(name, func(_ agent.Context, in string) (string, error) {
		return f(in), nil
	}, workflow.NodeConfig{})
}

// router wraps a routing decision as a node. It forwards its input unchanged
// and emits the routes that decide which outgoing edges run.
func router(name string, decide func(string) []string) workflow.Node {
	return workflow.NewEmittingFunctionNode(name, func(ctx agent.Context, in string, emit func(*session.Event) error) (any, error) {
		ev := session.NewEvent(ctx, ctx.InvocationID())
		ev.Routes = decide(in)
		ev.Output = in
		return nil, emit(ev)
	}, workflow.NodeConfig{})
}

// worker returns a stub-model LlmAgent for the sequential, parallel and loop patterns.
func worker(name, instruction string) (agent.Agent, error) {
	return llmagent.New(llmagent.Config{
		Name: name, Instruction: instruction, Model: declarative.NewStubModel("stub"),
	})
}

// historyModel makes session isolation visible: it replies "saw=N", where N is
// how many text parts of conversation it was shown. An agent that shares the
// conversation sees more than one that is isolated from it.
type historyModel struct{}

func (historyModel) Name() string { return "history" }

func (historyModel) GenerateContent(_ context.Context, req *adkmodel.LLMRequest, _ bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		seen := 0
		for _, c := range req.Contents {
			for _, part := range c.Parts {
				if part != nil && part.Text != "" {
					seen++
				}
			}
		}
		yield(&adkmodel.LLMResponse{
			Content:      genai.NewContentFromText(fmt.Sprintf("saw=%d", seen), genai.RoleModel),
			TurnComplete: true,
		}, nil)
	}
}

// callerModel is a parent model that calls one tool with the user's message,
// then answers with the tool's result.
type callerModel struct{ tool string }

func (callerModel) Name() string { return "caller" }

func (m callerModel) GenerateContent(_ context.Context, req *adkmodel.LLMRequest, _ bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		var request, result string
		for _, c := range req.Contents {
			for _, part := range c.Parts {
				switch {
				case part == nil:
				case part.FunctionResponse != nil:
					result = fmt.Sprint(part.FunctionResponse.Response["result"])
				case part.Text != "" && request == "":
					request = part.Text
				}
			}
		}
		if result != "" {
			yield(&adkmodel.LLMResponse{Content: genai.NewContentFromText("tool said: "+result, genai.RoleModel), TurnComplete: true}, nil)
			return
		}
		yield(&adkmodel.LLMResponse{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{
			FunctionCall: &genai.FunctionCall{ID: "call-1", Name: m.tool, Args: map[string]any{"request": request}},
		}}}, TurnComplete: true}, nil)
	}
}

// chatAgent returns a chat-mode agent on the history model, so its answer
// shows how much of the conversation it shares.
func chatAgent(name string) (agent.Agent, error) {
	return llmagent.New(llmagent.Config{
		Name: name, Instruction: "Answer.", Model: historyModel{}, Mode: llmagent.ModeChat,
	})
}
