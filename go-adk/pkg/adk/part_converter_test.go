package adk

import (
	"encoding/base64"
	"testing"

	"github.com/kagent-dev/kagent/go-adk/pkg/core"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

func TestConvertA2APartToGenAIPart_TextPart(t *testing.T) {
	textPart := &protocol.TextPart{Text: "Hello, world!"}
	result, err := core.ConvertA2APartToGenAIPart(textPart)
	if err != nil {
		t.Fatalf("ConvertA2APartToGenAIPart() error = %v", err)
	}

	if text, ok := result[core.PartKeyText].(string); !ok {
		t.Errorf("Expected %q key in result, got %v", core.PartKeyText, result)
	} else if text != "Hello, world!" {
		t.Errorf("Expected text = %q, got %q", "Hello, world!", text)
	}
}

func TestConvertA2APartToGenAIPart_FilePartWithURI(t *testing.T) {
	mimeType := "image/png"
	filePart := &protocol.FilePart{
		File: &protocol.FileWithURI{
			URI:      "gs://bucket/file.png",
			MimeType: &mimeType,
		},
	}

	result, err := core.ConvertA2APartToGenAIPart(filePart)
	if err != nil {
		t.Fatalf("ConvertA2APartToGenAIPart() error = %v", err)
	}

	fileData, ok := result[core.PartKeyFileData].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected %q key in result, got %v", core.PartKeyFileData, result)
	}

	if uri, ok := fileData[core.PartKeyFileURI].(string); !ok || uri != "gs://bucket/file.png" {
		t.Errorf("Expected file_uri = %q, got %v", "gs://bucket/file.png", fileData[core.PartKeyFileURI])
	}

	if mime, ok := fileData[core.PartKeyMimeType].(string); !ok || mime != "image/png" {
		t.Errorf("Expected mime_type = %q, got %v", "image/png", fileData[core.PartKeyMimeType])
	}
}

func TestConvertA2APartToGenAIPart_FilePartWithBytes(t *testing.T) {
	mimeType := "text/plain"
	testData := []byte("test file content")
	encodedBytes := base64.StdEncoding.EncodeToString(testData)

	filePart := &protocol.FilePart{
		File: &protocol.FileWithBytes{
			Bytes:    encodedBytes,
			MimeType: &mimeType,
		},
	}

	result, err := core.ConvertA2APartToGenAIPart(filePart)
	if err != nil {
		t.Fatalf("ConvertA2APartToGenAIPart() error = %v", err)
	}

	inlineData, ok := result[core.PartKeyInlineData].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected %q key in result, got %v", core.PartKeyInlineData, result)
	}

	data, ok := inlineData["data"].([]byte)
	if !ok {
		t.Fatalf("Expected 'data' to be []byte, got %T", inlineData["data"])
	}

	if string(data) != string(testData) {
		t.Errorf("Expected data = %q, got %q", string(testData), string(data))
	}

	if mime, ok := inlineData[core.PartKeyMimeType].(string); !ok || mime != "text/plain" {
		t.Errorf("Expected mime_type = %q, got %v", "text/plain", inlineData[core.PartKeyMimeType])
	}
}

func TestConvertA2APartToGenAIPart_DataPartFunctionCall(t *testing.T) {
	functionCallData := map[string]interface{}{
		core.PartKeyName: "search",
		core.PartKeyArgs: map[string]interface{}{
			"query": "test",
		},
	}

	dataPart := &protocol.DataPart{
		Data: functionCallData,
		Metadata: map[string]interface{}{
			core.GetKAgentMetadataKey(core.A2ADataPartMetadataTypeKey): core.A2ADataPartMetadataTypeFunctionCall,
		},
	}

	result, err := core.ConvertA2APartToGenAIPart(dataPart)
	if err != nil {
		t.Fatalf("ConvertA2APartToGenAIPart() error = %v", err)
	}

	if functionCall, ok := result[core.PartKeyFunctionCall].(map[string]interface{}); !ok {
		t.Errorf("Expected %q key in result, got %v", core.PartKeyFunctionCall, result)
	} else {
		if name, ok := functionCall[core.PartKeyName].(string); !ok || name != "search" {
			t.Errorf("Expected function name = %q, got %v", "search", functionCall[core.PartKeyName])
		}
	}
}

