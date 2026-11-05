package agent

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	adkmodel "google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/adk/v2/tool/functiontool"
	"google.golang.org/adk/v2/tool/toolconfirmation"
	"google.golang.org/genai"
)

// toolErrorContext is the slice of agent.Context the on-tool-error callback
// reads: the fields of its log line, and the turn's confirmation.
type toolErrorContext struct {
	adkagent.StrictContextMock
	confirmation *toolconfirmation.ToolConfirmation
}

func (toolErrorContext) FunctionCallID() string { return "call-1" }
func (toolErrorContext) SessionID() string      { return "session-1" }
func (toolErrorContext) InvocationID() string   { return "invocation-1" }

func (c *toolErrorContext) ToolConfirmation() *toolconfirmation.ToolConfirmation {
	return c.confirmation
}

type namedTool struct{ name string }

func (t namedTool) Name() string      { return t.name }
func (namedTool) Description() string { return "" }
func (namedTool) IsLongRunning() bool { return false }

func newToolErrorContext(confirmation *toolconfirmation.ToolConfirmation) *toolErrorContext {
	return &toolErrorContext{
		StrictContextMock: adkagent.NewStrictContextMock(context.Background()),
		confirmation:      confirmation,
	}
}

func rejectedWith(payload map[string]any) *toolconfirmation.ToolConfirmation {
	return &toolconfirmation.ToolConfirmation{Confirmed: false, Payload: payload}
}

func TestOnToolErrorCallback_FlagsAFailedCall(t *testing.T) {
	callback := makeOnToolErrorCallback(slog.New(slog.DiscardHandler))
	// The error adk-go's MCP toolset returns for a CallToolResult with IsError set.
	message := "Tool execution failed. Details: bad_data: invalid parameter \"query\": parse error"
	err := errors.New(message)

	response, cbErr := callback(newToolErrorContext(nil), namedTool{name: "call_tool"}, nil, err)

	require.NoError(t, cbErr)
	require.Equal(t, map[string]any{"error": message, "isError": true}, response)
}

func TestOnToolErrorCallback_TellsTheModelWhyAPersonRejectedACall(t *testing.T) {
	callback := makeOnToolErrorCallback(slog.New(slog.DiscardHandler))
	ctx := newToolErrorContext(rejectedWith(map[string]any{"rejection_reason": "What does this tool do?"}))
	err := fmt.Errorf("error tool %q %w", "call_tool", tool.ErrConfirmationRejected)

	response, cbErr := callback(ctx, namedTool{name: "call_tool"}, nil, err)

	require.NoError(t, cbErr)
	require.Equal(t, map[string]any{
		"error":   "Tool call was rejected by user. Reason: What does this tool do?",
		"isError": true,
	}, response)
}

func TestOnToolErrorCallback_LeavesAConfirmationRequestToTheFlow(t *testing.T) {
	callback := makeOnToolErrorCallback(slog.New(slog.DiscardHandler))
	err := fmt.Errorf("error tool %q %w", "call_tool", tool.ErrConfirmationRequired)

	response, cbErr := callback(newToolErrorContext(nil), namedTool{name: "call_tool"}, nil, err)

	require.NoError(t, cbErr)
	require.Nil(t, response, "a confirmation request is a pause, not a failure: adk-go builds the plain response")
}

func TestToolErrorResponse(t *testing.T) {
	require.Nil(t, toolErrorResponse(nil, nil))

	err := errors.New("connection refused")
	require.Equal(t, map[string]any{"error": "connection refused", "isError": true}, toolErrorResponse(nil, err))
}

func TestRejectionMessage(t *testing.T) {
	for _, tc := range []struct {
		name         string
		confirmation *toolconfirmation.ToolConfirmation
		want         string
	}{{
		name:         "the reason the person gave",
		confirmation: rejectedWith(map[string]any{"rejection_reason": "not production"}),
		want:         "Tool call was rejected by user. Reason: not production",
	}, {
		name:         "no reason given",
		confirmation: rejectedWith(nil),
		want:         "Tool call was rejected by user.",
	}, {
		name:         "a blank reason is no reason",
		confirmation: rejectedWith(map[string]any{"rejection_reason": "   "}),
		want:         "Tool call was rejected by user.",
	}, {
		// A rejection propagated from a child agent carries that agent's own
		// decisions, not a reason of its own.
		name:         "a nested confirmation",
		confirmation: rejectedWith(map[string]any{"subagent_name": "child", "task_id": "child-task"}),
		want:         "Tool call was rejected by user.",
	}, {
		name:         "no confirmation at all",
		confirmation: nil,
		want:         "Tool call was rejected by user.",
	}} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, rejectionMessage(tc.confirmation))
		})
	}
}

