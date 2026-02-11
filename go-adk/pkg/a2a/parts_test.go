package a2a

import (
	"encoding/base64"
	"testing"

	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

func TestConvertGenAIPartToA2APart_TextPart(t *testing.T) {
	genaiPart := map[string]any{
		PartKeyText: "Hello, world!",
	}

	result, err := ConvertGenAIPartToA2APart(genaiPart)
	if err != nil {
		t.Fatalf("ConvertGenAIPartToA2APart() error = %v", err)
	}

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
	genaiPart := map[string]any{
		PartKeyFileData: map[string]any{
			PartKeyFileURI:  "gs://bucket/file.png",
			PartKeyMimeType: "image/png",
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
	genaiPart := map[string]any{
		PartKeyInlineData: map[string]any{
			"data":          testData,
			PartKeyMimeType: "text/plain",
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
	genaiPart := map[string]any{
		PartKeyFunctionCall: map[string]any{
			PartKeyName: "search",
			PartKeyArgs: map[string]any{
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

	metadataKey := GetKAgentMetadataKey(A2ADataPartMetadataTypeKey)
	if partType, ok := dataPart.Metadata[metadataKey].(string); !ok {
		t.Errorf("Expected metadata type key, got %v", dataPart.Metadata)
	} else if partType != A2ADataPartMetadataTypeFunctionCall {
		t.Errorf("Expected metadata type = %q, got %q", A2ADataPartMetadataTypeFunctionCall, partType)
	}

	if functionCall, ok := dataPart.Data.(map[string]any); !ok {
		t.Errorf("Expected function_call data, got %T", dataPart.Data)
	} else {
		if name, ok := functionCall[PartKeyName].(string); !ok || name != "search" {
			t.Errorf("Expected function name = %q, got %v", "search", functionCall[PartKeyName])
		}
	}
}

func TestConvertGenAIPartToA2APart_FunctionResponse(t *testing.T) {
	genaiPart := map[string]any{
		PartKeyFunctionResponse: map[string]any{
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

	metadataKey := GetKAgentMetadataKey(A2ADataPartMetadataTypeKey)
	if partType, ok := dataPart.Metadata[metadataKey].(string); !ok {
		t.Errorf("Expected metadata type key, got %v", dataPart.Metadata)
	} else if partType != A2ADataPartMetadataTypeFunctionResponse {
		t.Errorf("Expected metadata type = %q, got %q", A2ADataPartMetadataTypeFunctionResponse, partType)
	}
}

func TestConvertGenAIPartToA2APart_FunctionResponseMCPContent(t *testing.T) {
	contentArr := []any{
		map[string]any{"type": "text", "text": "72°F and sunny"},
	}
	genaiPart := map[string]any{
		PartKeyFunctionResponse: map[string]any{
			PartKeyID:   "call_1",
			PartKeyName: "get_weather",
			PartKeyResponse: map[string]any{
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

	data, ok := dataPart.Data.(map[string]any)
	if !ok {
		t.Fatalf("Expected Data map, got %T", dataPart.Data)
	}
	resp, ok := data[PartKeyResponse].(map[string]any)
	if !ok {
		t.Fatalf("Expected response map, got %T", data[PartKeyResponse])
	}
	resultObj, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("Expected response.result object (JSON), got %T: %v", resp["result"], resp["result"])
	}
	resultContent, ok := resultObj["content"].([]any)
	if !ok || len(resultContent) == 0 {
		t.Fatalf("Expected result.content array, got %v", resultObj["content"])
	}
	first, ok := resultContent[0].(map[string]any)
	if !ok {
		t.Fatalf("Expected content[0] map, got %T", resultContent[0])
	}
	if first[PartKeyText] != "72°F and sunny" {
		t.Errorf("Expected content[0].text = %q, got %v", "72°F and sunny", first[PartKeyText])
	}
}

func TestConvertGenAIPartToA2APart_Unsupported(t *testing.T) {
	genaiPart := map[string]any{
		"unsupported_type": "value",
	}

	_, err := ConvertGenAIPartToA2APart(genaiPart)
	if err == nil {
		t.Error("Expected error for unsupported genai part type, got nil")
	}
}
