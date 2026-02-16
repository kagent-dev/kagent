package converter

import (
	"encoding/base64"
	"fmt"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/a2a"
	"google.golang.org/genai"
)

// newDataPart creates a DataPart with the given data and metadata type.
func newDataPart(data map[string]interface{}, partType string) *a2atype.DataPart {
	return &a2atype.DataPart{
		Data:     data,
		Metadata: map[string]interface{}{a2a.MetadataKeyType: partType},
	}
}

// GenAIPartToA2APart converts *genai.Part directly to A2A protocol Part.
// This is the primary conversion function - no intermediate map representation.
func GenAIPartToA2APart(part *genai.Part) (a2atype.Part, error) {
	if part == nil {
		return nil, fmt.Errorf("part is nil")
	}

	// Handle text parts
	if part.Text != "" {
		// thought metadata (part.thought) can be added when A2A protocol supports it
		return a2atype.TextPart{Text: part.Text}, nil
	}

	// Handle file_data parts
	if part.FileData != nil {
		mimeType := part.FileData.MIMEType
		return a2atype.FilePart{
			File: a2atype.FileURI{
				URI:      part.FileData.FileURI,
				FileMeta: a2atype.FileMeta{MimeType: mimeType},
			},
		}, nil
	}

	// Handle inline_data parts
	if part.InlineData != nil && len(part.InlineData.Data) > 0 {
		mimeType := part.InlineData.MIMEType
		return a2atype.FilePart{
			File: a2atype.FileBytes{
				Bytes:    base64.StdEncoding.EncodeToString(part.InlineData.Data),
				FileMeta: a2atype.FileMeta{MimeType: mimeType},
			},
		}, nil
	}

	// Handle function_call parts
	if part.FunctionCall != nil {
		data := map[string]interface{}{
			a2a.PartKeyName: part.FunctionCall.Name,
			a2a.PartKeyArgs: part.FunctionCall.Args,
		}
		if part.FunctionCall.ID != "" {
			data[a2a.PartKeyID] = part.FunctionCall.ID
		}
		return newDataPart(data, a2a.A2ADataPartMetadataTypeFunctionCall), nil
	}

	// Handle function_response parts
	if part.FunctionResponse != nil {
		response := normalizeFunctionResponse(part.FunctionResponse.Response)
		data := map[string]interface{}{
			a2a.PartKeyName:     part.FunctionResponse.Name,
			a2a.PartKeyResponse: response,
		}
		if part.FunctionResponse.ID != "" {
			data[a2a.PartKeyID] = part.FunctionResponse.ID
		}
		return newDataPart(data, a2a.A2ADataPartMetadataTypeFunctionResponse), nil
	}

	// Handle code_execution_result parts
	if part.CodeExecutionResult != nil {
		data := map[string]interface{}{
			a2a.PartKeyOutcome: string(part.CodeExecutionResult.Outcome),
			a2a.PartKeyOutput:  part.CodeExecutionResult.Output,
		}
		return newDataPart(data, a2a.A2ADataPartMetadataTypeCodeExecutionResult), nil
	}

	// Handle executable_code parts
	if part.ExecutableCode != nil {
		data := map[string]interface{}{
			a2a.PartKeyCode:     part.ExecutableCode.Code,
			a2a.PartKeyLanguage: string(part.ExecutableCode.Language),
		}
		return newDataPart(data, a2a.A2ADataPartMetadataTypeExecutableCode), nil
	}

	return nil, fmt.Errorf("part has no recognized content")
}

// normalizeFunctionResponse ensures the response has a "result" field the UI expects.
// Handles map[string]interface{} responses from GenAI.
func normalizeFunctionResponse(resp map[string]interface{}) map[string]interface{} {
	if resp == nil {
		return map[string]interface{}{"result": nil}
	}

	out := make(map[string]interface{})
	for k, v := range resp {
		if v != nil {
			out[k] = v
		}
	}

	// Already has result field
	if _, hasResult := out["result"]; hasResult {
		return out
	}

	// Handle error responses
	if errStr, ok := out["error"].(string); ok && errStr != "" {
		out["isError"] = true
		out["result"] = map[string]interface{}{"error": errStr}
		return out
	}

	// Handle content field (string or array)
	if contentStr, ok := out["content"].(string); ok {
		out["result"] = map[string]interface{}{"content": contentStr}
		return out
	}
	if contentArr, ok := out["content"].([]interface{}); ok && len(contentArr) > 0 {
		out["result"] = map[string]interface{}{"content": contentArr}
		return out
	}

	// Fallback: set result to the response object
	out["result"] = resp
	return out
}
