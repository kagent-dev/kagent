package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/adk/tool"
	"google.golang.org/adk/tool/functiontool"
)

type askUserQuestion struct {
	Question string   `json:"question"`
	Choices  []string `json:"choices,omitempty"`
	Multiple bool     `json:"multiple,omitempty"`
}

type askUserInput struct {
	Questions []askUserQuestion `json:"questions"`
}

const askUserDescription = "Ask the user one or more questions and wait for their answers " +
	"before continuing. Use this when you need clarifying information, " +
	"preferences, or explicit confirmation from the user."

// NewAskUserTool creates the ask_user tool using functiontool.New.
//
// First invocation (no ToolConfirmation): calls RequestConfirmation to pause
// and returns a pending status. The UI will display the questions.
//
// Resume invocation (ToolConfirmation.Confirmed == true): extracts answers
// from the confirmation payload and returns them as a Q&A list.
//
// Cancelled invocation (ToolConfirmation.Confirmed == false): the ADK
// framework returns a rejection error before reaching the handler.
//
// Port of ask_user_tool.py:AskUserTool.run_async().
func NewAskUserTool() (tool.Tool, error) {
	return functiontool.New(functiontool.Config{
		Name:        "ask_user",
		Description: askUserDescription,
	}, func(ctx tool.Context, in askUserInput) (map[string]any, error) {
		if ctx.ToolConfirmation() == nil {
			// Phase 1 — pause execution and ask the user.
			var sb strings.Builder
			for i, q := range in.Questions {
				if i > 0 {
					sb.WriteString("; ")
				}
				sb.WriteString(q.Question)
			}
			hint := sb.String()
			if hint == "" {
				hint = "Questions for the user."
			}

			// Build questions slice for the pending response.
			questionsSlice := make([]map[string]any, 0, len(in.Questions))
			for _, q := range in.Questions {
				qMap := map[string]any{"question": q.Question}
				if len(q.Choices) > 0 {
					qMap["choices"] = q.Choices
				}
				if q.Multiple {
					qMap["multiple"] = q.Multiple
				}
				questionsSlice = append(questionsSlice, qMap)
			}

			if err := ctx.RequestConfirmation(hint, nil); err != nil {
				return nil, fmt.Errorf("ask_user: failed to request confirmation: %w", err)
			}
			return map[string]any{"status": "pending", "questions": questionsSlice}, nil
		}

		// Phase 2 — executor injected answers via payload.
		payload, _ := ctx.ToolConfirmation().Payload.(map[string]any)
		var answers []any
		if payload != nil {
			answers, _ = payload["answers"].([]any)
		}

		result := make([]map[string]any, 0, len(in.Questions))
		for i, q := range in.Questions {
			var answer any
			if i < len(answers) {
				if answerMap, ok := answers[i].(map[string]any); ok {
					answer = answerMap["answer"]
				} else {
					answer = answers[i]
				}
			}
			if answer == nil {
				answer = []any{}
			}
			result = append(result, map[string]any{
				"question": q.Question,
				"answer":   answer,
			})
		}

		resultJSON, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("ask_user: failed to marshal result: %w", err)
		}
		return map[string]any{"result": string(resultJSON)}, nil
	})
}
