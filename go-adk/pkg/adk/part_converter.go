package adk

import (
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/kagent-dev/kagent/go-adk/pkg/core"
	"google.golang.org/genai"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// ConvertA2APartToGenAIPart converts an A2A Part to a GenAI Part (placeholder for now)
// In a full implementation, this would convert to Google GenAI types
func ConvertA2APartToGenAIPart(a2aPart protocol.Part) (map[string]interface{}, error) {
	result := make(map[string]interface{})

	switch part := a2aPart.(type) {
	case *protocol.TextPart:
		result[PartKeyText] = part.Text
		return result, nil

	case *protocol.FilePart:
		if part.File != nil {
			if uriFile, ok := part.File.(*protocol.FileWithURI); ok {
				mimeType := ""
				if uriFile.MimeType != nil {
					mimeType = *uriFile.MimeType
				}
				result[PartKeyFileData] = map[string]interface{}{
					PartKeyFileURI:  uriFile.URI,
					PartKeyMimeType: mimeType,
				}
				return result, nil
			}
			if bytesFile, ok := part.File.(*protocol.FileWithBytes); ok {
				data, err := base64.StdEncoding.DecodeString(bytesFile.Bytes)
				if err != nil {
					return nil, fmt.Errorf("failed to decode base64 file data: %w", err)
				}
				mimeType := ""
				if bytesFile.MimeType != nil {
					mimeType = *bytesFile.MimeType
				}
				result[PartKeyInlineData] = map[string]interface{}{
					"data":           data,
					PartKeyMimeType: mimeType,
				}
				return result, nil
			}
		}
		return nil, fmt.Errorf("unsupported file part type")

	case *protocol.DataPart:
		// Check metadata for special types
		if part.Metadata != nil {
			if partType, ok := part.Metadata[core.GetKAgentMetadataKey(core.A2ADataPartMetadataTypeKey)].(string); ok {
				switch partType {
				case core.A2ADataPartMetadataTypeFunctionCall:
					result[PartKeyFunctionCall] = part.Data
					return result, nil
				case core.A2ADataPartMetadataTypeFunctionResponse:
					result[PartKeyFunctionResponse] = part.Data
					return result, nil
				case core.A2ADataPartMetadataTypeCodeExecutionResult:
					result["code_execution_result"] = part.Data
					return result, nil
				case core.A2ADataPartMetadataTypeExecutableCode:
					result["executable_code"] = part.Data
					return result, nil
				}
			}
		}
		// Default: convert to JSON text
		dataJSON, err := json.Marshal(part.Data)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal data part: %w", err)
		}
		result[PartKeyText] = string(dataJSON)
		return result, nil

	default:
		return nil, fmt.Errorf("unsupported part type: %T", a2aPart)
	}
}

