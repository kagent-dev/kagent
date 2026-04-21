package models

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"iter"
	"net/http"
	"slices"
	"strings"

	"google.golang.org/adk/model"
	"google.golang.org/genai"
)

func (m *SAPAICoreModel) Name() string {
	return m.Config.Model
}

func (m *SAPAICoreModel) GenerateContent(ctx context.Context, req *model.LLMRequest, stream bool) iter.Seq2[*model.LLMResponse, error] {
	return func(yield func(*model.LLMResponse, error) bool) {
		resp, err := m.doRequest(ctx, req, stream)
		if err != nil {
			if isRetryableError(err) {
				m.invalidateToken()
				m.invalidateDeploymentURL()
				var he *orchHTTPError
				if errors.As(err, &he) {
					m.Logger.Info("SAP AI Core request failed, retrying", "status", he.StatusCode, "url", he.URL)
				} else {
					m.Logger.Info("SAP AI Core request failed, retrying", "error", err)
				}
				resp, err = m.doRequest(ctx, req, stream)
				if err != nil {
					yield(nil, fmt.Errorf("SAP AI Core retry failed: %w", err))
					return
				}
			} else {
				yield(nil, fmt.Errorf("SAP AI Core request failed: %w", err))
				return
			}
		}
		defer resp.Body.Close()

		if stream {
			m.handleStream(ctx, resp.Body, yield)
		} else {
			m.handleNonStream(resp.Body, yield)
		}
	}
}

func (m *SAPAICoreModel) doRequest(ctx context.Context, req *model.LLMRequest, stream bool) (*http.Response, error) {
	deploymentURL, err := m.resolveDeploymentURL(ctx)
	if err != nil {
		return nil, err
	}

	token, err := m.ensureToken(ctx)
	if err != nil {
		return nil, err
	}

	body := m.buildOrchestrationBody(req, stream)
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	url := deploymentURL + "/v2/completion"
	httpReq, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Authorization", "Bearer "+token)
	httpReq.Header.Set("AI-Resource-Group", m.Config.ResourceGroup)
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(httpReq)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, &orchHTTPError{StatusCode: resp.StatusCode, Body: string(errBody), URL: url}
	}

	return resp, nil
}

type orchHTTPError struct {
	StatusCode int
	Body       string
	URL        string
}

func (e *orchHTTPError) Error() string {
	return fmt.Sprintf("SAP AI Core returned HTTP %d (url: %s): %s", e.StatusCode, e.URL, e.Body)
}

func isRetryableError(err error) bool {
	if he, ok := err.(*orchHTTPError); ok {
		switch he.StatusCode {
		case 401, 403, 404, 502, 503, 504:
			return true
		}
	}
	return false
}

func (m *SAPAICoreModel) buildOrchestrationBody(req *model.LLMRequest, stream bool) map[string]any {
	messages, systemInstruction := genaiContentsToOrchTemplate(req.Contents, req.Config)
	if systemInstruction != "" {
		messages = append([]map[string]any{{"role": "system", "content": systemInstruction}}, messages...)
	}

	modelName := req.Model
	if modelName == "" {
		modelName = m.Config.Model
	}

	params := map[string]any{}
	if req.Config != nil {
		if req.Config.Temperature != nil {
			params["temperature"] = *req.Config.Temperature
		}
		if req.Config.MaxOutputTokens > 0 {
			params["max_tokens"] = req.Config.MaxOutputTokens
		}
		if req.Config.TopP != nil {
			params["top_p"] = *req.Config.TopP
		}
	}

	promptConfig := map[string]any{
		"template": messages,
	}

	if req.Config != nil && len(req.Config.Tools) > 0 {
		tools := genaiToolsToOrchTools(req.Config.Tools)
		if len(tools) > 0 {
			promptConfig["tools"] = tools
		}
	}

	return map[string]any{
		"config": map[string]any{
			"modules": map[string]any{
				"prompt_templating": map[string]any{
					"prompt": promptConfig,
					"model": map[string]any{
						"name":    modelName,
						"params":  params,
						"version": "latest",
					},
				},
			},
			"stream": map[string]any{
				"enabled": stream,
			},
		},
	}
}

