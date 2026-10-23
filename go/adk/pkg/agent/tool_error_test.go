package agent

import (
	"context"
	"errors"
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

// turn runs one turn and returns the function responses and function calls
// the flow emitted, in order.
func (r *toolRunner) turn(t *testing.T, ctx context.Context, content *genai.Content) (responses []*genai.FunctionResponse, calls []*genai.FunctionCall) {
	t.Helper()
	for event, err := range r.runner.Run(ctx, "user", r.sessionID, content, adkagent.RunConfig{}) {
		require.NoError(t, err)
		if event.Content == nil {
			continue
		}
		for _, part := range event.Content.Parts {
			if part.FunctionResponse != nil {
				responses = append(responses, part.FunctionResponse)
			}
			if part.FunctionCall != nil {
				calls = append(calls, part.FunctionCall)
			}
		}
	}
	return responses, calls
}

func TestFailedToolCallIsFlaggedInTheFunctionResponse(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	failing, err := functiontool.New(functiontool.Config{Name: "query_prometheus"}, func(adkagent.Context, queryArgs) (map[string]any, error) {
		return nil, errors.New(mcpToolError)
	})
	require.NoError(t, err)

	responses, _ := newToolRunner(t, ctx, failing).turn(t, ctx, genai.NewContentFromText("is prometheus up?", genai.RoleUser))

	require.Len(t, responses, 1)
	require.Equal(t, "query_prometheus", responses[0].Name)
	require.Equal(t, map[string]any{"error": mcpToolError, "isError": true}, responses[0].Response)
}

func TestConfirmationRequestIsAPauseAndRejectionAFailure(t *testing.T) {
	t.Parallel()
	ctx := t.Context()
	ran := false
	guarded, err := functiontool.New(functiontool.Config{Name: "delete_pod", RequireConfirmation: true}, func(adkagent.Context, queryArgs) (map[string]any, error) {
		ran = true
		return map[string]any{"result": "deleted"}, nil
	})
	require.NoError(t, err)
	r := newToolRunner(t, ctx, guarded)

	responses, calls := r.turn(t, ctx, genai.NewContentFromText("delete the pod", genai.RoleUser))

	require.Len(t, responses, 1, "the pending call answers with adk-go's plain response")
	require.Contains(t, responses[0].Response, "error")
	require.NotContains(t, responses[0].Response, "isError", "a confirmation request is a pause, not a failure")
	require.Len(t, calls, 2, "the model's call, then the confirmation request")
	require.Equal(t, toolconfirmation.FunctionCallName, calls[1].Name)

	// The user rejects the call: the tool does not run, and the call is flagged.
	rejection := &genai.Content{Role: genai.RoleUser, Parts: []*genai.Part{{FunctionResponse: &genai.FunctionResponse{
		ID: calls[1].ID, Name: toolconfirmation.FunctionCallName, Response: map[string]any{"confirmed": false},
	}}}}
	responses, _ = r.turn(t, ctx, rejection)

	require.False(t, ran, "a rejected call must not run")
	require.Len(t, responses, 1)
	require.Equal(t, "delete_pod", responses[0].Name)
	require.Equal(t, map[string]any{"error": "error tool \"delete_pod\" call is rejected", "isError": true}, responses[0].Response)
}
