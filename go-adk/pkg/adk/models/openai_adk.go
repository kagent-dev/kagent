// Package models: OpenAI model implementing Google ADK model.LLM using genai types only.
package models

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"iter"
	"sort"
	"strings"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/shared"
	"github.com/openai/openai-go/v3/shared/constant"
	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

// Name implements model.LLM.
func (m *OpenAIModel) Name() string {
	return "openai"
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
	var systemInstruction string
	if config != nil && config.SystemInstruction != nil {
		for _, p := range config.SystemInstruction.Parts {
			if p != nil && p.Text != "" {
				systemInstruction += p.Text + "\n"
			}
		}
		systemInstruction = strings.TrimSpace(systemInstruction)
	}

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

	var messages []openai.ChatCompletionMessageParamUnion
	for _, content := range contents {
		if content == nil || strings.TrimSpace(content.Role) == "system" {
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

		if len(functionCalls) > 0 && (role == "model" || role == "assistant") {
			toolCalls := make([]openai.ChatCompletionMessageToolCallUnionParam, 0, len(functionCalls))
			var toolResponseMessages []openai.ChatCompletionMessageParamUnion
			for _, fc := range functionCalls {
				argsJSON, _ := json.Marshal(fc.Args)
				toolCalls = append(toolCalls, openai.ChatCompletionMessageToolCallUnionParam{
					OfFunction: &openai.ChatCompletionMessageFunctionToolCallParam{
						ID:   fc.ID,
						Type: constant.Function("function"),
						Function: openai.ChatCompletionMessageFunctionToolCallFunctionParam{
							Name:      fc.Name,
							Arguments: string(argsJSON),
						},
					},
				})
				contentStr := "No response available for this function call."
				if fr := functionResponses[fc.ID]; fr != nil {
					contentStr = functionResponseContentString(fr.Response)
				}
				toolResponseMessages = append(toolResponseMessages, openai.ToolMessage(contentStr, fc.ID))
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

func functionResponseContentString(resp any) string {
	if resp == nil {
		return ""
	}
	if s, ok := resp.(string); ok {
		return s
	}
	if m, ok := resp.(map[string]interface{}); ok {
		if c, ok := m["content"].([]interface{}); ok && len(c) > 0 {
			if item, ok := c[0].(map[string]interface{}); ok {
				if t, ok := item["text"].(string); ok {
					return t
				}
			}
		}
		if r, ok := m["result"].(string); ok {
			return r
		}
	}
	b, _ := json.Marshal(resp)
	return string(b)
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
				if m, ok := fd.ParametersJsonSchema.(map[string]interface{}); ok {
					for k, v := range m {
						paramsMap[k] = v
					}
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
	stream := m.Client.Chat.Completions.NewStreaming(ctx, params)
	defer stream.Close()

	var aggregatedText string
	toolCallsAcc := make(map[int64]map[string]interface{})
	var finishReason string

	for stream.Next() {
		chunk := stream.Current()
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		delta := choice.Delta
		if delta.Content != "" {
			aggregatedText += delta.Content
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
				toolCallsAcc[idx] = map[string]interface{}{"id": "", "name": "", "arguments": ""}
			}
			if tc.ID != "" {
				toolCallsAcc[idx]["id"] = tc.ID
			}
			if tc.Function.Name != "" {
				toolCallsAcc[idx]["name"] = tc.Function.Name
			}
			if tc.Function.Arguments != "" {
				toolCallsAcc[idx]["arguments"] = toolCallsAcc[idx]["arguments"].(string) + tc.Function.Arguments
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

	// Final response
	finalParts := []*genai.Part{}
	if aggregatedText != "" {
		finalParts = append(finalParts, &genai.Part{Text: aggregatedText})
	}
	var indices []int64
	for k := range toolCallsAcc {
		indices = append(indices, k)
	}
	sort.Slice(indices, func(i, j int) bool { return indices[i] < indices[j] })
	for _, idx := range indices {
		tc := toolCallsAcc[idx]
		argsStr, _ := tc["arguments"].(string)
		var args map[string]interface{}
		if argsStr != "" {
			_ = json.Unmarshal([]byte(argsStr), &args)
		}
		name, _ := tc["name"].(string)
		id, _ := tc["id"].(string)
		if name != "" || id != "" {
			finalParts = append(finalParts, genai.NewPartFromFunctionCall(name, args))
			finalParts[len(finalParts)-1].FunctionCall.ID = id
		}
	}
	fr := genai.FinishReasonStop
	if finishReason == "tool_calls" {
		fr = genai.FinishReasonStop
	} else if finishReason == "length" {
		fr = genai.FinishReasonMaxTokens
	} else if finishReason == "content_filter" {
		fr = genai.FinishReasonSafety
	}
	_ = yield(&model.LLMResponse{
		Partial:      false,
		TurnComplete: true,
		FinishReason: fr,
		Content:      &genai.Content{Role: string(genai.RoleModel), Parts: finalParts},
	}, nil)
}

func runNonStreaming(ctx context.Context, m *OpenAIModel, params openai.ChatCompletionNewParams, yield func(*model.LLMResponse, error) bool) {
	completion, err := m.Client.Chat.Completions.New(ctx, params)
	if err != nil {
		yield(nil, err)
		return
	}
	if len(completion.Choices) == 0 {
		yield(&model.LLMResponse{ErrorCode: "API_ERROR", ErrorMessage: "No choices in response"}, nil)
		return
	}
	choice := completion.Choices[0]
	msg := choice.Message
	parts := []*genai.Part{}
	if msg.Content != "" {
		parts = append(parts, &genai.Part{Text: msg.Content})
	}
	for _, tc := range msg.ToolCalls {
		if tc.Type == "function" && tc.Function.Name != "" {
			var args map[string]interface{}
			if tc.Function.Arguments != "" {
				_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			}
			p := genai.NewPartFromFunctionCall(tc.Function.Name, args)
			p.FunctionCall.ID = tc.ID
			parts = append(parts, p)
		}
	}
	fr := genai.FinishReasonStop
	switch choice.FinishReason {
	case "length":
		fr = genai.FinishReasonMaxTokens
	case "content_filter":
		fr = genai.FinishReasonSafety
	}
	var usage *genai.GenerateContentResponseUsageMetadata
	if completion.Usage.PromptTokens > 0 || completion.Usage.CompletionTokens > 0 {
		usage = &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(completion.Usage.PromptTokens),
			CandidatesTokenCount: int32(completion.Usage.CompletionTokens),
		}
	}
	yield(&model.LLMResponse{
		Partial:       false,
		TurnComplete:  true,
		FinishReason:  fr,
		UsageMetadata: usage,
		Content:       &genai.Content{Role: string(genai.RoleModel), Parts: parts},
	}, nil)
}
