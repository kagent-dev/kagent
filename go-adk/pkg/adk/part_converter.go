package adk

import (
	"encoding/base64"
	"fmt"

	"github.com/kagent-dev/kagent/go-adk/pkg/core"
	"google.golang.org/genai"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// GenAIPartStructToMap converts *genai.Part to the map shape expected by ConvertGenAIPartToA2APart.
// Used when converting *adksession.Event to A2A (like Python: convert_genai_part_to_a2a_part(part)).
func GenAIPartStructToMap(part *genai.Part) map[string]interface{} {
	if part == nil {
		return nil
	}
	m := make(map[string]interface{})
	if part.Text != "" {
		m[core.PartKeyText] = part.Text
		if part.Thought {
			m["thought"] = true
		}
	}
	if part.FileData != nil {
		m[core.PartKeyFileData] = map[string]interface{}{
			core.PartKeyFileURI:  part.FileData.FileURI,
			core.PartKeyMimeType: part.FileData.MIMEType,
		}
	}
	if part.InlineData != nil {
		m[core.PartKeyInlineData] = map[string]interface{}{
			"data":               part.InlineData.Data,
			core.PartKeyMimeType: part.InlineData.MIMEType,
		}
	}
	if part.FunctionCall != nil {
		fc := map[string]interface{}{
			core.PartKeyName: part.FunctionCall.Name,
			core.PartKeyArgs: part.FunctionCall.Args,
		}
		if part.FunctionCall.ID != "" {
			fc[core.PartKeyID] = part.FunctionCall.ID
		}
		m[core.PartKeyFunctionCall] = fc
	}
	if part.FunctionResponse != nil {
		fr := map[string]interface{}{
			core.PartKeyName:     part.FunctionResponse.Name,
			core.PartKeyResponse: part.FunctionResponse.Response,
		}
		if part.FunctionResponse.ID != "" {
			fr[core.PartKeyID] = part.FunctionResponse.ID
		}
		m[core.PartKeyFunctionResponse] = fr
	}
	if len(m) == 0 {
		return nil
	}
	return m
}

// GenAIPartToA2APart converts *genai.Part to A2A protocol.Part (single layer: GenAI → A2A).
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
func ConvertGenAIPartToA2APart(genaiPart map[string]interface{}) (protocol.Part, error) {
	// Handle text parts (matching Python: if part.text)
	if text, ok := genaiPart[core.PartKeyText].(string); ok {
		// thought metadata (part.thought) can be added when A2A protocol supports it
		return protocol.NewTextPart(text), nil
	}

	// Handle file_data parts (matching Python: if part.file_data)
	if fileData, ok := genaiPart[core.PartKeyFileData].(map[string]interface{}); ok {
		if uri, ok := fileData[core.PartKeyFileURI].(string); ok {
			mimeType, _ := fileData[core.PartKeyMimeType].(string)
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
	if inlineData, ok := genaiPart[core.PartKeyInlineData].(map[string]interface{}); ok {
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
			mimeType, _ := inlineData[core.PartKeyMimeType].(string)
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
	if functionCall, ok := genaiPart[core.PartKeyFunctionCall].(map[string]interface{}); ok {
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
	if functionResponse, ok := genaiPart[core.PartKeyFunctionResponse].(map[string]interface{}); ok {
		cleanedResponse := make(map[string]interface{})
		for k, v := range functionResponse {
			if v != nil {
				cleanedResponse[k] = v
			}
		}
		// Normalize response so UI gets response.result (ToolResponseData). MCP/GenAI often use
		// "content" (array or string) or raw map; UI expects response.result for display.
		if resp, ok := cleanedResponse[core.PartKeyResponse].(map[string]interface{}); ok {
			normalized := normalizeFunctionResponseForUI(resp)
			cleanedResponse[core.PartKeyResponse] = normalized
		} else if respStr, ok := cleanedResponse[core.PartKeyResponse].(string); ok {
			cleanedResponse[core.PartKeyResponse] = map[string]interface{}{"result": respStr}
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
