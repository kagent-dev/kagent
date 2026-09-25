// Package models: Anthropic model implementing Google ADK model.LLM using genai types.
package models

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"iter"
	"slices"
	"strings"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	"google.golang.org/adk/v2/model"
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
	return m.Config.Model
}

// GenerateContent implements model.LLM. Uses only ADK/genai types.
func (m *AnthropicModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		params := buildAnthropicParams(req, m.Config)
		telemetry.SetLLMRequestAttributes(ctx, string(params.Model), req)

		if stream {
			runAnthropicStreaming(ctx, m, params, yield)
		} else {
			runAnthropicNonStreaming(ctx, m, params, yield)
		}
	}
}

// buildAnthropicParams translates an ADK request into a Messages API request:
// model, system prompt, conversation, sampling options and tools, plus the
// prompt-cache breakpoints when cfg enables them.
func buildAnthropicParams(req *model.LLMRequest, cfg *AnthropicConfig) anthropic.MessageNewParams {
	if cfg == nil {
		cfg = &AnthropicConfig{}
	}
	messages, systemPrompt := genaiContentsToAnthropicMessages(req.Contents, req.Config)

	// Always prefer config model - req.Model may contain the model type ("anthropic") instead of model name
	modelName := cfg.Model
	if modelName == "" {
		modelName = req.Model
	}
	if modelName == "" || modelName == "anthropic" {
		modelName = "claude-sonnet-4-20250514"
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(modelName),
		Messages:  messages,
		MaxTokens: int64(defaultAnthropicMaxTokens), // required by the Messages API
	}
	if cfg.MaxTokens != nil {
		params.MaxTokens = int64(*cfg.MaxTokens)
	}
	if systemPrompt != "" {
		params.System = []anthropic.TextBlockParam{{Text: systemPrompt}}
	}
	applyAnthropicConfig(&params, cfg)
	if req.Config != nil && len(req.Config.Tools) > 0 {
		params.Tools = genaiToolsToAnthropicTools(req.Config.Tools)
	}
	if cfg.PromptCaching {
		markAnthropicCacheBreakpoints(&params, anthropicCacheControl(cfg.CacheTTL))
	}
	return params
}

func applyAnthropicConfig(params *anthropic.MessageNewParams, cfg *AnthropicConfig) {
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

// anthropicCacheControl builds the cache_control marker for the configured
// retention window. "" or "5m" leaves the TTL unset, which is the API's default
// 5-minute cache; "1h" opts into the 1-hour cache, which is billed at a higher
// cache-write rate (see v1alpha2.AnthropicConfig.CacheTTL for the trade-off).
func anthropicCacheControl(cacheTTL string) anthropic.CacheControlEphemeralParam {
	control := anthropic.NewCacheControlEphemeralParam()
	if cacheTTL == string(anthropic.CacheControlEphemeralTTLTTL1h) {
		control.TTL = anthropic.CacheControlEphemeralTTLTTL1h
	}
	return control
}

// markAnthropicCacheBreakpoints places the prompt-cache breakpoints on a request.
//
// The Messages API renders tools, then system, then messages, and caches the
// prefix up to each breakpoint, so marking the last tool definition, the last
// system block and the last block of the latest turn keeps the stable head of
// an agent loop cached while the conversation grows: each call reads the
// previous prefix from the cache and writes only the new turn. That uses three
// of the four breakpoints Anthropic allows per request.
//
// The conversation breakpoint walks back from the end because a turn can end
// in a block Anthropic refuses to cache (a thinking block, for which the SDK
// reports no cache_control slot).
func markAnthropicCacheBreakpoints(params *anthropic.MessageNewParams, control anthropic.CacheControlEphemeralParam) {
	if n := len(params.Tools); n > 0 {
		if slot := params.Tools[n-1].GetCacheControl(); slot != nil {
			*slot = control
		}
	}
	if n := len(params.System); n > 0 {
		params.System[n-1].CacheControl = control
	}
	for _, message := range slices.Backward(params.Messages) {
		for _, block := range slices.Backward(message.Content) {
			if slot := block.GetCacheControl(); slot != nil {
				*slot = control
				return
			}
		}
	}
}

// anthropicUsageToGenai folds Anthropic usage into the GenAI shape.
//
// Anthropic reports tokens served from the prompt cache and tokens written to
// it in their own fields, disjoint from input_tokens, whereas GenAI expects one
// prompt count with the cached portion as a breakdown of it. Folding them in
// keeps PromptTokenCount equal to the prompt the model actually saw whether or
// not caching is on, and CachedContentTokenCount says how much of it was a
// cache read.
func anthropicUsageToGenai(usage anthropic.Usage) *genai.GenerateContentResponseUsageMetadata {
	prompt := usage.InputTokens + usage.CacheReadInputTokens + usage.CacheCreationInputTokens
	if prompt == 0 && usage.OutputTokens == 0 {
		return nil
	}
	return &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:        int32(prompt),
		CachedContentTokenCount: int32(usage.CacheReadInputTokens),
		CandidatesTokenCount:    int32(usage.OutputTokens),
	}
}

