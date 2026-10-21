package tools

import (
	"context"
	"iter"
	"testing"

	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/agent/llmagent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	"google.golang.org/adk/v2/session"
	"google.golang.org/adk/v2/tool"
	"google.golang.org/genai"
)

type askUserRecoveryModel struct {
	args      map[string]any
	calls     int
	toolError string
}

func (*askUserRecoveryModel) Name() string { return "ask-user-recovery" }

func (m *askUserRecoveryModel) GenerateContent(_ context.Context, request *model.LLMRequest, _ bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		m.calls++
		if m.calls == 1 {
			yield(&model.LLMResponse{Content: &genai.Content{Role: "model", Parts: []*genai.Part{{FunctionCall: &genai.FunctionCall{ID: "invalid-ask", Name: "ask_user", Args: m.args}}}}}, nil)
			return
		}
		for _, content := range request.Contents {
			for _, part := range content.Parts {
				if response := part.FunctionResponse; response != nil && response.ID == "invalid-ask" {
					m.toolError, _ = response.Response["error"].(string)
				}
			}
		}
		yield(&model.LLMResponse{Content: genai.NewContentFromText("Recovered", "model")}, nil)
	}
}

func TestAskUserMalformedArgumentsRemainRecoverableInGoRunner(t *testing.T) {
	for name, args := range map[string]map[string]any{
		"missing":           {},
		"string collection": {"questions": "Which environment?"},
		"string item":       {"questions": []any{"Which environment?"}},
		"null item":         {"questions": []any{nil}},
		"empty":             {"questions": []any{}},
		"blank":             {"questions": []any{map[string]any{"question": "  "}}},
	} {
		t.Run(name, func(t *testing.T) {
			m := &askUserRecoveryModel{args: args}
			ask, err := NewAskUserTool()
			require.NoError(t, err)
			a, err := llmagent.New(llmagent.Config{Name: "ask_recovery", Model: m, Tools: []tool.Tool{ask}})
			require.NoError(t, err)
			sessions := session.InMemoryService()
			r, err := runner.New(runner.Config{AppName: "test", Agent: a, SessionService: sessions})
			require.NoError(t, err)
			s, err := sessions.Create(t.Context(), &session.CreateRequest{AppName: "test", UserID: "user"})
			require.NoError(t, err)
			answer := ""
			for event, err := range r.Run(t.Context(), "user", s.Session.ID(), genai.NewContentFromText("Ask me", "user"), adkagent.RunConfig{}) {
				require.NoError(t, err)
				require.Empty(t, event.Actions.RequestedToolConfirmations)
				if event.Content != nil {
					for _, part := range event.Content.Parts {
						answer += part.Text
					}
				}
			}
			require.Equal(t, 2, m.calls)
			require.NotEmpty(t, m.toolError)
			require.Equal(t, "Recovered", answer)
		})
	}
}
