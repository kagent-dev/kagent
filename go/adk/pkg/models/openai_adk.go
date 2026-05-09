// Package models: OpenAI model implementing Google ADK model.LLM using genai types only.
package models

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/packages/respjson"
	"github.com/openai/openai-go/v3/shared"
	"github.com/openai/openai-go/v3/shared/constant"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// OpenAI API role and finish-reason values (for clarity and to avoid typos).
const (
	openAIRoleSystem          = "system"
	openAIRoleAssistant       = "assistant"
	openAIRoleModel           = "model"
	openAIFinishLength        = "length"
	openAIFinishContentFilter = "content_filter"
	openAIToolTypeFunction    = "function"
	openAIExtraContentKey     = "extra_content"
)

// openAIFinishReasonToGenai maps OpenAI finish_reason to genai.FinishReason.
func openAIFinishReasonToGenai(reason string) genai.FinishReason {
	switch reason {
	case openAIFinishLength:
		return genai.FinishReasonMaxTokens
	case openAIFinishContentFilter:
		return genai.FinishReasonSafety
	default:
		return genai.FinishReasonStop // includes "stop", "tool_calls", and empty
	}
}

type openAIThoughtSignatureExtra struct {
	Google struct {
		ThoughtSignature string `json:"thought_signature"`
	} `json:"google"`
}

func extractThoughtSignatureFromRaw(raw string) []byte {
	if raw == "" {
		return nil
	}

	var extra openAIThoughtSignatureExtra
	if err := json.Unmarshal([]byte(raw), &extra); err != nil {
		return nil
	}
	if extra.Google.ThoughtSignature == "" {
		return nil
	}

	decoded, err := base64.StdEncoding.DecodeString(extra.Google.ThoughtSignature)
	if err != nil {
		return nil
	}
	return decoded
}

func extractThoughtSignatureFromExtraFields(extraFields map[string]respjson.Field) []byte {
	if len(extraFields) == 0 {
		return nil
	}
	field, ok := extraFields[openAIExtraContentKey]
	if !ok {
		return nil
	}
	return extractThoughtSignatureFromRaw(field.Raw())
}

func openAIExtraContentForThoughtSignature(thoughtSignature []byte) map[string]any {
	if len(thoughtSignature) == 0 {
		return nil
	}

	return map[string]any{
		"google": map[string]any{
			"thought_signature": base64.StdEncoding.EncodeToString(thoughtSignature),
		},
	}
}

func thoughtSignaturesByToolCallID(contents []*genai.Content) map[string][]byte {
	thoughtSignatures := make(map[string][]byte)
	for _, content := range contents {
		if content == nil || content.Parts == nil {
			continue
		}
		for _, part := range content.Parts {
			if part == nil || part.FunctionCall == nil || len(part.ThoughtSignature) == 0 {
				continue
			}
			thoughtSignatures[part.FunctionCall.ID] = part.ThoughtSignature
		}
	}
	return thoughtSignatures
}

func newFunctionCallPart(name string, args map[string]any, id string, thoughtSignature []byte) *genai.Part {
	part := genai.NewPartFromFunctionCall(name, args)
	if part.FunctionCall != nil {
		part.FunctionCall.ID = id
	}
	if len(thoughtSignature) > 0 {
		part.ThoughtSignature = thoughtSignature
	}
	return part
}

// Name implements model.LLM.
func (m *OpenAIModel) Name() string {
	return m.Config.Model
}

// GenerateContent implements model.LLM. Uses only ADK/genai types.
func (m *OpenAIModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		messages, systemInstruction := genaiContentsToOpenAIMessages(req.Contents, req.Config)
		modelName := req.Model
		if modelName == "" {
			modelName = m.Config.Model
		}
		if m.IsAzure && m.Config.Model != "" {
			modelName = m.Config.Model
		}
		telemetry.SetLLMRequestAttributes(ctx, modelName, req)

		params := openai.ChatCompletionNewParams{
			Model:    shared.ChatModel(modelName),
			Messages: messages,
		}
		if systemInstruction != "" {
			params.Messages = append([]openai.ChatCompletionMessageParamUnion{
				openai.SystemMessage(systemInstruction),
			}, params.Messages...)
		}
		applyOpenAIConfig(&params, m.Config)

		if req.Config != nil && len(req.Config.Tools) > 0 {
			params.Tools = genaiToolsToOpenAITools(req.Config.Tools)
			params.ToolChoice = openai.ChatCompletionToolChoiceOptionUnionParam{
				OfAuto: openai.String("auto"),
			}
		}

		if stream {
			runStreaming(ctx, m, params, yield)
		} else {
			runNonStreaming(ctx, m, params, yield)
		}
	}
}