func genaiContentsToAnthropicMessages(contents []*genai.Content, config *genai.GenerateContentConfig) ([]anthropic.MessageParam, string) {
	systemPrompt := mergeSystemInstructionFromConfig("", config)

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
		var mediaBlocks []anthropic.ContentBlockParamUnion

		for _, part := range content.Parts {
			if part == nil {
				continue
			}
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			} else if part.FunctionCall != nil {
				functionCalls = append(functionCalls, part.FunctionCall)
			} else if part.InlineData != nil {
				mime := part.InlineData.MIMEType
				name := blobName(part.InlineData)
				if isImageMIME(mime) {
					mediaBlocks = append(mediaBlocks, anthropic.NewImageBlockBase64(mime, base64.StdEncoding.EncodeToString(part.InlineData.Data)))
				} else if isAnthropicPDF(mime, name) {
					mediaBlocks = append(mediaBlocks, anthropic.NewDocumentBlock(anthropic.Base64PDFSourceParam{
						Data: base64.StdEncoding.EncodeToString(part.InlineData.Data),
					}))
				} else if isAnthropicPlainText(mime, name) {
					mediaBlocks = append(mediaBlocks, anthropic.NewDocumentBlock(anthropic.PlainTextSourceParam{
						Data: string(part.InlineData.Data),
					}))
				} else {
					textParts = append(textParts, unsupportedFileNote(name, mime))
				}
			} else if part.FileData != nil {
				mime := part.FileData.MIMEType
				name := fileDataName(part.FileData)
				uri := part.FileData.FileURI
				if uri != "" && isAnthropicPDF(mime, name) {
					mediaBlocks = append(mediaBlocks, anthropic.NewDocumentBlock(anthropic.URLPDFSourceParam{URL: uri}))
				} else {
					textParts = append(textParts, unsupportedFileNote(name, mime))
				}
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
				var inputMap map[string]any
				_ = json.Unmarshal(argsJSON, &inputMap)
				if inputMap == nil {
					inputMap = make(map[string]any)
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
					contentStr = extractFunctionResponseContent(fr.Response)
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
			contentBlocks = append(contentBlocks, mediaBlocks...)
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
				Properties: make(map[string]any),
			}
			if fd.ParametersJsonSchema != nil {
				if m := parametersJsonSchemaToMap(fd.ParametersJsonSchema); m != nil {
					if props, ok := m["properties"].(map[string]any); ok {
						inputSchema.Properties = props
					}
					if required, ok := m["required"].([]any); ok {
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
	stream := m.Client.Messages.NewStreaming(ctx, params, anthropicPassthroughOpts(ctx, m.Config)...)
	defer stream.Close()

	var aggregatedText strings.Builder
	toolUseBlocks := make(map[int]struct {
		id        string
		name      string
		inputJSON string
	})
	var stopReason anthropic.StopReason
	// message_start carries the input-side counts (including the prompt-cache
	// breakdown); message_delta carries the final output count.
	var usage anthropic.Usage

	for stream.Next() {
		event := stream.Current()

		switch e := event.AsAny().(type) {
		case anthropic.MessageStartEvent:
			usage = e.Message.Usage
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
					aggregatedText.WriteString(textDelta.Text)
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
			usage.OutputTokens = e.Usage.OutputTokens
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
	aggregatedTextValue := aggregatedText.String()
	if aggregatedTextValue != "" {
		finalParts = append(finalParts, &genai.Part{Text: aggregatedTextValue})
	}
	for _, block := range toolUseBlocks {
		var args map[string]any
		if block.inputJSON != "" {
			_ = json.Unmarshal([]byte(block.inputJSON), &args)
		}
		if block.name != "" || block.id != "" {
			p := genai.NewPartFromFunctionCall(block.name, args)
			p.FunctionCall.ID = block.id
			finalParts = append(finalParts, p)
		}
	}

	resp := &model.LLMResponse{
		Partial:       false,
		TurnComplete:  true,
		FinishReason:  anthropicStopReasonToGenai(stopReason),
		UsageMetadata: anthropicUsageToGenai(usage),
		Content:       &genai.Content{Role: string(genai.RoleModel), Parts: finalParts},
	}
	telemetry.SetLLMResponseAttributes(ctx, resp)
	_ = yield(resp, nil)
}

func runAnthropicNonStreaming(ctx context.Context, m *AnthropicModel, params anthropic.MessageNewParams, yield func(*model.LLMResponse, error) bool) {
	message, err := m.Client.Messages.New(ctx, params, anthropicPassthroughOpts(ctx, m.Config)...)
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
				var args map[string]any
				inputBytes, _ := json.Marshal(toolUse.Input)
				_ = json.Unmarshal(inputBytes, &args)
				p := genai.NewPartFromFunctionCall(toolUse.Name, args)
				p.FunctionCall.ID = toolUse.ID
				parts = append(parts, p)
			}
		}
	}

	resp := &model.LLMResponse{
		Partial:       false,
		TurnComplete:  true,
		FinishReason:  anthropicStopReasonToGenai(message.StopReason),
		UsageMetadata: anthropicUsageToGenai(message.Usage),
		Content:       &genai.Content{Role: string(genai.RoleModel), Parts: parts},
	}
	telemetry.SetLLMResponseAttributes(ctx, resp)
	yield(resp, nil)
}