// The error adk-go's MCP toolset returns for a CallToolResult with IsError set.
const mcpToolError = "Tool execution failed. Details: bad_data: invalid parameter \"query\": parse error"

type queryArgs struct {
	Query string `json:"query"`
}

// toolCallingModel asks for one call of the tool on its first request and
// answers with text on every later one, so a turn ends once the function
// response is in the history.
type toolCallingModel struct {
	mu       sync.Mutex
	requests int
	tool     string
}

func (m *toolCallingModel) Name() string { return "tool-calling" }

func (m *toolCallingModel) GenerateContent(_ context.Context, _ *adkmodel.LLMRequest, _ bool) iter.Seq2[*adkmodel.LLMResponse, error] {
	m.mu.Lock()
	m.requests++
	first := m.requests == 1
	m.mu.Unlock()
	return func(yield func(*adkmodel.LLMResponse, error) bool) {
		if first {
			yield(&adkmodel.LLMResponse{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{{
				FunctionCall: &genai.FunctionCall{ID: "call-1", Name: m.tool, Args: map[string]any{"query": "up"}},
			}}}}, nil)
			return
		}
		yield(&adkmodel.LLMResponse{Content: genai.NewContentFromText("done", genai.RoleModel), FinishReason: genai.FinishReasonStop}, nil)
	}
}

// toolRunner runs an agent with the tool callbacks createGoogleADKAgent wires,
// through adk-go's flow, so the tests see the function responses it emits.
type toolRunner struct {
	runner    *runner.Runner
	sessionID string
}

func newToolRunner(t *testing.T, ctx context.Context, tools ...tool.Tool) *toolRunner {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	a, err := llmagent.New(llmagent.Config{
		Name:                 "tools",
		Model:                &toolCallingModel{tool: tools[0].Name()},
		Tools:                tools,
		BeforeToolCallbacks:  []llmagent.BeforeToolCallback{makeBeforeToolCallback(log)},
		AfterToolCallbacks:   []llmagent.AfterToolCallback{makeAfterToolCallback(log)},
		OnToolErrorCallbacks: []llmagent.OnToolErrorCallback{makeOnToolErrorCallback(log)},
	})
	require.NoError(t, err)
	sessions := adksession.InMemoryService()
	r, err := runner.New(runner.Config{AppName: "test", Agent: a, SessionService: sessions})
	require.NoError(t, err)
	sess, err := sessions.Create(ctx, &adksession.CreateRequest{AppName: "test", UserID: "user"})
	require.NoError(t, err)
	return &toolRunner{runner: r, sessionID: sess.Session.ID()}
}

// turn runs one turn and returns the function responses the flow emitted, in order.
func (r *toolRunner) turn(t *testing.T, ctx context.Context, content *genai.Content) []*genai.FunctionResponse {
	t.Helper()
	var responses []*genai.FunctionResponse
	for event, err := range r.runner.Run(ctx, "user", r.sessionID, content, adkagent.RunConfig{}) {
		require.NoError(t, err)
		if event.Content == nil {
			continue
		}
		for _, part := range event.Content.Parts {
			if part.FunctionResponse != nil {
				responses = append(responses, part.FunctionResponse)
			}
		}
	}
	return responses
}

// A tool error reaches the model through adk-go's flow as the flagged response,
// not only through a direct call of the callback.
func TestFailedToolCallIsFlaggedInTheFunctionResponse(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	failing, err := functiontool.New(functiontool.Config{Name: "query_prometheus"}, func(adkagent.Context, queryArgs) (map[string]any, error) {
		return nil, errors.New(mcpToolError)
	})
	require.NoError(t, err)

	responses := newToolRunner(t, ctx, failing).turn(t, ctx, genai.NewContentFromText("is prometheus up?", genai.RoleUser))

	require.Len(t, responses, 1)
	require.Equal(t, "query_prometheus", responses[0].Name)
	require.Equal(t, map[string]any{"error": mcpToolError, "isError": true}, responses[0].Response)
}
