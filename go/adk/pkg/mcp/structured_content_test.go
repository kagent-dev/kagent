package mcp

import (
	"context"
	"encoding/json"
	"iter"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
	"google.golang.org/adk/agent"
	"google.golang.org/adk/agent/llmagent"
	"google.golang.org/adk/model"
	"google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/mcptoolset"
	"google.golang.org/genai"

	kagentsession "github.com/kagent-dev/kagent/go/adk/pkg/session"
)

const emptyArraysToolName = "empty_arrays"

type emptyArraysOutput struct {
	Messages         []string `json:"messages"`
	ExplanationCodes []string `json:"explanation_codes"`
}

type toolCallModel struct {
	calls atomic.Int32
}

func (*toolCallModel) Name() string { return "tool-call-model" }

func (m *toolCallModel) GenerateContent(_ context.Context, _ *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		if m.calls.Add(1) == 1 {
			part := genai.NewPartFromFunctionCall(emptyArraysToolName, map[string]any{})
			part.FunctionCall.ID = "call-1"
			yield(&model.LLMResponse{
				Content:      &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{part}},
				TurnComplete: true,
			}, nil)
			return
		}
		yield(&model.LLMResponse{
			Content:      genai.NewContentFromText("done", genai.RoleModel),
			TurnComplete: true,
		}, nil)
	}
}

func TestMCPStructuredContentPreservesNestedEmptyArraysInFunctionResponseEvent(t *testing.T) {
	clientTransport, serverTransport := mcpsdk.NewInMemoryTransports()
	server := mcpsdk.NewServer(&mcpsdk.Implementation{Name: "empty-arrays", Version: "test"}, nil)
	mcpsdk.AddTool(server, &mcpsdk.Tool{Name: emptyArraysToolName}, func(context.Context, *mcpsdk.CallToolRequest, struct{}) (*mcpsdk.CallToolResult, emptyArraysOutput, error) {
		return nil, emptyArraysOutput{
			Messages:         []string{},
			ExplanationCodes: []string{},
		}, nil
	})
	serverSession, err := server.Connect(t.Context(), serverTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, serverSession.Close()) })

	toolset, err := mcptoolset.New(mcptoolset.Config{Transport: clientTransport})
	require.NoError(t, err)
	adkAgent, err := llmagent.New(llmagent.Config{
		Name:     "structured_content_agent",
		Model:    &toolCallModel{},
		Toolsets: []tool.Toolset{toolset},
	})
	require.NoError(t, err)

	controller := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/sessions":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{"id": "session-1", "user_id": "user-1"},
			}))
		case r.Method == http.MethodGet && r.URL.Path == "/api/sessions/session-1":
			require.NoError(t, json.NewEncoder(w).Encode(map[string]any{
				"data": map[string]any{
					"session": map[string]any{"id": "session-1", "user_id": "user-1"},
					"events":  []any{},
				},
			}))
		case r.Method == http.MethodPost && r.URL.Path == "/api/sessions/session-1/events":
			w.WriteHeader(http.StatusCreated)
		default:
			http.Error(w, "unexpected request", http.StatusNotFound)
		}
	}))
	t.Cleanup(controller.Close)

	sessionService := kagentsession.NewKAgentSessionService(controller.URL, controller.Client())
	_, err = sessionService.Create(t.Context(), &adksession.CreateRequest{
		AppName:   "test-app",
		UserID:    "user-1",
		SessionID: "session-1",
	})
	require.NoError(t, err)

	adkRunner, err := runner.New(runner.Config{
		AppName:        "test-app",
		Agent:          adkAgent,
		SessionService: sessionService,
	})
	require.NoError(t, err)

	var response map[string]any
	for event, runErr := range adkRunner.Run(
		t.Context(),
		"user-1",
		"session-1",
		genai.NewContentFromText("call the tool", genai.RoleUser),
		agent.RunConfig{},
	) {
		require.NoError(t, runErr)
		if event.Content == nil {
			continue
		}
		for _, part := range event.Content.Parts {
			if part.FunctionResponse != nil && part.FunctionResponse.Name == emptyArraysToolName {
				response = part.FunctionResponse.Response
			}
		}
	}

	require.NotNil(t, response)
	output, ok := response["output"].(map[string]any)
	require.True(t, ok, "output type = %T", response["output"])
	messages, ok := output["messages"].([]any)
	require.True(t, ok, "messages type = %T", output["messages"])
	require.NotNil(t, messages)
	require.Empty(t, messages)
	codes, ok := output["explanation_codes"].([]any)
	require.True(t, ok, "explanation_codes type = %T", output["explanation_codes"])
	require.NotNil(t, codes)
	require.Empty(t, codes)
}
