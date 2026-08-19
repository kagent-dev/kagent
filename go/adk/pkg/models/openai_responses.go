// Package models: OpenAI Responses API path for OpenAIModel.
package models

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"strings"

	"github.com/kagent-dev/kagent/go/adk/pkg/telemetry"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"
)

func generateContentResponses(
	ctx context.Context,
	m *OpenAIModel,
	req *model.LLMRequest,
	modelName string,
	stream bool,
	yield func(*model.LLMResponse, error) bool,
) {
	input, instructions := genaiContentsToResponsesInput(req.Contents, req.Config)
	params := responses.ResponseNewParams{
		Model: shared.ResponsesModel(modelName),
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: input,
		},
	}
	if instructions != "" {
		params.Instructions = param.NewOpt(instructions)
	}
	applyOpenAIResponsesConfig(&params, m.Config)

	if req.Config != nil && len(req.Config.Tools) > 0 {
		params.Tools = genaiToolsToResponsesTools(req.Config.Tools)
		params.ToolChoice = responses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: param.NewOpt(responses.ToolChoiceOptionsAuto),
		}
	}

	if stream {
		runResponsesStreaming(ctx, m, params, yield)
	} else {
		runResponsesNonStreaming(ctx, m, params, yield)
	}
}

func applyOpenAIResponsesConfig(params *responses.ResponseNewParams, cfg *OpenAIConfig) {
	if cfg == nil {
		return
	}
	if cfg.Temperature != nil {
		params.Temperature = param.NewOpt(*cfg.Temperature)
	}
	// Responses uses max_output_tokens (same semantics as max_completion_tokens).
	if cfg.MaxCompletionTokens != nil {
		params.MaxOutputTokens = param.NewOpt(int64(*cfg.MaxCompletionTokens))
	} else if cfg.MaxTokens != nil {
		params.MaxOutputTokens = param.NewOpt(int64(*cfg.MaxTokens))
	}
	if cfg.TopP != nil {
		params.TopP = param.NewOpt(*cfg.TopP)
	}
	if cfg.ReasoningEffort != nil {
		params.Reasoning = shared.ReasoningParam{
			Effort: shared.ReasoningEffort(*cfg.ReasoningEffort),
		}
	}
}

func genaiContentsToResponsesInput(contents []*genai.Content, config *genai.GenerateContentConfig) (responses.ResponseInputParam, string) {
	instructions := mergeSystemInstructionFromConfig("", config)

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

	var input responses.ResponseInputParam
	for _, content := range contents {
		if content == nil || strings.TrimSpace(content.Role) == openAIRoleSystem {
			continue
		}
		role := strings.TrimSpace(content.Role)
		var textParts []string
		var functionCalls []*genai.FunctionCall
		var contentParts responses.ResponseInputMessageContentListParam

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
					img := responses.ResponseInputContentParamOfInputImage(responses.ResponseInputImageDetailAuto)
					img.OfInputImage.ImageURL = param.NewOpt(dataURI(mime, part.InlineData.Data))
					contentParts = append(contentParts, img)
				} else if isOpenAIResponsesFileMIME(mime, name) {
					// Responses input_file: PDF + txt/md/csv/docx/xlsx/pptx/…
					fileMIME := mime
					if isOpenAIPDF(mime, name) {
						fileMIME = "application/pdf"
					}
					fname := openAIFilename(name, fileMIME)
					file := responses.ResponseInputFileParam{
						FileData: param.NewOpt(dataURI(fileMIME, part.InlineData.Data)),
						Filename: param.NewOpt(fname),
					}
					contentParts = append(contentParts, responses.ResponseInputContentUnionParam{OfInputFile: &file})
				} else {
					textParts = append(textParts, unsupportedFileNote(name, mime))
				}
			} else if part.FileData != nil {
				mime := part.FileData.MIMEType
				name := fileDataName(part.FileData)
				uri := part.FileData.FileURI
				// file_url is Responses-only (Chat Completions docs: not supported).
				if uri != "" && isOpenAIResponsesFileMIME(mime, name) {
					file := responses.ResponseInputFileParam{
						FileURL:  param.NewOpt(uri),
						Filename: param.NewOpt(openAIFilename(name, mime)),
					}
					contentParts = append(contentParts, responses.ResponseInputContentUnionParam{OfInputFile: &file})
				} else {
					textParts = append(textParts, unsupportedFileNote(name, mime))
				}
			}
		}

		if len(functionCalls) > 0 && (role == openAIRoleModel || role == openAIRoleAssistant) {
			if len(textParts) > 0 {
				input = append(input, responses.ResponseInputItemParamOfMessage(
					strings.Join(textParts, "\n"),
					responses.EasyInputMessageRoleAssistant,
				))
			}
			for _, fc := range functionCalls {
				argsJSON, _ := json.Marshal(fc.Args)
				input = append(input, responses.ResponseInputItemParamOfFunctionCall(
					string(argsJSON),
					fc.ID,
					fc.Name,
				))
				output := "No response available for this function call."
				if fr := functionResponses[fc.ID]; fr != nil {
					output = extractFunctionResponseContent(fr.Response)
				}
				input = append(input, responses.ResponseInputItemParamOfFunctionCallOutput(fc.ID, output))
			}
			continue
		}

		if len(textParts) == 0 && len(contentParts) == 0 {
			continue
		}

		msgRole := responses.EasyInputMessageRoleUser
		if role == openAIRoleModel || role == openAIRoleAssistant {
			msgRole = responses.EasyInputMessageRoleAssistant
		}

		if len(contentParts) > 0 {
			parts := make(responses.ResponseInputMessageContentListParam, 0, len(textParts)+len(contentParts))
			for _, t := range textParts {
				parts = append(parts, responses.ResponseInputContentParamOfInputText(t))
			}
			parts = append(parts, contentParts...)
			input = append(input, responses.ResponseInputItemParamOfMessage(parts, msgRole))
		} else {
			input = append(input, responses.ResponseInputItemParamOfMessage(strings.Join(textParts, "\n"), msgRole))
		}
	}
	return input, instructions
}

