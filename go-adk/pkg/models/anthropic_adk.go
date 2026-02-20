// Package models: Anthropic model implementing Google ADK model.LLM using genai types.
package models

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"iter"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// Default max tokens for Anthropic (required parameter)
const defaultAnthropicMaxTokens = 8192

// anthropicStopReasonToGenai maps Anthropic stop_reason to genai.FinishReason.
func anthropicStopReasonToGenai(reason anthropic.StopReason) genai.FinishReason {
	switch reason {
	case anthropic.StopReasonMaxTokens:
		return genai.FinishReasonMaxTokens
	case anthropic.StopReasonEndTurn:
		return genai.FinishReasonStop
	case anthropic.StopReasonToolUse:
		return genai.FinishReasonStop
	default:
		return genai.FinishReasonStop
	}
}

// Name implements model.LLM.
func (m *AnthropicModel) Name() string {
	return "anthropic"
}

// GenerateContent implements model.LLM. Uses only ADK/genai types.
func (m *AnthropicModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		messages, systemPrompt := genaiContentsToAnthropicMessages(req.Contents, req.Config)
		// Always prefer config model - req.Model may contain the model type ("anthropic") instead of model name
		modelName := m.Config.Model
		if modelName == "" {
			modelName = req.Model
		}
		if modelName == "" || modelName == "anthropic" {
			modelName = "claude-sonnet-4-20250514"
		}

		// Build request parameters
		params := anthropic.MessageNewParams{
			Model:    anthropic.Model(modelName),
			Messages: messages,
		}

		// Set max tokens (required for Anthropic)
		maxTokens := int64(defaultAnthropicMaxTokens)
		if m.Config.MaxTokens != nil {
			maxTokens = int64(*m.Config.MaxTokens)
		}
		params.MaxTokens = maxTokens

		// Set system prompt if provided
		if systemPrompt != "" {
			params.System = []anthropic.TextBlockParam{
				{Text: systemPrompt},
			}
		}

		// Apply config options
		applyAnthropicConfig(&params, m.Config)

		// Add tools if provided
		if req.Config != nil && len(req.Config.Tools) > 0 {
			params.Tools = genaiToolsToAnthropicTools(req.Config.Tools)
		}

		if stream {
			runAnthropicStreaming(ctx, m, params, yield)
		} else {
			runAnthropicNonStreaming(ctx, m, params, yield)
		}
	}
}

func applyAnthropicConfig(params *anthropic.MessageNewParams, cfg *AnthropicConfig) {
	if cfg == nil {
		return
	}
	if cfg.Temperature != nil {
		params.Temperature = anthropic.Float(*cfg.Temperature)
	}
	if cfg.TopP != nil {
		params.TopP = anthropic.Float(*cfg.TopP)
	}
	if cfg.TopK != nil {
		params.TopK = anthropic.Int(int64(*cfg.TopK))
	}
}