// GenAIPartStructToMap converts *genai.Part to the map shape expected by ConvertGenAIPartToA2APart.
// Used when converting *adksession.Event to A2A (like Python: convert_genai_part_to_a2a_part(part)).
func GenAIPartStructToMap(part *genai.Part) map[string]interface{} {
	if part == nil {
		return nil
	}
	m := make(map[string]interface{})
	if part.Text != "" {
		m[PartKeyText] = part.Text
		if part.Thought {
			m["thought"] = true
		}
	}
	if part.FileData != nil {
		m[PartKeyFileData] = map[string]interface{}{
			PartKeyFileURI:  part.FileData.FileURI,
			PartKeyMimeType: part.FileData.MIMEType,
		}
	}
	if part.InlineData != nil {
		m[PartKeyInlineData] = map[string]interface{}{
			"data":           part.InlineData.Data,
			PartKeyMimeType: part.InlineData.MIMEType,
		}
	}
	if part.FunctionCall != nil {
		fc := map[string]interface{}{
			PartKeyName: part.FunctionCall.Name,
			PartKeyArgs: part.FunctionCall.Args,
		}
		if part.FunctionCall.ID != "" {
			fc[PartKeyID] = part.FunctionCall.ID
		}
		m[PartKeyFunctionCall] = fc
	}
	if part.FunctionResponse != nil {
		fr := map[string]interface{}{
			PartKeyName:     part.FunctionResponse.Name,
			PartKeyResponse: part.FunctionResponse.Response,
		}
		if part.FunctionResponse.ID != "" {
			fr[PartKeyID] = part.FunctionResponse.ID
		}
		m[PartKeyFunctionResponse] = fr
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// ConvertGenAIPartToA2APart converts a GenAI Part to an A2A Part
// This matches Python's convert_genai_part_to_a2a_part function
func ConvertGenAIPartToA2APart(genaiPart map[string]interface{}) (protocol.Part, error) {
	// Handle text parts (matching Python: if part.text)
	if text, ok := genaiPart[PartKeyText].(string); ok {
		// TODO: Handle thought metadata if present (part.thought)
		return protocol.NewTextPart(text), nil
	}

	// Handle file_data parts (matching Python: if part.file_data)
	if fileData, ok := genaiPart[PartKeyFileData].(map[string]interface{}); ok {
		if uri, ok := fileData[PartKeyFileURI].(string); ok {
			mimeType, _ := fileData[PartKeyMimeType].(string)
			return &protocol.FilePart{
				Kind: "file",
				File: &protocol.FileWithURI{
					URI:      uri,
					MimeType: &mimeType,
				},
			}, nil
		}
	}

	// Handle inline_data parts (matching Python: if part.inline_data)
	if inlineData, ok := genaiPart[PartKeyInlineData].(map[string]interface{}); ok {
		var data []byte
		var err error

		// Handle different data types
		if dataBytes, ok := inlineData["data"].([]byte); ok {
			data = dataBytes
		} else if dataStr, ok := inlineData["data"].(string); ok {
			// Try to decode base64 if it's a string
			data, err = base64.StdEncoding.DecodeString(dataStr)
			if err != nil {
				// If not base64, use as-is
				data = []byte(dataStr)
			}
		}

		if len(data) > 0 {
			mimeType, _ := inlineData[PartKeyMimeType].(string)
			// TODO: Handle video_metadata if present
			return &protocol.FilePart{
				Kind: "file",
				File: &protocol.FileWithBytes{
					Bytes:    base64.StdEncoding.EncodeToString(data),
					MimeType: &mimeType,
				},
			}, nil
		}
	}

	// Handle function_call parts (matching Python: if part.function_call)
	if functionCall, ok := genaiPart[PartKeyFunctionCall].(map[string]interface{}); ok {
		// Marshal to ensure proper format (matching Python: model_dump(by_alias=True, exclude_none=True))
		cleanedCall := make(map[string]interface{})
		for k, v := range functionCall {
			if v != nil {
				cleanedCall[k] = v
			}
		}
		return &protocol.DataPart{
			Kind: "data",
			Data: cleanedCall,
			Metadata: map[string]interface{}{
				core.GetKAgentMetadataKey(core.A2ADataPartMetadataTypeKey): core.A2ADataPartMetadataTypeFunctionCall,
			},
		}, nil
	}

	// Handle function_response parts (matching Python: if part.function_response)
	if functionResponse, ok := genaiPart[PartKeyFunctionResponse].(map[string]interface{}); ok {
		cleanedResponse := make(map[string]interface{})
		for k, v := range functionResponse {
			if v != nil {
				cleanedResponse[k] = v
			}
		}
		// Normalize response so UI gets response.result (ToolResponseData). MCP/GenAI often use
		// "content" (array or string) or raw map; UI expects response.result for display.
		if resp, ok := cleanedResponse[PartKeyResponse].(map[string]interface{}); ok {
			normalized := normalizeFunctionResponseForUI(resp)
			cleanedResponse[PartKeyResponse] = normalized
		} else if respStr, ok := cleanedResponse[PartKeyResponse].(string); ok {
			cleanedResponse[PartKeyResponse] = map[string]interface{}{"result": respStr}
		}
		return &protocol.DataPart{
			Kind: "data",
			Data: cleanedResponse,
			Metadata: map[string]interface{}{
				core.GetKAgentMetadataKey(core.A2ADataPartMetadataTypeKey): core.A2ADataPartMetadataTypeFunctionResponse,
			},
		}, nil
	}

	// Handle code_execution_result parts (matching Python: if part.code_execution_result)
	if codeExecutionResult, ok := genaiPart["code_execution_result"].(map[string]interface{}); ok {
		cleanedResult := make(map[string]interface{})
		for k, v := range codeExecutionResult {
			if v != nil {
				cleanedResult[k] = v
			}
		}
		return &protocol.DataPart{
			Kind: "data",
			Data: cleanedResult,
			Metadata: map[string]interface{}{
				core.GetKAgentMetadataKey(core.A2ADataPartMetadataTypeKey): core.A2ADataPartMetadataTypeCodeExecutionResult,
			},
		}, nil
	}

	// Handle executable_code parts (matching Python: if part.executable_code)
	if executableCode, ok := genaiPart["executable_code"].(map[string]interface{}); ok {
		cleanedCode := make(map[string]interface{})
		for k, v := range executableCode {
			if v != nil {
				cleanedCode[k] = v
			}
		}
		return &protocol.DataPart{
			Kind: "data",
			Data: cleanedCode,
			Metadata: map[string]interface{}{
				core.GetKAgentMetadataKey(core.A2ADataPartMetadataTypeKey): core.A2ADataPartMetadataTypeExecutableCode,
			},
		}, nil
	}

	return nil, fmt.Errorf("unsupported genai part type: %v", genaiPart)
}

// normalizeFunctionResponseForUI ensures the response map has a "result" field the UI expects
// (ToolResponseData.response.result). Aligns with Python packages: report response as JSON (object),
// not string — e.g. kagent-openai uses "response": {"result": actual_output}, kagent-adk uses
// model_dump (full object), kagent-langgraph uses "response": message.content (object or string).
func normalizeFunctionResponseForUI(resp map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{})
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
		out["result"] = map[string]interface{}{"error": errStr}
		return out
	}
	if contentStr, ok := out["content"].(string); ok {
		out["result"] = map[string]interface{}{"content": contentStr}
		return out
	}
	if contentArr, ok := out["content"].([]interface{}); ok && len(contentArr) > 0 {
		out["result"] = map[string]interface{}{"content": contentArr}
		return out
	}
	// Fallback: set result to the response object (JSON), matching Python model_dump / message.content
	out["result"] = resp
	return out
}