func genaiToolsToResponsesTools(tools []*genai.Tool) []responses.ToolUnionParam {
	var out []responses.ToolUnionParam
	for _, t := range tools {
		if t == nil || t.FunctionDeclarations == nil {
			continue
		}
		for _, fd := range t.FunctionDeclarations {
			if fd == nil {
				continue
			}
			paramsMap := make(map[string]any)
			if fd.ParametersJsonSchema != nil {
				if m := parametersJsonSchemaToMap(fd.ParametersJsonSchema); m != nil {
					maps.Copy(paramsMap, m)
				}
			} else if fd.Parameters != nil {
				if m := genaiSchemaToMap(fd.Parameters); m != nil {
					maps.Copy(paramsMap, m)
				}
			}
			if _, ok := paramsMap["type"]; !ok {
				paramsMap["type"] = "object"
			}
			if paramsMap["type"] == "object" {
				if _, ok := paramsMap["properties"]; !ok {
					paramsMap["properties"] = map[string]any{}
				}
			}
			ft := responses.FunctionToolParam{
				Name:        fd.Name,
				Parameters:  paramsMap,
				Description: param.NewOpt(fd.Description),
				Strict:      param.NewOpt(false),
			}
			out = append(out, responses.ToolUnionParam{OfFunction: &ft})
		}
	}
	return out
}

func runResponsesNonStreaming(
	ctx context.Context,
	m *OpenAIModel,
	params responses.ResponseNewParams,
	yield func(*model.LLMResponse, error) bool,
) {
	resp, err := m.Client.Responses.New(ctx, params, openAIPassthroughOpts(ctx, m)...)
	if err != nil {
		yield(nil, fmt.Errorf("OpenAI responses request failed: %w", err))
		return
	}
	out := responseToLLMResponse(resp)
	telemetry.SetLLMResponseAttributes(ctx, out)
	yield(out, nil)
}

