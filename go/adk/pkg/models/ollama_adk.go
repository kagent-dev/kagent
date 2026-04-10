package models

import (
	"context"
	"fmt"
	"iter"
	"strings"

	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	"github.com/ollama/ollama/api"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// GenerateContent implements model.LLM for Ollama models using the native SDK.
// It converts genai.Content to Ollama message format and handles tool conversion.
func (m *OllamaModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		// Get model name
		modelName := m.Config.Model
		if req.Model != "" {
			modelName = req.Model
		}

		// Convert options
		var options map[string]any
		if m.Config.Options != nil {
			options = convertOllamaOptions(m.Config.Options)
		}

		// Convert content to Ollama messages
		messages, systemInstruction := convertGenaiContentsToOllamaMessages(req.Contents)

		// Add system instruction as first message if present
		if systemInstruction != "" {
			systemMsg := api.Message{
				Role:    "system",
				Content: systemInstruction,
			}
			messages = append([]api.Message{systemMsg}, messages...)
		}

		// Convert tools
		var tools []api.Tool
		if req.Config != nil && len(req.Config.Tools) > 0 {
			tools = convertGenaiToolsToOllama(req.Config.Tools)
		}

		// Set telemetry attributes
		telemetry.SetLLMRequestAttributes(ctx, modelName, req)

		if stream {
			m.generateStreaming(ctx, modelName, messages, tools, options, yield)
		} else {
			m.generateNonStreaming(ctx, modelName, messages, tools, options, yield)
		}
	}
}

// generateStreaming handles streaming responses from Ollama.
func (m *OllamaModel) generateStreaming(ctx context.Context, modelName string, messages []api.Message, tools []api.Tool, options map[string]any, yield func(*model.LLMResponse, error) bool) {
	var aggregatedText strings.Builder

	streamValue := true
	chatReq := &api.ChatRequest{
		Model:    modelName,
		Messages: messages,
		Tools:    tools,
		Options:  options,
		Stream:   &streamValue,
	}

	err := m.Client.Chat(ctx, chatReq, func(resp api.ChatResponse) error {
		// Handle content
		if resp.Message.Content != "" {
			aggregatedText.WriteString(resp.Message.Content)

			response := &model.LLMResponse{
				Content: &genai.Content{
					Role: "model",
					Parts: []*genai.Part{
						{Text: resp.Message.Content},
					},
				},
				Partial:      true,
				TurnComplete: false,
			}
			if !yield(response, nil) {
				return fmt.Errorf("streaming cancelled")
			}
		}

		// Handle completion
		if resp.Done {
			// Build final response with complete message
			finalParts := []*genai.Part{}

			text := aggregatedText.String()
			if text != "" {
				finalParts = append(finalParts, &genai.Part{Text: text})
			}

			// Convert tool calls from final message
			for _, tc := range resp.Message.ToolCalls {
				if tc.Function.Name != "" {
					functionCall := &genai.FunctionCall{
						Name: tc.Function.Name,
						Args: tc.Function.Arguments.ToMap(),
						ID:   uuid.New().String(),
					}
					finalParts = append(finalParts, &genai.Part{FunctionCall: functionCall})
				}
			}

			// Build finish reason
			var finishReason genai.FinishReason
			if resp.DoneReason == "length" {
				finishReason = genai.FinishReasonMaxTokens
			} else {
				finishReason = genai.FinishReasonStop
			}

			// Build usage metadata
			var usageMetadata *genai.GenerateContentResponseUsageMetadata
			if resp.PromptEvalCount > 0 || resp.EvalCount > 0 {
				usageMetadata = &genai.GenerateContentResponseUsageMetadata{
					PromptTokenCount:     int32(resp.PromptEvalCount),
					CandidatesTokenCount: int32(resp.EvalCount),
					TotalTokenCount:      int32(resp.PromptEvalCount + resp.EvalCount),
				}
			}

			response := &model.LLMResponse{
				Content: &genai.Content{
					Role:  "model",
					Parts: finalParts,
				},
				Partial:       false,
				TurnComplete:  true,
				FinishReason:  finishReason,
				UsageMetadata: usageMetadata,
			}
			yield(response, nil)
		}

		return nil
	})

	if err != nil {
		yield(&model.LLMResponse{
			ErrorCode:    "API_ERROR",
			ErrorMessage: err.Error(),
		}, nil)
	}
}

// generateNonStreaming handles non-streaming responses from Ollama.
func (m *OllamaModel) generateNonStreaming(ctx context.Context, modelName string, messages []api.Message, tools []api.Tool, options map[string]any, yield func(*model.LLMResponse, error) bool) {
	streamValue := false
	chatReq := &api.ChatRequest{
		Model:    modelName,
		Messages: messages,
		Tools:    tools,
		Options:  options,
		Stream:   &streamValue,
	}

	var finalResponse api.ChatResponse
	err := m.Client.Chat(ctx, chatReq, func(resp api.ChatResponse) error {
		finalResponse = resp
		return nil
	})

	if err != nil {
		yield(&model.LLMResponse{
			ErrorCode:    "API_ERROR",
			ErrorMessage: err.Error(),
		}, nil)
		return
	}

	// Build parts from response
	parts := []*genai.Part{}

	if finalResponse.Message.Content != "" {
		parts = append(parts, &genai.Part{Text: finalResponse.Message.Content})
	}

	// Convert tool calls
	for _, tc := range finalResponse.Message.ToolCalls {
		if tc.Function.Name != "" {
			functionCall := &genai.FunctionCall{
				Name: tc.Function.Name,
				Args: tc.Function.Arguments.ToMap(),
				ID:   uuid.New().String(),
			}
			parts = append(parts, &genai.Part{FunctionCall: functionCall})
		}
	}

	// Build finish reason
	var finishReason genai.FinishReason
	if finalResponse.DoneReason == "length" {
		finishReason = genai.FinishReasonMaxTokens
	} else {
		finishReason = genai.FinishReasonStop
	}

	// Build usage metadata
	var usageMetadata *genai.GenerateContentResponseUsageMetadata
	if finalResponse.PromptEvalCount > 0 || finalResponse.EvalCount > 0 {
		usageMetadata = &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(finalResponse.PromptEvalCount),
			CandidatesTokenCount: int32(finalResponse.EvalCount),
			TotalTokenCount:      int32(finalResponse.PromptEvalCount + finalResponse.EvalCount),
		}
	}

	response := &model.LLMResponse{
		Content: &genai.Content{
			Role:  "model",
			Parts: parts,
		},
		Partial:       false,
		TurnComplete:  true,
		FinishReason:  finishReason,
		UsageMetadata: usageMetadata,
	}
	telemetry.SetLLMResponseAttributes(ctx, response)
	yield(response, nil)
}