func applyOpenAIConfig(params *openai.ChatCompletionNewParams, cfg *OpenAIConfig) {
	if cfg == nil {
		return
	}
	if cfg.Temperature != nil {
		params.Temperature = openai.Float(*cfg.Temperature)
	}
	if cfg.MaxTokens != nil {
		params.MaxTokens = openai.Int(int64(*cfg.MaxTokens))
	}
	if cfg.TopP != nil {
		params.TopP = openai.Float(*cfg.TopP)
	}
	if cfg.FrequencyPenalty != nil {
		params.FrequencyPenalty = openai.Float(*cfg.FrequencyPenalty)
	}
	if cfg.PresencePenalty != nil {
		params.PresencePenalty = openai.Float(*cfg.PresencePenalty)
	}
	if cfg.Seed != nil {
		params.Seed = openai.Int(int64(*cfg.Seed))
	}
	if cfg.N != nil {
		params.N = openai.Int(int64(*cfg.N))
	}
}

func genaiContentsToOpenAIMessages(contents []*genai.Content, config *genai.GenerateContentConfig) ([]openai.ChatCompletionMessageParamUnion, string) {
	var systemBuilder strings.Builder
	if config != nil && config.SystemInstruction != nil {
		for _, p := range config.SystemInstruction.Parts {
			if p != nil && p.Text != "" {
				systemBuilder.WriteString(p.Text)
				systemBuilder.WriteByte('\n')
			}
		}
	}
	systemInstruction := strings.TrimSpace(systemBuilder.String())

	functionResponses := make(map[string]*genai.FunctionResponse)
	thoughtSignatures := thoughtSignaturesByToolCallID(contents)
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

	var messages []openai.ChatCompletionMessageParamUnion
	for _, content := range contents {
		if content == nil || strings.TrimSpace(content.Role) == openAIRoleSystem {
			continue
		}
		role := strings.TrimSpace(content.Role)
		var textParts []string
		var functionCalls []*genai.FunctionCall
		var imageParts []openai.ChatCompletionContentPartImageImageURLParam

		for _, part := range content.Parts {
			if part == nil {
				continue
			}
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			} else if part.FunctionCall != nil {
				functionCalls = append(functionCalls, part.FunctionCall)
			} else if part.InlineData != nil && strings.HasPrefix(part.InlineData.MIMEType, "image/") {
				imageParts = append(imageParts, openai.ChatCompletionContentPartImageImageURLParam{
					URL: fmt.Sprintf("data:%s;base64,%s", part.InlineData.MIMEType, base64.StdEncoding.EncodeToString(part.InlineData.Data)),
				})
			}
		}

		if len(functionCalls) > 0 && (role == openAIRoleModel || role == openAIRoleAssistant) {
			toolCalls := make([]openai.ChatCompletionMessageToolCallUnionParam, 0, len(functionCalls))
			var toolResponseMessages []openai.ChatCompletionMessageParamUnion
			for _, fc := range functionCalls {
				argsJSON, _ := json.Marshal(fc.Args)
				toolCall := openai.ChatCompletionMessageFunctionToolCallParam{
					ID:   fc.ID,
					Type: constant.Function(openAIToolTypeFunction),
					Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
						Name:      fc.Name,
						Arguments: string(argsJSON),
					},
				}
				if extraContent := openAIExtraContentForThoughtSignature(thoughtSignatures[fc.ID]); extraContent != nil {
					toolCall.SetExtraFields(map[string]any{openAIExtraContentKey: extraContent})
				}
				toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallUnionParam{
					OfFunction: &toolCall,
				})
				contentStr := "No response available for this function call."
				if fr := functionResponses[fc.ID]; fr != nil {
					contentStr = extractFunctionResponseContent(fr.Response)
				}
				toolMessage := openai.ChatCompletionToolMessageParam{
					Content: openai.ChatCompletionToolMessageParamContentUnion{
						OfString: param.NewOpt(contentStr),
					},
					ToolCallID: fc.ID,
					Role:       constant.Tool("tool"),
				}
				if extraContent := openAIExtraContentForThoughtSignature(thoughtSignatures[fc.ID]); extraContent != nil {
					toolMessage.SetExtraFields(map[string]any{openAIExtraContentKey: extraContent})
				}
				toolResponseMessages = append(toolResponseMessages, openai.ChatCompletionMessageParamUnion{OfTool: &toolMessage})
			}
			textContent := strings.Join(textParts, "\n")
			asst := openai.ChatCompletionAssistantMessageParam{
				Role:      constant.Assistant("assistant"),
				ToolCalls: toolCalls,
			}
			if len(textParts) > 0 {
				asst.Content.OfString = param.NewOpt(textContent)
			}
			messages = append(messages, openai.ChatCompletionMessageParamUnion{OfAssistant: &asst})
			messages = append(messages, toolResponseMessages...)
		} else {
			if len(imageParts) > 0 {
				parts := make([]openai.ChatCompletionContentPartUnionParam, 0, len(textParts)+len(imageParts))
				for _, t := range textParts {
					parts = append(parts, openai.TextContentPart(t))
				}
				for _, img := range imageParts {
					parts = append(parts, openai.ImageContentPart(img))
				}
				messages = append(messages, openai.UserMessage(parts))
			} else if len(textParts) > 0 {
				messages = append(messages, openai.UserMessage(strings.Join(textParts, "\n")))
			}
		}
	}
	return messages, systemInstruction
}

