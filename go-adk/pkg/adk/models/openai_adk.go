// Package models: OpenAI model implementing Google ADK model.LLM using genai types only.
package models

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"sort"
	"strings"

	"github.com/sashabaranov/go-openai"
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

		openaiReq := openai.ChatCompletionRequest{
			Model:    modelName,
			Messages: messages,
		}
		if systemInstruction != "" {
			openaiReq.Messages = append([]openai.ChatCompletionMessage{{
				Role:    openai.ChatMessageRoleSystem,
				Content: systemInstruction,
			}}, openaiReq.Messages...)
		}
		applyOpenAIConfig(&openaiReq, m.Config)

		if req.Config != nil && len(req.Config.Tools) > 0 {
			openaiReq.Tools = genaiToolsToOpenAITools(req.Config.Tools)
			openaiReq.ToolChoice = "auto"
		}

		if stream {
			runStreaming(ctx, m, openaiReq, yield)
		} else {
			runNonStreaming(ctx, m, openaiReq, yield)
		}
	}
}

func applyOpenAIConfig(req *openai.ChatCompletionRequest, cfg *OpenAIConfig) {
	if cfg.Temperature != nil {
		req.Temperature = float32(*cfg.Temperature)
	}
	if cfg.MaxTokens != nil {
		req.MaxTokens = *cfg.MaxTokens
	}
	if cfg.TopP != nil {
		req.TopP = float32(*cfg.TopP)
	}
	if cfg.FrequencyPenalty != nil {
		req.FrequencyPenalty = float32(*cfg.FrequencyPenalty)
	}
	if cfg.PresencePenalty != nil {
		req.PresencePenalty = float32(*cfg.PresencePenalty)
	}
	if cfg.Seed != nil {
		req.Seed = cfg.Seed
	}
	if cfg.N != nil {
		req.N = *cfg.N
	}
}

