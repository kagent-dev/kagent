// Package models: OpenAI Responses API path for OpenAIModel.
package models

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"mime"
	"net/http"
	"strings"

	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
	"google.golang.org/adk/v2/model"
	"google.golang.org/genai"

	"github.com/kagent-dev/kagent/go/adk/pkg/fileextract"
)

func generateContentResponses(
	ctx context.Context,
	m *OpenAIModel,
	req *model.LLMRequest,
	modelName string,
	stream bool,
	yield func(*model.LLMResponse, error) bool,
) {
	input, instructions, err := genaiContentsToResponsesInput(req.Contents, req.Config)
	if err != nil {
		yield(nil, err)
		return
	}
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
	if schema, err := structuredOutputSchema(req.Config); err != nil {
		yield(nil, err)
		return
	} else if schema != nil {
		format := responses.ResponseFormatTextConfigParamOfJSONSchema(structuredOutputName, schema)
		params.Text.Format = format
	}

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
		params.MaxOutputTokens = param.NewOpt(*cfg.MaxCompletionTokens)
	} else if cfg.MaxTokens != nil {
		params.MaxOutputTokens = param.NewOpt(*cfg.MaxTokens)
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

const linkRule = "this provider integration can only pass http and https links"

func genaiContentsToResponsesInput(contents []*genai.Content, config *genai.GenerateContentConfig) (responses.ResponseInputParam, string, error) {
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

	// The size of this file and every newer one: while it is over the cap, the file
	// becomes a note, so the newest files are the ones sent.
	fileBytes := requestFileBytes(contents)
	var input responses.ResponseInputParam
	for _, content := range contents {
		if content == nil || strings.TrimSpace(content.Role) == openAIRoleSystem {
			continue
		}
		role := strings.TrimSpace(content.Role)
		var textParts []string
		var functionCalls []*genai.FunctionCall
		// contentParts keeps text and file parts in order; used once a file part exists.
		var contentParts responses.ResponseInputMessageContentListParam
		hasFile := false

		for _, part := range content.Parts {
			if part == nil {
				continue
			}
			if part.Text != "" {
				textParts = append(textParts, part.Text)
				contentParts = append(contentParts, responses.ResponseInputContentParamOfInputText(part.Text))
			} else if part.FunctionCall != nil {
				functionCalls = append(functionCalls, part.FunctionCall)
			} else if part.InlineData != nil && strings.HasPrefix(part.InlineData.MIMEType, "image/") {
				hasFile = true
				contentParts = append(contentParts, responsesInputImage(dataURL(part.InlineData.MIMEType, part.InlineData.Data)))
			} else if fileType := responsesFileType(part); fileType != "" && role == genai.RoleUser && fileBytes > maxRequestFileBytes {
				fileBytes -= len(part.InlineData.Data)
				note := overFileCapNote(part.InlineData.DisplayName)
				textParts = append(textParts, note)
				contentParts = append(contentParts, responses.ResponseInputContentParamOfInputText(note))
			} else if fileType != "" && role == genai.RoleUser {
				hasFile = true
				contentParts = append(contentParts, fileNameParts(part.InlineData.DisplayName)...)
				file := responses.ResponseInputFileParam{
					FileData: param.NewOpt(dataURL(fileType, part.InlineData.Data)),
					Filename: param.NewOpt(cmp.Or(part.InlineData.DisplayName, "file")),
				}
				contentParts = append(contentParts, responses.ResponseInputContentUnionParam{OfInputFile: &file})
			} else if part.FileData != nil && role == genai.RoleUser {
				// The provider fetches the link itself. fileextract makes links in older turns text.
				switch {
				case !isHTTPLink(part.FileData.FileURI):
					return nil, "", unsupportedLinkError(part.FileData)
				case strings.HasPrefix(part.FileData.MIMEType, "image/"):
					hasFile = true
					contentParts = append(contentParts, fileNameParts(part.FileData.DisplayName)...)
					contentParts = append(contentParts, responsesInputImage(part.FileData.FileURI))
				default:
					hasFile = true
					contentParts = append(contentParts, fileNameParts(part.FileData.DisplayName)...)
					file := responses.ResponseInputFileParam{FileURL: param.NewOpt(part.FileData.FileURI)}
					contentParts = append(contentParts, responses.ResponseInputContentUnionParam{OfInputFile: &file})
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
				argsJSON, _ := json.Marshal(nonNilFunctionCallArgs(fc.Args))
				input = append(input, responses.ResponseInputItemParamOfFunctionCall(
					string(argsJSON),
					fc.ID,
					fc.Name,
				))
				output := "No response available for this function call."
				if fr := functionResponses[fc.ID]; fr != nil {
					output = extractFunctionResponseContent(fr.Response)
				}
				functionOutput := responses.ResponseInputItemParamOfFunctionCallOutput(output)
				functionOutput.OfFunctionCallOutput.CallID = param.NewOpt(fc.ID)
				input = append(input, functionOutput)
			}
			continue
		}

		if len(textParts) == 0 && !hasFile {
			continue
		}

		msgRole := responses.EasyInputMessageRoleUser
		if role == openAIRoleModel || role == openAIRoleAssistant {
			msgRole = responses.EasyInputMessageRoleAssistant
		}

		if hasFile {
			input = append(input, responses.ResponseInputItemParamOfMessage(contentParts, msgRole))
		} else {
			input = append(input, responses.ResponseInputItemParamOfMessage(strings.Join(textParts, "\n"), msgRole))
		}
	}
	return input, instructions, nil
}

// fileNameParts names a file in a text part before it: the model does not see
// input_file's filename, and neither input_file nor input_image takes a name next
// to a URL (Azure rejects filename beside file_url).
func fileNameParts(name string) responses.ResponseInputMessageContentListParam {
	if name == "" {
		return nil
	}
	return responses.ResponseInputMessageContentListParam{responses.ResponseInputContentParamOfInputText("File: " + name)}
}

// responsesInputImage builds an input_image part from a data or https URL.
func responsesInputImage(url string) responses.ResponseInputContentUnionParam {
	img := responses.ResponseInputContentParamOfInputImage(responses.ResponseInputImageDetailAuto)
	img.OfInputImage.ImageURL = param.NewOpt(url)
	return img
}

func isHTTPLink(uri string) bool {
	lower := strings.ToLower(uri)
	return strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://")
}

// maxRequestFileBytes caps the bytes of the files in one request: OpenAI takes at
// most 50 MB of files in total per request.
// https://developers.openai.com/api/docs/guides/file-inputs#usage-considerations
const maxRequestFileBytes = 50_000_000 // decimal MB: under the limit if OpenAI means MiB too

// requestFileBytes is the size of the user files that input_file gets as bytes.
func requestFileBytes(contents []*genai.Content) (n int) {
	for _, c := range contents {
		if c != nil && strings.TrimSpace(c.Role) == genai.RoleUser {
			for _, p := range c.Parts {
				if p != nil && responsesFileType(p) != "" {
					n += len(p.InlineData.Data)
				}
			}
		}
	}
	return n
}

func overFileCapNote(name string) string {
	return fmt.Sprintf("[File %q was not sent: the files in this chat are over the %d MB limit for one request.]",
		cmp.Or(name, "file"), maxRequestFileBytes/1_000_000)
}

func unsupportedLinkError(f *genai.FileData) error {
	if f.MIMEType == "" {
		return fmt.Errorf("cannot send link %q without a media type: %s", f.FileURI, linkRule)
	}
	return fmt.Errorf("cannot send link %q with media type %q: %s", f.FileURI, f.MIMEType, linkRule)
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

// responsesUsageToGenai follows the genai convention, where candidates exclude
// thoughts: the Responses API counts reasoning inside output_tokens.
func responsesUsageToGenai(u responses.ResponseUsage) *genai.GenerateContentResponseUsageMetadata {
	if u.InputTokens == 0 && u.OutputTokens == 0 {
		return nil
	}
	reasoning := u.OutputTokensDetails.ReasoningTokens
	return &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:        int32(u.InputTokens),
		CandidatesTokenCount:    int32(u.OutputTokens - reasoning),
		CachedContentTokenCount: int32(u.InputTokensDetails.CachedTokens),
		ThoughtsTokenCount:      int32(reasoning),
		TotalTokenCount:         int32(u.TotalTokens),
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

// responsesFileTypes are the document types that input_file reads from bytes
// (pdf and Office; text types stay with fileextract as plain text), each with the
// type http.DetectContentType gives its bytes: an Office file is a zip archive.
var responsesFileTypes = map[string]string{
	"application/pdf": "application/pdf",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document":   "application/zip",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet":         "application/zip",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation": "application/zip",
}

// responsesFileType is the media type of p's bytes, without parameters, when
// input_file reads them, else "". The bytes must match the type, as the provider
// rejects a file that does not; fileextract gives that file the could-not-be-read note.
func responsesFileType(p *genai.Part) string {
	if p.InlineData == nil {
		return ""
	}
	mediaType, _, _ := mime.ParseMediaType(p.InlineData.MIMEType)
	if sniffed, ok := responsesFileTypes[mediaType]; ok && http.DetectContentType(p.InlineData.Data) == sniffed {
		return mediaType
	}
	return ""
}

var _ fileextract.FileSender = (*OpenAIModel)(nil)

// ProviderAccepts implements fileextract.FileSender: the Responses API takes links,
// and pdf and Office files as bytes. Chat Completions takes neither.
func (m *OpenAIModel) ProviderAccepts(p *genai.Part) bool {
	return m.apiFormat() == OpenAIAPIFormatResponses && (p.FileData != nil || responsesFileType(p) != "")
}