func runResponsesStreaming(
	ctx context.Context,
	m *OpenAIModel,
	params responses.ResponseNewParams,
	yield func(*model.LLMResponse, error) bool,
) {
	stream := m.Client.Responses.NewStreaming(ctx, params, openAIPassthroughOpts(ctx, m)...)
	defer stream.Close()

	var aggregatedText strings.Builder
	toolCalls := make(map[string]*genai.Part) // call_id -> part
	var toolCallOrder []string
	var usage *genai.GenerateContentResponseUsageMetadata
	var finishReason = genai.FinishReasonStop

	for stream.Next() {
		event := stream.Current()
		switch v := event.AsAny().(type) {
		case responses.ResponseTextDeltaEvent:
			if v.Delta == "" {
				continue
			}
			aggregatedText.WriteString(v.Delta)
			if !yield(&model.LLMResponse{
				Partial:      true,
				TurnComplete: false,
				Content: &genai.Content{
					Role:  string(genai.RoleModel),
					Parts: []*genai.Part{{Text: v.Delta}},
				},
			}, nil) {
				return
			}
		case responses.ResponseOutputItemDoneEvent:
			if fc, ok := v.Item.AsAny().(responses.ResponseFunctionToolCall); ok {
				var args map[string]any
				if fc.Arguments != "" {
					_ = json.Unmarshal([]byte(fc.Arguments), &args)
				}
				callID := fc.CallID
				if callID == "" {
					callID = fc.ID
				}
				if _, seen := toolCalls[callID]; !seen {
					toolCallOrder = append(toolCallOrder, callID)
				}
				toolCalls[callID] = newFunctionCallPart(fc.Name, args, callID, nil)
			}
		case responses.ResponseCompletedEvent:
			usage = responsesUsageToGenai(v.Response.Usage)
			finishReason = responsesStatusToFinishReason(v.Response.Status)
		case responses.ResponseIncompleteEvent:
			usage = responsesUsageToGenai(v.Response.Usage)
			finishReason = genai.FinishReasonMaxTokens
		}
	}

	if err := stream.Err(); err != nil {
		if ctx.Err() == context.Canceled {
			return
		}
		_ = yield(&model.LLMResponse{ErrorCode: "STREAM_ERROR", ErrorMessage: err.Error()}, nil)
		return
	}

	finalParts := make([]*genai.Part, 0, 1+len(toolCallOrder))
	if text := aggregatedText.String(); text != "" {
		finalParts = append(finalParts, &genai.Part{Text: text})
	}
	for _, id := range toolCallOrder {
		finalParts = append(finalParts, toolCalls[id])
	}

	out := &model.LLMResponse{
		Partial:       false,
		TurnComplete:  true,
		FinishReason:  finishReason,
		UsageMetadata: usage,
		Content:       &genai.Content{Role: string(genai.RoleModel), Parts: finalParts},
	}
	telemetry.SetLLMResponseAttributes(ctx, out)
	_ = yield(out, nil)
}

func responseToLLMResponse(resp *responses.Response) *model.LLMResponse {
	parts := make([]*genai.Part, 0, len(resp.Output))
	for _, item := range resp.Output {
		switch v := item.AsAny().(type) {
		case responses.ResponseOutputMessage:
			for _, c := range v.Content {
				if ot, ok := c.AsAny().(responses.ResponseOutputText); ok && ot.Text != "" {
					parts = append(parts, &genai.Part{Text: ot.Text})
				}
			}
		case responses.ResponseFunctionToolCall:
			var args map[string]any
			if v.Arguments != "" {
				_ = json.Unmarshal([]byte(v.Arguments), &args)
			}
			callID := v.CallID
			if callID == "" {
				callID = v.ID
			}
			parts = append(parts, newFunctionCallPart(v.Name, args, callID, nil))
		}
	}

	return &model.LLMResponse{
		Partial:       false,
		TurnComplete:  true,
		FinishReason:  responsesStatusToFinishReason(resp.Status),
		UsageMetadata: responsesUsageToGenai(resp.Usage),
		Content:       &genai.Content{Role: string(genai.RoleModel), Parts: parts},
	}
}

func responsesUsageToGenai(u responses.ResponseUsage) *genai.GenerateContentResponseUsageMetadata {
	if u.InputTokens == 0 && u.OutputTokens == 0 {
		return nil
	}
	return &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:     int32(u.InputTokens),
		CandidatesTokenCount: int32(u.OutputTokens),
	}
}

func responsesStatusToFinishReason(status responses.ResponseStatus) genai.FinishReason {
	switch status {
	case responses.ResponseStatusIncomplete:
		return genai.FinishReasonMaxTokens
	case responses.ResponseStatusFailed:
		return genai.FinishReasonOther
	default:
		return genai.FinishReasonStop
	}
}