func genaiToolsToOpenAITools(tools []*genai.Tool) []openai.ChatCompletionToolUnionParam {
	var out []openai.ChatCompletionToolUnionParam
	for _, t := range tools {
		if t == nil || t.FunctionDeclarations == nil {
			continue
		}
		for _, fd := range t.FunctionDeclarations {
			if fd == nil {
				continue
			}
			paramsMap := make(shared.FunctionParameters)
			if fd.ParametersJsonSchema != nil {
				if m := parametersJsonSchemaToMap(fd.ParametersJsonSchema); m != nil {
					maps.Copy(paramsMap, m)
				}
			} else if fd.Parameters != nil {
				if m := genaiSchemaToMap(fd.Parameters); m != nil {
					maps.Copy(paramsMap, m)
				}
			}
			// OpenAI requires object schemas to have a "properties" field.
			if _, ok := paramsMap["type"]; !ok {
				paramsMap["type"] = "object"
			}
			if paramsMap["type"] == "object" {
				if _, ok := paramsMap["properties"]; !ok {
					paramsMap["properties"] = map[string]any{}
				}
			}
			def := shared.FunctionDefinitionParam{
				Name:        fd.Name,
				Parameters:  paramsMap,
				Description: openai.String(fd.Description),
			}
			out = append(out, openai.ChatCompletionFunctionTool(def))
		}
	}
	return out
}