func TestConvertA2APartToGenAIPart_DataPartFunctionResponse(t *testing.T) {
	functionResponseData := map[string]interface{}{
		"result": "search results",
	}

	dataPart := &protocol.DataPart{
		Data: functionResponseData,
		Metadata: map[string]interface{}{
			core.GetKAgentMetadataKey(core.A2ADataPartMetadataTypeKey): core.A2ADataPartMetadataTypeFunctionResponse,
		},
	}

	result, err := core.ConvertA2APartToGenAIPart(dataPart)
	if err != nil {
		t.Fatalf("ConvertA2APartToGenAIPart() error = %v", err)
	}

	if functionResponse, ok := result[core.PartKeyFunctionResponse].(map[string]interface{}); !ok {
		t.Errorf("Expected %q key in result, got %v", core.PartKeyFunctionResponse, result)
	} else {
		if result, ok := functionResponse["result"].(string); !ok || result != "search results" {
			t.Errorf("Expected result = %q, got %v", "search results", functionResponse["result"])
		}
	}
}

func TestConvertA2APartToGenAIPart_DataPartDefault(t *testing.T) {
	// DataPart without special metadata should convert to JSON text
	dataPart := &protocol.DataPart{
		Data: map[string]interface{}{
			"key": "value",
		},
		Metadata: nil,
	}

	result, err := core.ConvertA2APartToGenAIPart(dataPart)
	if err != nil {
		t.Fatalf("ConvertA2APartToGenAIPart() error = %v", err)
	}

	if text, ok := result[core.PartKeyText].(string); !ok {
		t.Errorf("Expected 'text' key in result for default DataPart, got %v", result)
	} else if text == "" {
		t.Error("Expected non-empty text for default DataPart")
	}
}

func TestConvertGenAIPartToA2APart_TextPart(t *testing.T) {
	genaiPart := map[string]interface{}{
		core.PartKeyText: "Hello, world!",
	}

	result, err := ConvertGenAIPartToA2APart(genaiPart)
	if err != nil {
		t.Fatalf("ConvertGenAIPartToA2APart() error = %v", err)
	}

	// Handle both pointer and value types
	var textPart *protocol.TextPart
	if tp, ok := result.(*protocol.TextPart); ok {
		textPart = tp
	} else if tp, ok := result.(protocol.TextPart); ok {
		textPart = &tp
	} else {
		t.Fatalf("Expected TextPart, got %T", result)
	}

	if textPart.Text != "Hello, world!" {
		t.Errorf("Expected text = %q, got %q", "Hello, world!", textPart.Text)
	}
}

func TestConvertGenAIPartToA2APart_FilePartWithURI(t *testing.T) {
	genaiPart := map[string]interface{}{
		core.PartKeyFileData: map[string]interface{}{
			core.PartKeyFileURI:  "gs://bucket/file.png",
			core.PartKeyMimeType: "image/png",
		},
	}

	result, err := ConvertGenAIPartToA2APart(genaiPart)
	if err != nil {
		t.Fatalf("ConvertGenAIPartToA2APart() error = %v", err)
	}

	filePart, ok := result.(*protocol.FilePart)
	if !ok {
		t.Fatalf("Expected FilePart, got %T", result)
	}

	uriFile, ok := filePart.File.(*protocol.FileWithURI)
	if !ok {
		t.Fatalf("Expected FileWithURI, got %T", filePart.File)
	}

	if uriFile.URI != "gs://bucket/file.png" {
		t.Errorf("Expected URI = %q, got %q", "gs://bucket/file.png", uriFile.URI)
	}

	if uriFile.MimeType == nil || *uriFile.MimeType != "image/png" {
		t.Errorf("Expected MimeType = %q, got %v", "image/png", uriFile.MimeType)
	}
}

func TestConvertGenAIPartToA2APart_FilePartWithBytes(t *testing.T) {
	testData := []byte("test file content")
	genaiPart := map[string]interface{}{
		core.PartKeyInlineData: map[string]interface{}{
			"data":               testData,
			core.PartKeyMimeType: "text/plain",
		},
	}

	result, err := ConvertGenAIPartToA2APart(genaiPart)
	if err != nil {
		t.Fatalf("ConvertGenAIPartToA2APart() error = %v", err)
	}

	filePart, ok := result.(*protocol.FilePart)
	if !ok {
		t.Fatalf("Expected FilePart, got %T", result)
	}

	bytesFile, ok := filePart.File.(*protocol.FileWithBytes)
	if !ok {
		t.Fatalf("Expected FileWithBytes, got %T", filePart.File)
	}

	decoded, err := base64.StdEncoding.DecodeString(bytesFile.Bytes)
	if err != nil {
		t.Fatalf("Failed to decode base64: %v", err)
	}

	if string(decoded) != string(testData) {
		t.Errorf("Expected decoded data = %q, got %q", string(testData), string(decoded))
	}
}