func genaiContentsToOrchTemplate(contents []*genai.Content, config *genai.GenerateContentConfig) ([]map[string]any, string) {
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
	for _, c := range contents {
		if c == nil {
			continue
		}
		for _, p := range c.Parts {
			if p != nil && p.FunctionResponse != nil {
				functionResponses[p.FunctionResponse.ID] = p.FunctionResponse
			}
		}
	}

	var messages []map[string]any
	for _, content := range contents {
		if content == nil || strings.TrimSpace(content.Role) == "system" {
			continue
		}
		role := "user"
		if content.Role == "model" || content.Role == "assistant" {
			role = "assistant"
		}

		var textParts []string
		var functionCalls []*genai.FunctionCall

		for _, part := range content.Parts {
			if part == nil {
				continue
			}
			if part.Text != "" {
				textParts = append(textParts, part.Text)
			} else if part.FunctionCall != nil {
				functionCalls = append(functionCalls, part.FunctionCall)
			}
		}

		if len(functionCalls) > 0 && role == "assistant" {
			toolCalls := make([]map[string]any, 0, len(functionCalls))
			var toolResponses []map[string]any
			for _, fc := range functionCalls {
				argsJSON, _ := json.Marshal(fc.Args)
				tc := map[string]any{
					"type": "function",
					"function": map[string]any{
						"name":      fc.Name,
						"arguments": string(argsJSON),
					},
				}
				if fc.ID != "" {
					tc["id"] = fc.ID
				}
				toolCalls = append(toolCalls, tc)

				respContent := "No response available."
				if fr := functionResponses[fc.ID]; fr != nil {
					respContent = extractFunctionResponseContent(fr.Response)
				}
				toolResponses = append(toolResponses, map[string]any{
					"role":         "tool",
					"tool_call_id": fc.ID,
					"content":      respContent,
				})
			}

			msg := map[string]any{"role": "assistant", "tool_calls": toolCalls}
			if len(textParts) > 0 {
				msg["content"] = strings.Join(textParts, "\n")
			} else {
				msg["content"] = ""
			}
			messages = append(messages, msg)
			messages = append(messages, toolResponses...)
		} else if len(textParts) > 0 {
			messages = append(messages, map[string]any{
				"role":    role,
				"content": strings.Join(textParts, "\n"),
			})
		}
	}

	return messages, systemInstruction
}

func genaiToolsToOrchTools(tools []*genai.Tool) []map[string]any {
	var out []map[string]any
	for _, t := range tools {
		if t == nil || t.FunctionDeclarations == nil {
			continue
		}
		for _, fd := range t.FunctionDeclarations {
			if fd == nil {
				continue
			}
			params := map[string]any{"type": "object", "properties": map[string]any{}}
			if fd.ParametersJsonSchema != nil {
				if m, ok := fd.ParametersJsonSchema.(map[string]any); ok {
					params = m
				}
			} else if fd.Parameters != nil {
				if m := genaiSchemaToMap(fd.Parameters); m != nil {
					params = m
				}
			}
			out = append(out, map[string]any{
				"type": "function",
				"function": map[string]any{
					"name":        fd.Name,
					"description": fd.Description,
					"parameters":  params,
				},
			})
		}
	}
	return out
}

