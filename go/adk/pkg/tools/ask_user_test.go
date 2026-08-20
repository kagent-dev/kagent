package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/tool/toolconfirmation"
)

type askUserConfirmationRequest struct {
	hint    string
	payload any
}

type askUserTestContext struct {
	adkagent.Context
	confirmation *toolconfirmation.ToolConfirmation
	requests     []askUserConfirmationRequest
}

func (c *askUserTestContext) ToolConfirmation() *toolconfirmation.ToolConfirmation {
	return c.confirmation
}

func (c *askUserTestContext) RequestConfirmation(hint string, payload any) error {
	c.requests = append(c.requests, askUserConfirmationRequest{hint: hint, payload: payload})
	return nil
}

func runAskUserTool(
	t *testing.T,
	ctx adkagent.Context,
	args map[string]any,
) (map[string]any, error) {
	t.Helper()

	askUserTool, err := NewAskUserTool()
	require.NoError(t, err)
	runner, ok := askUserTool.(interface {
		Run(adkagent.Context, any) (map[string]any, error)
	})
	require.True(t, ok, "ask_user tool %T does not implement Run", askUserTool)
	return runner.Run(ctx, args)
}

func TestAskUserRejectsInvalidQuestionsWithoutRequestingConfirmation(t *testing.T) {
	tests := []struct {
		name    string
		args    map[string]any
		wantErr string
	}{
		{
			name:    "empty list",
			args:    map[string]any{"questions": []any{}},
			wantErr: "ask_user: at least one question is required",
		},
		{
			name:    "empty first question",
			args:    map[string]any{"questions": []any{map[string]any{"question": ""}}},
			wantErr: "ask_user: question 1 must contain non-whitespace text",
		},
		{
			name:    "spaces-only first question",
			args:    map[string]any{"questions": []any{map[string]any{"question": "   "}}},
			wantErr: "ask_user: question 1 must contain non-whitespace text",
		},
		{
			name:    "tab-newline-only first question",
			args:    map[string]any{"questions": []any{map[string]any{"question": "\t\n"}}},
			wantErr: "ask_user: question 1 must contain non-whitespace text",
		},
		{
			name: "blank second question",
			args: map[string]any{"questions": []any{
				map[string]any{"question": "Which environment?"},
				map[string]any{"question": " \t\n"},
			}},
			wantErr: "ask_user: question 2 must contain non-whitespace text",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := &askUserTestContext{}

			_, err := runAskUserTool(t, ctx, tt.args)

			assert.EqualError(t, err, tt.wantErr)
			assert.Empty(t, ctx.requests)
		})
	}
}

func TestAskUserValidQuestionRequestsConfirmation(t *testing.T) {
	ctx := &askUserTestContext{}
	question := "  Which environment?  "

	result, err := runAskUserTool(t, ctx, map[string]any{
		"questions": []any{map[string]any{
			"question": question,
			"choices":  []any{"prod", "staging"},
			"multiple": true,
		}},
	})

	require.NoError(t, err)
	assert.Equal(t, []askUserConfirmationRequest{{hint: question, payload: nil}}, ctx.requests)
	assert.Equal(t, map[string]any{
		"status": "pending",
		"questions": []any{map[string]any{
			"question": question,
			"choices":  []any{"prod", "staging"},
			"multiple": true,
		}},
	}, result)
}

func TestAskUserValidConfirmedQuestionReturnsAnswer(t *testing.T) {
	ctx := &askUserTestContext{
		confirmation: &toolconfirmation.ToolConfirmation{
			Confirmed: true,
			Payload: map[string]any{
				"answers": []any{map[string]any{"answer": "prod"}},
			},
		},
	}

	result, err := runAskUserTool(t, ctx, map[string]any{
		"questions": []any{map[string]any{"question": "  Which environment?  "}},
	})

	require.NoError(t, err)
	assert.Empty(t, ctx.requests)
	resultJSON, ok := result["result"].(string)
	require.True(t, ok)
	assert.JSONEq(t, `[{"answer":"prod","question":"  Which environment?  "}]`, resultJSON)
}