func runStreaming(ctx context.Context, m *OpenAIModel, params openai.ChatCompletionNewParams, yield func(*model.LLMResponse, error) bool) {
	params.StreamOptions = openai.ChatCompletionStreamOptionsParam{
		IncludeUsage: param.NewOpt(true),
	}
	stream := m.Client.Chat.Completions.NewStreaming(ctx, params, openAIPassthroughOpts(ctx, m)...)
	defer stream.Close()

	var aggregatedText strings.Builder
	toolCallsAcc := make(map[int64]map[string]any)
	var finishReason string
	var promptTokens, completionTokens int64

	for stream.Next() {
		chunk := stream.Current()
		if chunk.Usage.PromptTokens > 0 || chunk.Usage.CompletionTokens > 0 {
			promptTokens = chunk.Usage.PromptTokens
			completionTokens = chunk.Usage.CompletionTokens
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		delta := choice.Delta
		if delta.Content != "" {
			aggregatedText.WriteString(delta.Content)
			if !yield(&model.LLMResponse{
				Partial:      true,
				TurnComplete: choice.FinishReason != "",
				Content:      &genai.Content{Role: string(genai.RoleModel), Parts: []*genai.Part{{Text: delta.Content}}},
			}, nil) {
				return
			}
		}
		for _, tc := range delta.ToolCalls {
			idx := tc.Index
			if toolCallsAcc[idx] == nil {
				toolCallsAcc[idx] = map[string]any{"id": "", "name": "", "arguments": "", "thought_signature": []byte(nil)}
			}
			if tc.ID != "" {
				toolCallsAcc[idx]["id"] = tc.ID
			}
			if tc.Function.Name != "" {
				toolCallsAcc[idx]["name"] = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				prev, _ := toolCallsAcc[idx]["arguments"].(string)
				toolCallsAcc[idx]["arguments"] = prev + tc.Function.Arguments
			}
			if thoughtSignature := extractThoughtSignatureFromExtraFields(tc.JSON.ExtraFields); len(thoughtSignature) > 0 {
				toolCallsAcc[idx]["thought_signature"] = thoughtSignature
			}
		}
		if choice.FinishReason != "" {
			finishReason = choice.FinishReason
		}
	}

	if err := stream.Err(); err != nil {
		if ctx.Err() == context.Canceled {
			return
		}
		_ = yield(&model.LLMResponse{ErrorCode: "STREAM_ERROR", ErrorMessage: err.Error()}, nil)
		return
	}

	// Final response: build parts in index order
	nToolCalls := len(toolCallsAcc)
	indices := make([]int64, 0, nToolCalls)
	for k := range toolCallsAcc {
		indices = append(indices, k)
	}
	slices.Sort(indices)
	finalParts := make([]*genai.Part, 0, 1+nToolCalls)
	text := aggregatedText.String()
	if text != "" {
		finalParts = append(finalParts, &genai.Part{Text: text})
	}
	for _, idx := range indices {
		tc := toolCallsAcc[idx]
		argsStr, _ := tc["arguments"].(string)
		var args map[string]any
		if argsStr != "" {
			_ = json.Unmarshal([]byte(argsStr), &args)
		}
		name, _ := tc["name"].(string)
		id, _ := tc["id"].(string)
		if name != "" || id != "" {
			thoughtSignature, _ := tc["thought_signature"].([]byte)
			p := newFunctionCallPart(name, args, id, thoughtSignature)
			finalParts = append(finalParts, p)
		}
	}

	var usage *genai.GenerateContentResponseUsageMetadata
	if promptTokens > 0 || completionTokens > 0 {
		usage = &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(promptTokens),
			CandidatesTokenCount: int32(completionTokens),
		}
	}
	resp := &model.LLMResponse{
		Partial:       false,
		TurnComplete:  true,
		FinishReason:  openAIFinishReasonToGenai(finishReason),
		UsageMetadata: usage,
		Content:       &genai.Content{Role: string(genai.RoleModel), Parts: finalParts},
	}
	telemetry.SetLLMResponseAttributes(ctx, resp)
	_ = yield(resp, nil)
}

func runNonStreaming(ctx context.Context, m *OpenAIModel, params openai.ChatCompletionNewParams, yield func(*model.LLMResponse, error) bool) {
	completion, err := m.Client.Chat.Completions.New(ctx, params, openAIPassthroughOpts(ctx, m)...)
	if err != nil {
		yield(nil, fmt.Errorf("OpenAI chat completion request failed: %w", err))
		return
	}
	if len(completion.Choices) == 0 {
		yield(&model.LLMResponse{ErrorCode: "API_ERROR", ErrorMessage: "No choices in response"}, nil)
		return
	}
	resp := chatCompletionToLLMResponse(completion)
	telemetry.SetLLMResponseAttributes(ctx, resp)
	yield(resp, nil)
}

func chatCompletionToLLMResponse(completion *openai.ChatCompletion) *model.LLMResponse {
	choice := completion.Choices[0]
	msg := choice.Message
	nParts := 0
	if msg.Content != "" {
		nParts++
	}
	nParts += len(msg.ToolCalls)
	parts := make([]*genai.Part, 0, nParts)
	if msg.Content != "" {
		parts = append(parts, &genai.Part{Text: msg.Content})
	}
	for _, tc := range msg.ToolCalls {
		if tc.Type == openAIToolTypeFunction && tc.Function.Name != "" {
			var args map[string]any
			if tc.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			}
			functionToolCall := tc.AsFunction()
			p := newFunctionCallPart(
				tc.Function.Name,
				args,
				tc.ID,
				extractThoughtSignatureFromExtraFields(functionToolCall.JSON.ExtraFields),
			)
			parts = append(parts, p)
		}
	}
	var usage *genai.GenerateContentResponseUsageMetadata
	if completion.Usage.PromptTokens > 0 || completion.Usage.CompletionTokens > 0 {
		usage = &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(completion.Usage.PromptTokens),
			CandidatesTokenCount: int32(completion.Usage.CompletionTokens),
		}
	}
	return &model.LLMResponse{
		Partial:       false,
		TurnComplete:  true,
		FinishReason:  openAIFinishReasonToGenai(choice.FinishReason),
		UsageMetadata: usage,
		Content:       &genai.Content{Role: string(genai.RoleModel), Parts: parts},
	}
}