func (m *SAPAICoreModel) handleStream(ctx context.Context, body io.Reader, yield func(*model.LLMResponse, error) bool) {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var aggregatedText string
	toolCallsAcc := make(map[int64]map[string]any)
	var finishReason string
	var promptTokens, completionTokens int64

	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		payload := line
		if strings.HasPrefix(line, "data: ") {
			payload = line[6:]
		}
		if payload == "[DONE]" {
			break
		}

		var event map[string]any
		if err := json.Unmarshal([]byte(payload), &event); err != nil {
			continue
		}

		if _, ok := event["code"]; ok {
			yield(nil, fmt.Errorf("SAP AI Core stream error: %s", payload))
			return
		}

		chunk := parseOrchChunk(event)
		if chunk == nil {
			continue
		}

		choices, _ := chunk["choices"].([]any)
		for _, c := range choices {
			choice, ok := c.(map[string]any)
			if !ok {
				continue
			}
			delta, _ := choice["delta"].(map[string]any)
			if content, ok := delta["content"].(string); ok && content != "" {
				aggregatedText += content
				if !yield(&model.LLMResponse{
					Partial:      true,
					TurnComplete: false,
					Content:      &genai.Content{Role: string(genai.RoleModel), Parts: []*genai.Part{{Text: content}}},
				}, nil) {
					return
				}
			}

			if tcs, ok := delta["tool_calls"].([]any); ok {
				for _, tcRaw := range tcs {
					tc, ok := tcRaw.(map[string]any)
					if !ok {
						continue
					}
					idx := int64(0)
					if v, ok := tc["index"].(float64); ok {
						idx = int64(v)
					}
					if toolCallsAcc[idx] == nil {
						toolCallsAcc[idx] = map[string]any{"id": "", "name": "", "arguments": ""}
					}
					if id, ok := tc["id"].(string); ok && id != "" {
						toolCallsAcc[idx]["id"] = id
					}
					if fn, ok := tc["function"].(map[string]any); ok {
						if name, ok := fn["name"].(string); ok && name != "" {
							toolCallsAcc[idx]["name"] = name
						}
						if args, ok := fn["arguments"].(string); ok && args != "" {
							prev, _ := toolCallsAcc[idx]["arguments"].(string)
							toolCallsAcc[idx]["arguments"] = prev + args
						}
					}
				}
			}

			if fr, ok := choice["finish_reason"].(string); ok && fr != "" {
				finishReason = fr
			}
		}

		if usage, ok := chunk["usage"].(map[string]any); ok {
			if v, ok := usage["prompt_tokens"].(float64); ok {
				promptTokens = int64(v)
			}
			if v, ok := usage["completion_tokens"].(float64); ok {
				completionTokens = int64(v)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() == context.Canceled {
			return
		}
		yield(nil, fmt.Errorf("SAP AI Core stream read error: %w", err))
		return
	}

	indices := make([]int64, 0, len(toolCallsAcc))
	for k := range toolCallsAcc {
		indices = append(indices, k)
	}
	slices.Sort(indices)

	finalParts := make([]*genai.Part, 0, 1+len(toolCallsAcc))
	if aggregatedText != "" {
		finalParts = append(finalParts, &genai.Part{Text: aggregatedText})
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
			p := genai.NewPartFromFunctionCall(name, args)
			p.FunctionCall.ID = id
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

	yield(&model.LLMResponse{
		Partial:       false,
		TurnComplete:  true,
		FinishReason:  openAIFinishReasonToGenai(finishReason),
		UsageMetadata: usage,
		Content:       &genai.Content{Role: string(genai.RoleModel), Parts: finalParts},
	}, nil)
}

func (m *SAPAICoreModel) handleNonStream(body io.Reader, yield func(*model.LLMResponse, error) bool) {
	var data map[string]any
	if err := json.NewDecoder(body).Decode(&data); err != nil {
		yield(nil, fmt.Errorf("failed to decode SAP AI Core response: %w", err))
		return
	}

	result, ok := data["final_result"].(map[string]any)
	if !ok {
		result = data
	}

	choices, _ := result["choices"].([]any)
	if len(choices) == 0 {
		yield(&model.LLMResponse{ErrorCode: "API_ERROR", ErrorMessage: "No choices in response"}, nil)
		return
	}

	parts := make([]*genai.Part, 0)
	firstChoice, _ := choices[0].(map[string]any)
	msg, _ := firstChoice["message"].(map[string]any)

	if content, ok := msg["content"].(string); ok && content != "" {
		parts = append(parts, &genai.Part{Text: content})
	}

	if toolCalls, ok := msg["tool_calls"].([]any); ok {
		for _, tcRaw := range toolCalls {
			tc, ok := tcRaw.(map[string]any)
			if !ok {
				continue
			}
			fn, _ := tc["function"].(map[string]any)
			name, _ := fn["name"].(string)
			argsStr, _ := fn["arguments"].(string)
			var args map[string]any
			if argsStr != "" {
				_ = json.Unmarshal([]byte(argsStr), &args)
			}
			id, _ := tc["id"].(string)
			p := genai.NewPartFromFunctionCall(name, args)
			p.FunctionCall.ID = id
			parts = append(parts, p)
		}
	}

	var usage *genai.GenerateContentResponseUsageMetadata
	if u, ok := result["usage"].(map[string]any); ok {
		pt, _ := u["prompt_tokens"].(float64)
		ct, _ := u["completion_tokens"].(float64)
		if pt > 0 || ct > 0 {
			usage = &genai.GenerateContentResponseUsageMetadata{
				PromptTokenCount:     int32(pt),
				CandidatesTokenCount: int32(ct),
			}
		}
	}

	fr := "stop"
	if f, ok := firstChoice["finish_reason"].(string); ok {
		fr = f
	}

	yield(&model.LLMResponse{
		Partial:       false,
		TurnComplete:  true,
		FinishReason:  openAIFinishReasonToGenai(fr),
		UsageMetadata: usage,
		Content:       &genai.Content{Role: string(genai.RoleModel), Parts: parts},
	}, nil)
}

func parseOrchChunk(event map[string]any) map[string]any {
	if r, ok := event["orchestration_result"].(map[string]any); ok {
		return r
	}
	if r, ok := event["final_result"].(map[string]any); ok {
		return r
	}
	if _, ok := event["choices"]; ok {
		return event
	}
	return nil
}
