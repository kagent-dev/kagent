package tools

import (
	"encoding/json"
	"fmt"
	"strings"

	"google.golang.org/adk/model"
	"google.golang.org/adk/tool"
	"google.golang.org/genai"
)

// packTool registers a tool's FunctionDeclaration into the LLM request so the
// model knows the tool exists. This replicates the internal
// toolutils.PackTool logic from the upstream Google ADK which is not
// accessible to external packages.
func packTool(req *model.LLMRequest, name string, decl *genai.FunctionDeclaration, self any) error {
	if req.Tools == nil {
		req.Tools = make(map[string]any)
	}
	if _, ok := req.Tools[name]; ok {
		return fmt.Errorf("duplicate tool: %q", name)
	}
	req.Tools[name] = self

	if decl == nil {
		return nil
	}

	if req.Config == nil {
		req.Config = &genai.GenerateContentConfig{}
	}
	// Find an existing genai.Tool with FunctionDeclarations or create one.
	var funcTool *genai.Tool
	for _, gt := range req.Config.Tools {
		if gt != nil && gt.FunctionDeclarations != nil {
			funcTool = gt
			break
		}
	}
	if funcTool == nil {
		req.Config.Tools = append(req.Config.Tools, &genai.Tool{
			FunctionDeclarations: []*genai.FunctionDeclaration{decl},
		})
	} else {
		funcTool.FunctionDeclarations = append(funcTool.FunctionDeclarations, decl)
	}
	return nil
}

// AskUserTool lets the agent ask the user one or more questions and wait
// for answers before continuing.
type AskUserTool struct{}

func (t *AskUserTool) Name() string { return "ask_user" }

func (t *AskUserTool) Description() string {
	return "Ask the user one or more questions and wait for their answers " +
		"before continuing. Use this when you need clarifying information, " +
		"preferences, or explicit confirmation from the user."
}

func (t *AskUserTool) IsLongRunning() bool { return false }

func (t *AskUserTool) Declaration() *genai.FunctionDeclaration {
	questionSchema := &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"question": {
				Type:        genai.TypeString,
				Description: "The question text to display to the user.",
			},
			"choices": {
				Type: genai.TypeArray,
				Items: &genai.Schema{
					Type: genai.TypeString,
				},
				Description: "Predefined answer choices shown as selectable chips. Leave empty for a free-text-only question.",
			},
			"multiple": {
				Type:        genai.TypeBoolean,
				Description: "If true, the user can select multiple choices. Defaults to false (single-select).",
			},
		},
		Required: []string{"question"},
	}

	return &genai.FunctionDeclaration{
		Name:        t.Name(),
		Description: t.Description(),
		Parameters: &genai.Schema{
			Type: genai.TypeObject,
			Properties: map[string]*genai.Schema{
				"questions": {
					Type:        genai.TypeArray,
					Items:       questionSchema,
					Description: "List of questions to ask the user.",
				},
			},
			Required: []string{"questions"},
		},
	}
}

// Run executes the ask_user tool.
//
// First invocation (no ToolConfirmation): calls RequestConfirmation to pause
// and returns a pending status. The UI will display the questions.
//
// Resume invocation (ToolConfirmation.Confirmed == true): extracts answers
// from the confirmation payload and returns them as a Q&A list.
//
// Cancelled invocation (ToolConfirmation.Confirmed == false): returns a
// cancelled status.
//
// Port of ask_user_tool.py:AskUserTool.run_async().
func (t *AskUserTool) Run(ctx tool.Context, args any) (map[string]any, error) {
	argsMap, ok := args.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("ask_user: unexpected args type %T", args)
	}

	questionsRaw, _ := argsMap["questions"].([]any)
	questions := make([]map[string]any, 0, len(questionsRaw))
	for _, q := range questionsRaw {
		if qMap, ok := q.(map[string]any); ok {
			questions = append(questions, qMap)
		}
	}

	if ctx.ToolConfirmation() == nil {
		// First invocation — pause execution and ask the user.
		var sb strings.Builder
		for i, q := range questions {
			if i > 0 {
				sb.WriteString("; ")
			}
			if text, _ := q["question"].(string); text != "" {
				sb.WriteString(text)
			}
		}
		hint := sb.String()
		if hint == "" {
			hint = "Questions for the user."
		}

		if err := ctx.RequestConfirmation(hint, nil); err != nil {
			return nil, fmt.Errorf("ask_user: failed to request confirmation: %w", err)
		}
		return map[string]any{"status": "pending", "questions": questions}, nil
	}

	if ctx.ToolConfirmation().Confirmed {
		// Second invocation — executor injected answers via payload.
		payload, _ := ctx.ToolConfirmation().Payload.(map[string]any)
		var answers []any
		if payload != nil {
			answers, _ = payload["answers"].([]any)
		}

		result := make([]map[string]any, 0, len(questions))
		for i, q := range questions {
			questionText, _ := q["question"].(string)
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
				"question": questionText,
				"answer":   answer,
			})
		}

		resultJSON, err := json.Marshal(result)
		if err != nil {
			return nil, fmt.Errorf("ask_user: failed to marshal result: %w", err)
		}
		return map[string]any{"result": string(resultJSON)}, nil
	}

	// User cancelled or rejected.
	cancelledJSON, _ := json.Marshal(map[string]any{"status": "cancelled"})
	return map[string]any{"result": string(cancelledJSON)}, nil
}

// ProcessRequest packs the tool's FunctionDeclaration into the LLM request
// so the model knows it can call ask_user.
func (t *AskUserTool) ProcessRequest(_ tool.Context, req *model.LLMRequest) error {
	return packTool(req, t.Name(), t.Declaration(), t)
}