func genaiContentsToAnthropicMessages(contents []*genai.Content, config *genai.GenerateContentConfig) ([]anthropic.MessageParam, string) {
	// Extract system instruction
	var systemBuilder strings.Builder
	if config != nil && config.SystemInstruction != nil {
		for _, p := range config.SystemInstruction.Parts {
			if p != nil && p.Text != "" {
				systemBuilder.WriteString(p.Text)
				systemBuilder.WriteByte('\n')
			}
		}
	}
	systemPrompt := strings.TrimSpace(systemBuilder.String())

	// Collect function responses for matching with function calls
	functionResponses := make(map[string]*genai.FunctionResponse)
	for _, c := range contents {
		if c == nil || c.Parts == nil {
			continue
		}
		for _, p := range c.Parts {
			if p != nil && p.FunctionResponse != nil {
				functionResponses[p.FunctionResponse.ID] = p.FunctionResponse
			}
		}
	}

	var messages []anthropic.MessageParam
	for _, content := range contents {
		if content == nil {
			continue
		}
		role := strings.TrimSpace(content.Role)
		if role == "system" {
			continue // System messages handled separately
		}

		var textParts []string
		var functionCalls []*genai.FunctionCall
		var imageParts []struct {
			mimeType string
			data     []byte
		}

		for _, part := range content.Parts {
			if part == nil {
				continue
			}
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			} else if part.FunctionCall != nil {
				functionCalls = append(functionCalls, part.FunctionCall)
			} else if part.InlineData != nil && strings.HasPrefix(part.InlineData.MIMEType, "image/") {
				imageParts = append(imageParts, struct {
					mimeType string
					data     []byte
				}{part.InlineData.MIMEType, part.InlineData.Data})
			}
		}

		// Handle assistant messages with tool use
		if len(functionCalls) > 0 && (role == "model" || role == "assistant") {
			// Build assistant message with tool use blocks
			var contentBlocks []anthropic.ContentBlockParamUnion
			if len(textParts) > 0 {
				contentBlocks = append(contentBlocks, anthropic.NewTextBlock(strings.Join(textParts, "\n")))
			}
			for _, fc := range functionCalls {
				argsJSON, _ := json.Marshal(fc.Args)
				var inputMap map[string]interface{}
				_ = json.Unmarshal(argsJSON, &inputMap)
				if inputMap == nil {
					inputMap = make(map[string]interface{})
				}
				contentBlocks = append(contentBlocks, anthropic.NewToolUseBlock(fc.ID, inputMap, fc.Name))
			}
			messages = append(messages, anthropic.MessageParam{
				Role:    anthropic.MessageParamRoleAssistant,
				Content: contentBlocks,
			})

			// Add tool results as user message
			var toolResultBlocks []anthropic.ContentBlockParamUnion
			for _, fc := range functionCalls {
				contentStr := "No response available for this function call."
				if fr := functionResponses[fc.ID]; fr != nil {
					contentStr = functionResponseContentString(fr.Response)
				}
				toolResultBlocks = append(toolResultBlocks, anthropic.NewToolResultBlock(fc.ID, contentStr, false))
			}
			messages = append(messages, anthropic.MessageParam{
				Role:    anthropic.MessageParamRoleUser,
				Content: toolResultBlocks,
			})
		} else {
			// Regular user message
			var contentBlocks []anthropic.ContentBlockParamUnion

			// Add images first
			for _, img := range imageParts {
				contentBlocks = append(contentBlocks, anthropic.NewImageBlockBase64(img.mimeType, base64.StdEncoding.EncodeToString(img.data)))
			}

			// Add text
			if len(textParts) > 0 {
				contentBlocks = append(contentBlocks, anthropic.NewTextBlock(strings.Join(textParts, "\n")))
			}

			if len(contentBlocks) > 0 {
				messages = append(messages, anthropic.MessageParam{
					Role:    anthropic.MessageParamRoleUser,
					Content: contentBlocks,
				})
			}
		}
	}

	return messages, systemPrompt
}

func genaiToolsToAnthropicTools(tools []*genai.Tool) []anthropic.ToolUnionParam {
	var out []anthropic.ToolUnionParam
	for _, t := range tools {
		if t == nil || t.FunctionDeclarations == nil {
			continue
		}
		for _, fd := range t.FunctionDeclarations {
			if fd == nil {
				continue
			}
			// Build input schema
			inputSchema := anthropic.ToolInputSchemaParam{
				Properties: make(map[string]interface{}),
			}
			if fd.ParametersJsonSchema != nil {
				if m, ok := fd.ParametersJsonSchema.(map[string]interface{}); ok {
					if props, ok := m["properties"].(map[string]interface{}); ok {
						inputSchema.Properties = props
					}
					if required, ok := m["required"].([]interface{}); ok {
						reqStrings := make([]string, 0, len(required))
						for _, r := range required {
							if s, ok := r.(string); ok {
								reqStrings = append(reqStrings, s)
							}
						}
						inputSchema.Required = reqStrings
					}
				}
			}

			tool := anthropic.ToolParam{
				Name:        fd.Name,
				Description: anthropic.String(fd.Description),
				InputSchema: inputSchema,
			}
			out = append(out, anthropic.ToolUnionParam{OfTool: &tool})
		}
	}
	return out
}