func genaiContentsToOpenAIMessages(contents []*genai.Content, config *genai.GenerateContentConfig) ([]openai.ChatCompletionMessage, string) {
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

	var messages []openai.ChatCompletionMessage
	for _, content := range contents {
		if content == nil || strings.TrimSpace(content.Role) == "system" {
			continue
		}
		role := genaiRoleToOpenAI(content.Role)
		var textParts []string
		var functionCalls []*genai.FunctionCall
		var imageParts []openai.ChatMessageImageURL

		for _, part := range content.Parts {
			if part == nil {
				continue
			}
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			} else if part.FunctionCall != nil {
				functionCalls = append(functionCalls, part.FunctionCall)
			} else if part.InlineData != nil && strings.HasPrefix(part.InlineData.MIMEType, "image/") {
				imageParts = append(imageParts, openai.ChatMessageImageURL{
					URL: fmt.Sprintf("data:%s;base64,%s", part.InlineData.MIMEType, base64.StdEncoding.EncodeToString(part.InlineData.Data)),
				})
			}
		}

		if len(functionCalls) > 0 && role == openai.ChatMessageRoleAssistant {
			toolCalls := make([]openai.ToolCall, 0, len(functionCalls))
			var toolResponseMessages []openai.ChatCompletionMessage
			for _, fc := range functionCalls {
				argsJSON, _ := json.Marshal(fc.Args)
				toolCalls = append(toolCalls, openai.ToolCall{
					ID:   fc.ID,
					Type: openai.ToolTypeFunction,
					Function: openai.FunctionCall{Name: fc.Name, Arguments: string(argsJSON)},
				})
				contentStr := "No response available for this function call."
				if fr := functionResponses[fc.ID]; fr != nil {
					contentStr = functionResponseContentString(fr.Response)
				}
				toolResponseMessages = append(toolResponseMessages, openai.ChatCompletionMessage{
					Role: openai.ChatMessageRoleTool, ToolCallID: fc.ID, Content: contentStr,
				})
			}
			textContent := strings.Join(textParts, "\n")
			msg := openai.ChatCompletionMessage{Role: role, ToolCalls: toolCalls}
			if len(textParts) > 0 {
				msg.Content = textContent
			}
			messages = append(messages, msg)
			messages = append(messages, toolResponseMessages...)
		} else {
			contentParts := []openai.ChatMessagePart{}
			for _, t := range textParts {
				contentParts = append(contentParts, openai.ChatMessagePart{Type: openai.ChatMessagePartTypeText, Text: t})
			}
			for _, img := range imageParts {
				contentParts = append(contentParts, openai.ChatMessagePart{Type: openai.ChatMessagePartTypeImageURL, ImageURL: &img})
			}
			if len(contentParts) > 0 {
				messages = append(messages, openai.ChatCompletionMessage{Role: role, MultiContent: contentParts})
			} else if len(textParts) > 0 {
				messages = append(messages, openai.ChatCompletionMessage{Role: role, Content: strings.Join(textParts, "\n")})
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

func genaiRoleToOpenAI(role string) string {
	switch strings.TrimSpace(role) {
	case "model", "assistant":
		return openai.ChatMessageRoleAssistant
	case "system":
		return openai.ChatMessageRoleSystem
	default:
		return openai.ChatMessageRoleUser
	}
}

func genaiToolsToOpenAITools(tools []*genai.Tool) []openai.Tool {
	var out []openai.Tool
	for _, t := range tools {
		if t == nil || t.FunctionDeclarations == nil {
			continue
		}
		for _, fd := range t.FunctionDeclarations {
			if fd == nil {
				continue
			}
			params := make(map[string]interface{})
			if fd.ParametersJsonSchema != nil {
				if m, ok := fd.ParametersJsonSchema.(map[string]interface{}); ok {
					params = m
				}
			}
			out = append(out, openai.Tool{
				Type: openai.ToolTypeFunction,
				Function: &openai.FunctionDefinition{
					Name:        fd.Name,
					Description: fd.Description,
					Parameters:  params,
				},
			})
		}
	}
	return out
}

func runStreaming(ctx context.Context, m *OpenAIModel, req openai.ChatCompletionRequest, yield func(*model.LLMResponse, error) bool) {
	req.Stream = true
	// Both OpenAI and Azure constructors set m.Client; Azure does not set AzureClient.
	if m.Client == nil {
		yield(nil, errors.New("OpenAI client is nil"))
		return
	}
	stream, err := m.Client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		yield(nil, err)
		return
	}
	defer stream.Close()

	var aggregatedText string
	toolCallsAcc := make(map[int]map[string]interface{})
	var finishReason string

	for {
		response, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) || err.Error() == "stream closed" {
				break
			}
			if ctx.Err() == context.Canceled {
				break
			}
			yield(&model.LLMResponse{ErrorCode: "STREAM_ERROR", ErrorMessage: err.Error()}, nil)
			return
		}
		if len(response.Choices) == 0 {
			continue
		}
		choice := response.Choices[0]
		delta := choice.Delta
		if delta.Content != "" {
			aggregatedText += delta.Content
			yield(&model.LLMResponse{
				Partial:      true,
				TurnComplete: choice.FinishReason != "",
				Content:      &genai.Content{Role: string(genai.RoleModel), Parts: []*genai.Part{{Text: delta.Content}}},
			}, nil)
		}
		for _, tc := range delta.ToolCalls {
			idx := 0
			if tc.Index != nil {
				idx = *tc.Index
			}
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
			finishReason = string(choice.FinishReason)
		}
	}

	// Final response
	finalParts := []*genai.Part{}
	if aggregatedText != "" {
		finalParts = append(finalParts, &genai.Part{Text: aggregatedText})
	}
	indices := make([]int, 0, len(toolCallsAcc))
	for k := range toolCallsAcc {
		indices = append(indices, k)
	}
	sort.Ints(indices)
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
	yield(&model.LLMResponse{
		Partial:      false,
		TurnComplete: true,
		FinishReason: fr,
		Content:      &genai.Content{Role: string(genai.RoleModel), Parts: finalParts},
	}, nil)
}

func runNonStreaming(ctx context.Context, m *OpenAIModel, req openai.ChatCompletionRequest, yield func(*model.LLMResponse, error) bool) {
	req.Stream = false
	// Both OpenAI and Azure constructors set m.Client; Azure does not set AzureClient.
	if m.Client == nil {
		yield(nil, errors.New("OpenAI client is nil"))
		return
	}
	response, err := m.Client.CreateChatCompletion(ctx, req)
	if err != nil {
		yield(nil, err)
		return
	}
	if len(response.Choices) == 0 {
		yield(&model.LLMResponse{ErrorCode: "API_ERROR", ErrorMessage: "No choices in response"}, nil)
		return
	}
	choice := response.Choices[0]
	msg := choice.Message
	parts := []*genai.Part{}
	if msg.Content != "" {
		parts = append(parts, &genai.Part{Text: msg.Content})
	}
	for _, tc := range msg.ToolCalls {
		if tc.Type == openai.ToolTypeFunction && tc.Function.Name != "" {
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
	case openai.FinishReasonLength:
		fr = genai.FinishReasonMaxTokens
	case openai.FinishReasonContentFilter:
		fr = genai.FinishReasonSafety
	}
	var usage *genai.GenerateContentResponseUsageMetadata
	if response.Usage.PromptTokens > 0 || response.Usage.CompletionTokens > 0 {
		usage = &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:     int32(response.Usage.PromptTokens),
			CandidatesTokenCount: int32(response.Usage.CompletionTokens),
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