// convertGenaiContentsToOllamaMessages converts genai.Content to Ollama message format.
// Returns messages and system instruction (extracted from system role content).
func convertGenaiContentsToOllamaMessages(contents []*genai.Content) ([]api.Message, string) {
	var messages []api.Message
	var systemInstruction string

	for _, content := range contents {
		if content == nil || len(content.Parts) == 0 {
			continue
		}

		// Determine role
		role := "user"
		if content.Role == "model" || content.Role == "assistant" {
			role = "assistant"
		}

		var textParts []string
		var toolCalls []api.ToolCall
		var toolResults []struct {
			content string
		}

		for _, part := range content.Parts {
			if part == nil {
				continue
			}

			// Handle text
			if part.Text != "" {
				textParts = append(textParts, part.Text)
				continue
			}

			// Handle function call (tool call)
			if part.FunctionCall != nil {
				toolCall := api.ToolCall{
					Function: api.ToolCallFunction{
						Name:      part.FunctionCall.Name,
						Arguments: api.NewToolCallFunctionArguments(),
					},
				}
				// Copy arguments
				for k, v := range part.FunctionCall.Args {
					toolCall.Function.Arguments.Set(k, v)
				}
				toolCalls = append(toolCalls, toolCall)
				continue
			}

			// Handle function response (tool result)
			if part.FunctionResponse != nil {
				// Extract response content
				content := extractFunctionResponseContent(part.FunctionResponse.Response)
				toolResults = append(toolResults, struct {
					content string
				}{content: content})
				continue
			}
		}

		// Build message based on what we found
		if len(toolCalls) > 0 {
			// Tool call message
			msg := api.Message{
				Role:      "assistant",
				ToolCalls: toolCalls,
			}
			messages = append(messages, msg)
		}

		if len(toolResults) > 0 {
			// Tool result messages
			for _, tr := range toolResults {
				msg := api.Message{
					Role:    "tool",
					Content: tr.content,
				}
				messages = append(messages, msg)
			}
		}

		if len(textParts) > 0 {
			// Regular text message
			// Check if this is a system message
			if content.Role == "system" {
				systemInstruction = strings.Join(textParts, "\n")
			} else {
				msg := api.Message{
					Role:    role,
					Content: strings.Join(textParts, "\n"),
				}
				messages = append(messages, msg)
			}
		}
	}

	return messages, systemInstruction
}

// extractFunctionResponseContent extracts text content from a function response.
func extractFunctionResponseContent(response any) string {
	if response == nil {
		return ""
	}

	switch v := response.(type) {
	case string:
		return v
	case map[string]any:
		// Try to extract text from common formats
		if content, ok := v["content"].([]any); ok {
			var parts []string
			for _, item := range content {
				if itemMap, ok := item.(map[string]any); ok {
					if text, ok := itemMap["text"].(string); ok {
						parts = append(parts, text)
					}
				}
			}
			return strings.Join(parts, "\n")
		}
		if result, ok := v["result"].(string); ok {
			return result
		}
		// Fallback: serialize the whole map
		return fmt.Sprintf("%v", v)
	default:
		return fmt.Sprintf("%v", v)
	}
}

// convertGenaiToolsToOllama converts genai.Tool to Ollama tool format.
func convertGenaiToolsToOllama(tools []*genai.Tool) []api.Tool {
	if len(tools) == 0 {
		return nil
	}

	var ollamaTools []api.Tool

	for _, tool := range tools {
		if tool == nil || tool.FunctionDeclarations == nil {
			continue
		}

		for _, decl := range tool.FunctionDeclarations {
			if decl == nil {
				continue
			}

			// Build parameters
			params := api.ToolFunctionParameters{
				Type:       "object",
				Properties: api.NewToolPropertiesMap(),
			}
			if decl.Parameters != nil {
				for name, schema := range decl.Parameters.Properties {
					if schema == nil {
						continue
					}
					prop := api.ToolProperty{
						Type:        api.PropertyType{string(schema.Type)},
						Description: schema.Description,
					}
					if len(schema.Enum) > 0 {
						// Convert []string to []any
						enumVals := make([]any, len(schema.Enum))
						for i, v := range schema.Enum {
							enumVals[i] = v
						}
						prop.Enum = enumVals
					}
					params.Properties.Set(name, prop)
				}
				if len(decl.Parameters.Required) > 0 {
					params.Required = decl.Parameters.Required
				}
			}

			ollamaTool := api.Tool{
				Type: "function",
				Function: api.ToolFunction{
					Name:        decl.Name,
					Description: decl.Description,
					Parameters:  params,
				},
			}
			ollamaTools = append(ollamaTools, ollamaTool)
		}
	}

	return ollamaTools
}