func runAnthropicStreaming(ctx context.Context, m *AnthropicModel, params anthropic.MessageNewParams, yield func(*model.LLMResponse, error) bool) {
	stream := m.Client.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	var aggregatedText string
	toolUseBlocks := make(map[int]struct {
		id        string
		name      string
		inputJSON string
	})
	var stopReason anthropic.StopReason

	for stream.Next() {
		event := stream.Current()

		switch e := event.AsAny().(type) {
		case anthropic.ContentBlockStartEvent:
			idx := int(e.Index)
			if e.ContentBlock.Type == "tool_use" {
				if toolUse, ok := e.ContentBlock.AsAny().(anthropic.ToolUseBlock); ok {
					toolUseBlocks[idx] = struct {
						id        string
						name      string
						inputJSON string
					}{id: toolUse.ID, name: toolUse.Name, inputJSON: ""}
				}
			}
		case anthropic.ContentBlockDeltaEvent:
			idx := int(e.Index)
			delta := e.Delta
			switch delta.Type {
			case "text_delta":
				if textDelta, ok := delta.AsAny().(anthropic.TextDelta); ok {
					aggregatedText += textDelta.Text
					if !yield(&model.LLMResponse{
						Partial:      true,
						TurnComplete: false,
						Content:      &genai.Content{Role: string(genai.RoleModel), Parts: []*genai.Part{{Text: textDelta.Text}}},
					}, nil) {
						return
					}
				}
			case "input_json_delta":
				if jsonDelta, ok := delta.AsAny().(anthropic.InputJSONDelta); ok {
					if block, exists := toolUseBlocks[idx]; exists {
						block.inputJSON += jsonDelta.PartialJSON
						toolUseBlocks[idx] = block
					}
				}
			}
		case anthropic.MessageDeltaEvent:
			stopReason = e.Delta.StopReason
		}
	}

	if err := stream.Err(); err != nil {
		if ctx.Err() == context.Canceled {
			return
		}
		_ = yield(&model.LLMResponse{ErrorCode: "STREAM_ERROR", ErrorMessage: err.Error()}, nil)
		return
	}

	// Build final response
	finalParts := make([]*genai.Part, 0, 1+len(toolUseBlocks))
	if aggregatedText != "" {
		finalParts = append(finalParts, &genai.Part{Text: aggregatedText})
	}
	for _, block := range toolUseBlocks {
		var args map[string]interface{}
		if block.inputJSON != "" {
			_ = json.Unmarshal([]byte(block.inputJSON), &args)
		}
		if block.name != "" || block.id != "" {
			p := genai.NewPartFromFunctionCall(block.name, args)
			p.FunctionCall.ID = block.id
			finalParts = append(finalParts, p)
		}
	}

	_ = yield(&model.LLMResponse{
		Partial:      false,
		TurnComplete: true,
		FinishReason: anthropicStopReasonToGenai(stopReason),
		Content:      &genai.Content{Role: string(genai.RoleModel), Parts: finalParts},
	}, nil)
}

func runAnthropicNonStreaming(ctx context.Context, m *AnthropicModel, params anthropic.MessageNewParams, yield func(*model.LLMResponse, error) bool) {
	message, err := m.Client.Messages.New(ctx, params)
	if err != nil {
		yield(nil, fmt.Errorf("anthropic API error: %w", err))
		return
	}

	// Build parts from response content
	parts := make([]*genai.Part, 0, len(message.Content))
	for _, block := range message.Content {
		switch block.Type {
		case "text":
			if textBlock, ok := block.AsAny().(anthropic.TextBlock); ok {
				parts = append(parts, &genai.Part{Text: textBlock.Text})
			}
		case "tool_use":
			if toolUse, ok := block.AsAny().(anthropic.ToolUseBlock); ok {
				// Convert input to map[string]interface{}
				var args map[string]interface{}
				inputBytes, _ := json.Marshal(toolUse.Input)
				_ = json.Unmarshal(inputBytes, &args)
				p := genai.NewPartFromFunctionCall(toolUse.Name, args)
				p.FunctionCall.ID = toolUse.ID
				parts = append(parts, p)
			}
		}
	}

	// Build usage metadata
	var usage *genai.GenerateContentResponseUsageMetadata
	if message.Usage.InputTokens > 0 || message.Usage.OutputTokens > 0 {
		usage = &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(message.Usage.InputTokens),
			CandidatesTokenCount: int32(message.Usage.OutputTokens),
		}
	}

	yield(&model.LLMResponse{
		Partial:       false,
		TurnComplete:  true,
		FinishReason:  anthropicStopReasonToGenai(message.StopReason),
		UsageMetadata: usage,
		Content:       &genai.Content{Role: string(genai.RoleModel), Parts: parts},
	}, nil)
}
