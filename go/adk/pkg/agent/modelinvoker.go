package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/temporal"
	"github.com/kagent-dev/kagent/go/api/adk"
	adkmodel "google.golang.org/adk/model"
	"google.golang.org/genai"
)

// NewModelInvoker returns a temporal.ModelInvoker that creates an LLM from
// the serialized AgentConfig, converts the conversation history to genai
// format, and invokes the model.
//
// toolDecls are the MCP tool declarations discovered at startup. They are
// passed to the LLM so it knows which tools are available and can generate
// FunctionCall responses. If nil, the LLM will not produce tool calls.
func NewModelInvoker(logger logr.Logger, toolDecls []*genai.FunctionDeclaration) temporal.ModelInvoker {
	return func(ctx context.Context, configBytes []byte, historyBytes []byte, onToken func(string)) (*temporal.LLMResponse, error) {
		log := logger.WithName("model-invoker")

		// 1. Parse agent config.
		var agentConfig adk.AgentConfig
		if err := json.Unmarshal(configBytes, &agentConfig); err != nil {
			return nil, fmt.Errorf("failed to parse agent config: %w", err)
		}

		if agentConfig.Model == nil {
			return nil, fmt.Errorf("agent config has no model configuration")
		}

		// 2. Create LLM from config.
		llm, err := createLLM(ctx, agentConfig.Model, log)
		if err != nil {
			return nil, fmt.Errorf("failed to create LLM: %w", err)
		}

		// 3. Parse conversation history.
		var history []conversationEntry
		if err := json.Unmarshal(historyBytes, &history); err != nil {
			return nil, fmt.Errorf("failed to parse conversation history: %w", err)
		}

		// 4. Convert history to genai.Content format.
		contents := historyToContents(history)

		// 5. Build LLM request with system instruction and tool declarations.
		genConfig := &genai.GenerateContentConfig{}
		if agentConfig.Instruction != "" {
			genConfig.SystemInstruction = &genai.Content{
				Role: "user",
				Parts: []*genai.Part{
					genai.NewPartFromText(agentConfig.Instruction),
				},
			}
		}

		// Include tool declarations so the LLM can generate FunctionCall responses.
		if len(toolDecls) > 0 {
			genConfig.Tools = []*genai.Tool{{
				FunctionDeclarations: toolDecls,
			}}
		}

		req := &adkmodel.LLMRequest{
			Contents: contents,
			Config:   genConfig,
		}

		// 6. Invoke LLM (non-streaming; collect full response).
		stream := onToken != nil
		var finalResp *adkmodel.LLMResponse
		for resp, err := range llm.GenerateContent(ctx, req, stream) {
			if err != nil {
				return nil, fmt.Errorf("LLM generation failed: %w", err)
			}
			if resp.Partial && onToken != nil {
				// Stream partial text tokens.
				if resp.Content != nil {
					for _, part := range resp.Content.Parts {
						if part.Text != "" {
							onToken(part.Text)
						}
					}
				}
				continue
			}
			finalResp = resp
		}

		if finalResp == nil {
			return nil, fmt.Errorf("LLM returned no response")
		}

		// 7. Convert LLM response to temporal.LLMResponse.
		return convertResponse(finalResp)
	}
}

// conversationEntry mirrors the workflow's conversation history format.
type conversationEntry struct {
	Role       string              `json:"role"`
	Content    string              `json:"content,omitempty"`
	ToolCalls  []temporal.ToolCall `json:"toolCalls,omitempty"`
	ToolCallID string              `json:"toolCallID,omitempty"`
	ToolResult json.RawMessage     `json:"toolResult,omitempty"`
}

// historyToContents converts conversation entries to genai.Content slices.
func historyToContents(history []conversationEntry) []*genai.Content {
	var contents []*genai.Content

	for _, entry := range history {
		switch entry.Role {
		case "user":
			contents = append(contents, &genai.Content{
				Role: "user",
				Parts: []*genai.Part{
					genai.NewPartFromText(entry.Content),
				},
			})

		case "assistant":
			c := &genai.Content{Role: "model"}
			if entry.Content != "" {
				c.Parts = append(c.Parts, genai.NewPartFromText(entry.Content))
			}
			for _, tc := range entry.ToolCalls {
				var args map[string]any
				if len(tc.Args) > 0 {
					_ = json.Unmarshal(tc.Args, &args)
				}
				c.Parts = append(c.Parts, genai.NewPartFromFunctionCall(tc.Name, args))
			}
			if len(c.Parts) > 0 {
				contents = append(contents, c)
			}

		case "tool":
			var result map[string]any
			if len(entry.ToolResult) > 0 {
				_ = json.Unmarshal(entry.ToolResult, &result)
			}
			if result == nil {
				result = map[string]any{"result": string(entry.ToolResult)}
			}
			contents = append(contents, &genai.Content{
				Role: "user",
				Parts: []*genai.Part{
					genai.NewPartFromFunctionResponse(entry.ToolCallID, result),
				},
			})
		}
	}

	return contents
}

// convertResponse converts a Google ADK LLM response to a temporal.LLMResponse.
func convertResponse(resp *adkmodel.LLMResponse) (*temporal.LLMResponse, error) {
	result := &temporal.LLMResponse{}

	if resp.Content == nil {
		result.Terminal = true
		return result, nil
	}

	for _, part := range resp.Content.Parts {
		if part.Text != "" {
			result.Content += part.Text
		}
		if part.FunctionCall != nil {
			argsBytes, _ := json.Marshal(part.FunctionCall.Args)
			result.ToolCalls = append(result.ToolCalls, temporal.ToolCall{
				ID:   part.FunctionCall.ID,
				Name: part.FunctionCall.Name,
				Args: argsBytes,
			})
		}
	}

	// Terminal if no tool calls and no agent calls.
	if len(result.ToolCalls) == 0 && len(result.AgentCalls) == 0 {
		result.Terminal = true
	}

	return result, nil
}