func TestConvertGenAIPartToA2APart_FunctionCall(t *testing.T) {
	genaiPart := map[string]interface{}{
		core.PartKeyFunctionCall: map[string]interface{}{
			core.PartKeyName: "search",
			core.PartKeyArgs: map[string]interface{}{
				"query": "test",
			},
		},
	}

	result, err := ConvertGenAIPartToA2APart(genaiPart)
	if err != nil {
		t.Fatalf("ConvertGenAIPartToA2APart() error = %v", err)
	}

	dataPart, ok := result.(*protocol.DataPart)
	if !ok {
		t.Fatalf("Expected DataPart, got %T", result)
	}

	// Check metadata
	metadataKey := core.GetKAgentMetadataKey(core.A2ADataPartMetadataTypeKey)
	if partType, ok := dataPart.Metadata[metadataKey].(string); !ok {
		t.Errorf("Expected metadata type key, got %v", dataPart.Metadata)
	} else if partType != core.A2ADataPartMetadataTypeFunctionCall {
		t.Errorf("Expected metadata type = %q, got %q", core.A2ADataPartMetadataTypeFunctionCall, partType)
	}

	// Check data
	if functionCall, ok := dataPart.Data.(map[string]interface{}); !ok {
		t.Errorf("Expected function_call data, got %T", dataPart.Data)
	} else {
		if name, ok := functionCall[core.PartKeyName].(string); !ok || name != "search" {
			t.Errorf("Expected function name = %q, got %v", "search", functionCall[core.PartKeyName])
		}
	}
}

func TestConvertGenAIPartToA2APart_FunctionResponse(t *testing.T) {
	genaiPart := map[string]interface{}{
		core.PartKeyFunctionResponse: map[string]interface{}{
			"result": "search results",
		},
	}

	result, err := ConvertGenAIPartToA2APart(genaiPart)
	if err != nil {
		t.Fatalf("ConvertGenAIPartToA2APart() error = %v", err)
	}

	dataPart, ok := result.(*protocol.DataPart)
	if !ok {
		t.Fatalf("Expected DataPart, got %T", result)
	}

	// Check metadata
	metadataKey := core.GetKAgentMetadataKey(core.A2ADataPartMetadataTypeKey)
	if partType, ok := dataPart.Metadata[metadataKey].(string); !ok {
		t.Errorf("Expected metadata type key, got %v", dataPart.Metadata)
	} else if partType != core.A2ADataPartMetadataTypeFunctionResponse {
		t.Errorf("Expected metadata type = %q, got %q", core.A2ADataPartMetadataTypeFunctionResponse, partType)
	}
}

// TestConvertGenAIPartToA2APart_FunctionResponseMCPContent ensures MCP-style response
// (content array, no result) is normalized so response.result is a JSON object (aligned with Python).
func TestConvertGenAIPartToA2APart_FunctionResponseMCPContent(t *testing.T) {
	contentArr := []interface{}{
		map[string]interface{}{"type": "text", "text": "72°F and sunny"},
	}
	genaiPart := map[string]interface{}{
		core.PartKeyFunctionResponse: map[string]interface{}{
			core.PartKeyID:   "call_1",
			core.PartKeyName: "get_weather",
			core.PartKeyResponse: map[string]interface{}{
				"content": contentArr,
			},
		},
	}

	result, err := ConvertGenAIPartToA2APart(genaiPart)
	if err != nil {
		t.Fatalf("ConvertGenAIPartToA2APart() error = %v", err)
	}

	dataPart, ok := result.(*protocol.DataPart)
	if !ok {
		t.Fatalf("Expected DataPart, got %T", result)
	}

	data, ok := dataPart.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected Data map, got %T", dataPart.Data)
	}
	resp, ok := data[core.PartKeyResponse].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected response map, got %T", data[core.PartKeyResponse])
	}
	// Align with Python: result is JSON object (e.g. {"content": [...]}), not string
	resultObj, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected response.result object (JSON), got %T: %v", resp["result"], resp["result"])
	}
	resultContent, ok := resultObj["content"].([]interface{})
	if !ok || len(resultContent) == 0 {
		t.Fatalf("Expected result.content array, got %v", resultObj["content"])
	}
	first, ok := resultContent[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected content[0] map, got %T", resultContent[0])
	}
	if first[core.PartKeyText] != "72°F and sunny" {
		t.Errorf("Expected content[0].text = %q, got %v", "72°F and sunny", first[core.PartKeyText])
	}
}

func TestConvertGenAIPartToA2APart_Unsupported(t *testing.T) {
	genaiPart := map[string]interface{}{
		"unsupported_type": "value",
	}

	_, err := ConvertGenAIPartToA2APart(genaiPart)
	if err == nil {
		t.Error("Expected error for unsupported genai part type, got nil")
	}
}
