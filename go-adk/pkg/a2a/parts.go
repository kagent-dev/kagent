package a2a

import (
	"encoding/base64"
	"fmt"

	"google.golang.org/genai"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// Part/map keys for GenAI-style content (parts, function_call, function_response, file_data, etc.).
const (
	PartKeyText             = "text"
	PartKeyParts            = "parts"
	PartKeyRole             = "role"
	PartKeyFunctionCall     = "function_call"
	PartKeyFunctionResponse = "function_response"
	PartKeyFileData         = "file_data"
	PartKeyInlineData       = "inline_data"
	PartKeyFileURI          = "file_uri"
	PartKeyMimeType         = "mime_type"
	PartKeyName             = "name"
	PartKeyArgs             = "args"
	PartKeyResponse         = "response"
	PartKeyID               = "id"
)

// GenAIPartStructToMap converts *genai.Part to the map shape expected by ConvertGenAIPartToA2APart.
// Used when converting *adksession.Event to A2A (like Python: convert_genai_part_to_a2a_part(part)).
func GenAIPartStructToMap(part *genai.Part) map[string]any {
	if part == nil {
		return nil
	}
	m := make(map[string]any)
	if part.Text != "" {
		m[PartKeyText] = part.Text
		if part.Thought {
			m["thought"] = true
		}
	}
	if part.FileData != nil {
		m[PartKeyFileData] = map[string]any{
			PartKeyFileURI:  part.FileData.FileURI,
			PartKeyMimeType: part.FileData.MIMEType,
		}
	}
	if part.InlineData != nil {
		m[PartKeyInlineData] = map[string]any{
			"data":          part.InlineData.Data,
			PartKeyMimeType: part.InlineData.MIMEType,
		}
	}
	if part.FunctionCall != nil {
		fc := map[string]any{
			PartKeyName: part.FunctionCall.Name,
			PartKeyArgs: part.FunctionCall.Args,
		}
		if part.FunctionCall.ID != "" {
			fc[PartKeyID] = part.FunctionCall.ID
		}
		m[PartKeyFunctionCall] = fc
	}
	if part.FunctionResponse != nil {
		fr := map[string]any{
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

// GenAIPartToA2APart converts *genai.Part to A2A protocol.Part (single layer: GenAI -> A2A).
func GenAIPartToA2APart(part *genai.Part) (protocol.Part, error) {
	if part == nil {
		return nil, fmt.Errorf("part is nil")
	}
	m := GenAIPartStructToMap(part)
	if m == nil {
		return nil, fmt.Errorf("part has no content")
	}
	return ConvertGenAIPartToA2APart(m)
}

// ConvertGenAIPartToA2APart converts a GenAI Part (as map) to an A2A Part.
// This matches Python's convert_genai_part_to_a2a_part function.
func ConvertGenAIPartToA2APart(genaiPart map[string]any) (protocol.Part, error) {
	// Handle text parts (matching Python: if part.text)
	if text, ok := genaiPart[PartKeyText].(string); ok {
		// thought metadata (part.thought) can be added when A2A protocol supports it
		return protocol.NewTextPart(text), nil
	}

	// Handle file_data parts (matching Python: if part.file_data)
	if fileData, ok := genaiPart[PartKeyFileData].(map[string]any); ok {
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
	if inlineData, ok := genaiPart[PartKeyInlineData].(map[string]any); ok {
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
			// video_metadata can be added when A2A protocol supports it
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
	if functionCall, ok := genaiPart[PartKeyFunctionCall].(map[string]any); ok {
		// Marshal to ensure proper format (matching Python: model_dump(by_alias=True, exclude_none=True))
		cleanedCall := make(map[string]any)
		for k, v := range functionCall {
			if v != nil {
				cleanedCall[k] = v
			}
		}
		return &protocol.DataPart{
			Kind: "data",
			Data: cleanedCall,
			Metadata: map[string]any{
				GetKAgentMetadataKey(A2ADataPartMetadataTypeKey): A2ADataPartMetadataTypeFunctionCall,
			},
		}, nil
	}

	// Handle function_response parts (matching Python: if part.function_response)
	if functionResponse, ok := genaiPart[PartKeyFunctionResponse].(map[string]any); ok {
		cleanedResponse := make(map[string]any)
		for k, v := range functionResponse {
			if v != nil {
				cleanedResponse[k] = v
			}
		}
		// Normalize response so UI gets response.result (ToolResponseData). MCP/GenAI often use
		// "content" (array or string) or raw map; UI expects response.result for display.
		if resp, ok := cleanedResponse[PartKeyResponse].(map[string]any); ok {
			normalized := normalizeFunctionResponseForUI(resp)
			cleanedResponse[PartKeyResponse] = normalized
		} else if respStr, ok := cleanedResponse[PartKeyResponse].(string); ok {
			cleanedResponse[PartKeyResponse] = map[string]any{"result": respStr}
		}
		return &protocol.DataPart{
			Kind: "data",
			Data: cleanedResponse,
			Metadata: map[string]any{
				GetKAgentMetadataKey(A2ADataPartMetadataTypeKey): A2ADataPartMetadataTypeFunctionResponse,
			},
		}, nil
	}

	// Handle code_execution_result parts (matching Python: if part.code_execution_result)
	if codeExecutionResult, ok := genaiPart["code_execution_result"].(map[string]any); ok {
		cleanedResult := make(map[string]any)
		for k, v := range codeExecutionResult {
			if v != nil {
				cleanedResult[k] = v
			}
		}
		return &protocol.DataPart{
			Kind: "data",
			Data: cleanedResult,
			Metadata: map[string]any{
				GetKAgentMetadataKey(A2ADataPartMetadataTypeKey): A2ADataPartMetadataTypeCodeExecutionResult,
			},
		}, nil
	}

	// Handle executable_code parts (matching Python: if part.executable_code)
	if executableCode, ok := genaiPart["executable_code"].(map[string]any); ok {
		cleanedCode := make(map[string]any)
		for k, v := range executableCode {
			if v != nil {
				cleanedCode[k] = v
			}
		}
		return &protocol.DataPart{
			Kind: "data",
			Data: cleanedCode,
			Metadata: map[string]any{
				GetKAgentMetadataKey(A2ADataPartMetadataTypeKey): A2ADataPartMetadataTypeExecutableCode,
			},
		}, nil
	}

	return nil, fmt.Errorf("unsupported genai part type: %v", genaiPart)
}

// normalizeFunctionResponseForUI ensures the response map has a "result" field the UI expects
// (ToolResponseData.response.result). Aligns with Python packages: report response as JSON (object),
// not string.
func normalizeFunctionResponseForUI(resp map[string]any) map[string]any {
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
	// Fallback: set result to the response object (JSON), matching Python model_dump / message.content
	out["result"] = resp
	return out
}
