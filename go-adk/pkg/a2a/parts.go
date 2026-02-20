package a2a

import (
	"encoding/base64"
	"fmt"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"google.golang.org/genai"
)

// Part/map keys for GenAI-style content.
const (
	PartKeyName     = "name"
	PartKeyArgs     = "args"
	PartKeyResponse = "response"
	PartKeyID       = "id"
	PartKeyOutcome  = "outcome"
	PartKeyOutput   = "output"
	PartKeyCode     = "code"
	PartKeyLanguage = "language"
)

// newDataPart creates a DataPart with the given data and metadata type.
func newDataPart(data map[string]any, partType string) *a2atype.DataPart {
	return &a2atype.DataPart{
		Data: data,
		Metadata: map[string]any{
			GetKAgentMetadataKey(A2ADataPartMetadataTypeKey): partType,
		},
	}
}

// GenAIPartToA2APart converts *genai.Part directly to A2A protocol Part.
func GenAIPartToA2APart(part *genai.Part) (a2atype.Part, error) {
	if part == nil {
		return nil, fmt.Errorf("part is nil")
	}

	if part.Text != "" {
		return a2atype.TextPart{Text: part.Text}, nil
	}

	if part.FileData != nil {
		return a2atype.FilePart{
			File: a2atype.FileURI{
				URI:      part.FileData.FileURI,
				FileMeta: a2atype.FileMeta{MimeType: part.FileData.MIMEType},
			},
		}, nil
	}

	if part.InlineData != nil && len(part.InlineData.Data) > 0 {
		return a2atype.FilePart{
			File: a2atype.FileBytes{
				Bytes:    base64.StdEncoding.EncodeToString(part.InlineData.Data),
				FileMeta: a2atype.FileMeta{MimeType: part.InlineData.MIMEType},
			},
		}, nil
	}

	if part.FunctionCall != nil {
		data := map[string]any{
			PartKeyName: part.FunctionCall.Name,
			PartKeyArgs: part.FunctionCall.Args,
		}
		if part.FunctionCall.ID != "" {
			data[PartKeyID] = part.FunctionCall.ID
		}
		return newDataPart(data, A2ADataPartMetadataTypeFunctionCall), nil
	}

	if part.FunctionResponse != nil {
		response := normalizeFunctionResponse(part.FunctionResponse.Response)
		data := map[string]any{
			PartKeyName:     part.FunctionResponse.Name,
			PartKeyResponse: response,
		}
		if part.FunctionResponse.ID != "" {
			data[PartKeyID] = part.FunctionResponse.ID
		}
		return newDataPart(data, A2ADataPartMetadataTypeFunctionResponse), nil
	}

	if part.CodeExecutionResult != nil {
		data := map[string]any{
			PartKeyOutcome: string(part.CodeExecutionResult.Outcome),
			PartKeyOutput:  part.CodeExecutionResult.Output,
		}
		return newDataPart(data, A2ADataPartMetadataTypeCodeExecutionResult), nil
	}

	if part.ExecutableCode != nil {
		data := map[string]any{
			PartKeyCode:     part.ExecutableCode.Code,
			PartKeyLanguage: string(part.ExecutableCode.Language),
		}
		return newDataPart(data, A2ADataPartMetadataTypeExecutableCode), nil
	}

	return nil, fmt.Errorf("part has no recognized content")
}

// normalizeFunctionResponse ensures the response has a "result" field the UI expects.
func normalizeFunctionResponse(resp map[string]any) map[string]any {
	if resp == nil {
		return map[string]any{"result": nil}
	}

	out := make(map[string]any)
	for k, v := range resp {
		if v != nil {
			out[k] = v
		}
	}

	if _, hasResult := out["result"]; hasResult {
		return out
	}
	if errStr, ok := out["error"].(string); ok && errStr != "" {
		out["isError"] = true
		out["result"] = map[string]any{"error": errStr}
		return out
	}
	if contentStr, ok := out["content"].(string); ok {
		out["result"] = map[string]any{"content": contentStr}
		return out
	}
	if contentArr, ok := out["content"].([]any); ok && len(contentArr) > 0 {
		out["result"] = map[string]any{"content": contentArr}
		return out
	}
	out["result"] = resp
	return out
}
