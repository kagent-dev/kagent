package converter

import (
	"encoding/base64"
	"testing"

	"github.com/kagent-dev/kagent/go-adk/pkg/core/a2a"
	"google.golang.org/genai"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

func TestA2APartToGenAIPart_TextPart(t *testing.T) {
	textPart := &protocol.TextPart{Text: "Hello, world!"}
	result, err := A2APartToGenAIPart(textPart)
	if err != nil {
		t.Fatalf("A2APartToGenAIPart() error = %v", err)
	}
	if result.Text != "Hello, world!" {
		t.Errorf("Expected text = %q, got %q", "Hello, world!", result.Text)
	}
}

func TestA2APartToGenAIPart_FilePartWithURI(t *testing.T) {
	mimeType := "image/png"
	filePart := &protocol.FilePart{
		File: &protocol.FileWithURI{
			URI:      "gs://bucket/file.png",
			MimeType: &mimeType,
		},
	}

	result, err := A2APartToGenAIPart(filePart)
	if err != nil {
		t.Fatalf("A2APartToGenAIPart() error = %v", err)
	}
	if result.FileData == nil {
		t.Fatal("Expected FileData to be set")
	}
	if result.FileData.FileURI != "gs://bucket/file.png" {
		t.Errorf("Expected URI = %q, got %q", "gs://bucket/file.png", result.FileData.FileURI)
	}
	if result.FileData.MIMEType != "image/png" {
		t.Errorf("Expected MIME = %q, got %q", "image/png", result.FileData.MIMEType)
	}
}

func TestA2APartToGenAIPart_FilePartWithBytes(t *testing.T) {
	mimeType := "text/plain"
	testData := []byte("test file content")
	encodedBytes := base64.StdEncoding.EncodeToString(testData)

	filePart := &protocol.FilePart{
		File: &protocol.FileWithBytes{
			Bytes:    encodedBytes,
			MimeType: &mimeType,
		},
	}

	result, err := A2APartToGenAIPart(filePart)
	if err != nil {
		t.Fatalf("A2APartToGenAIPart() error = %v", err)
	}
	if result.InlineData == nil {
		t.Fatal("Expected InlineData to be set")
	}
	if string(result.InlineData.Data) != string(testData) {
		t.Errorf("Expected data = %q, got %q", string(testData), string(result.InlineData.Data))
	}
	if result.InlineData.MIMEType != "text/plain" {
		t.Errorf("Expected MIME = %q, got %q", "text/plain", result.InlineData.MIMEType)
	}
}

func TestA2APartToGenAIPart_DataPartFunctionCall(t *testing.T) {
	dataPart := &protocol.DataPart{
		Data: map[string]interface{}{
			a2a.PartKeyName: "search",
			a2a.PartKeyArgs: map[string]interface{}{"query": "test"},
		},
		Metadata: map[string]interface{}{
			a2a.MetadataKeyType: a2a.A2ADataPartMetadataTypeFunctionCall,
		},
	}

	result, err := A2APartToGenAIPart(dataPart)
	if err != nil {
		t.Fatalf("A2APartToGenAIPart() error = %v", err)
	}
	if result.FunctionCall == nil {
		t.Fatal("Expected FunctionCall to be set")
	}
	if result.FunctionCall.Name != "search" {
		t.Errorf("Expected name = %q, got %q", "search", result.FunctionCall.Name)
	}
}

func TestA2APartToGenAIPart_DataPartFunctionResponse(t *testing.T) {
	dataPart := &protocol.DataPart{
		Data: map[string]interface{}{
			a2a.PartKeyName:     "search",
			a2a.PartKeyResponse: map[string]interface{}{"result": "search results"},
		},
		Metadata: map[string]interface{}{
			a2a.MetadataKeyType: a2a.A2ADataPartMetadataTypeFunctionResponse,
		},
	}

	result, err := A2APartToGenAIPart(dataPart)
	if err != nil {
		t.Fatalf("A2APartToGenAIPart() error = %v", err)
	}
	if result.FunctionResponse == nil {
		t.Fatal("Expected FunctionResponse to be set")
	}
	if result.FunctionResponse.Name != "search" {
		t.Errorf("Expected name = %q, got %q", "search", result.FunctionResponse.Name)
	}
}

func TestA2APartToGenAIPart_DataPartDefault(t *testing.T) {
	dataPart := &protocol.DataPart{
		Data:     map[string]interface{}{"key": "value"},
		Metadata: nil,
	}

	result, err := A2APartToGenAIPart(dataPart)
	if err != nil {
		t.Fatalf("A2APartToGenAIPart() error = %v", err)
	}
	if result.Text == "" {
		t.Error("Expected non-empty text for default DataPart")
	}
}

func TestGenAIPartToA2APart_TextPart(t *testing.T) {
	genaiPart := &genai.Part{
		Text: "Hello, world!",
	}

	result, err := GenAIPartToA2APart(genaiPart)
	if err != nil {
		t.Fatalf("GenAIPartToA2APart() error = %v", err)
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

func TestGenAIPartToA2APart_FilePartWithURI(t *testing.T) {
	genaiPart := &genai.Part{
		FileData: &genai.FileData{
			FileURI:  "gs://bucket/file.png",
			MIMEType: "image/png",
		},
	}

	result, err := GenAIPartToA2APart(genaiPart)
	if err != nil {
		t.Fatalf("GenAIPartToA2APart() error = %v", err)
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

func TestGenAIPartToA2APart_FilePartWithBytes(t *testing.T) {
	testData := []byte("test file content")
	genaiPart := &genai.Part{
		InlineData: &genai.Blob{
			Data:     testData,
			MIMEType: "text/plain",
		},
	}

	result, err := GenAIPartToA2APart(genaiPart)
	if err != nil {
		t.Fatalf("GenAIPartToA2APart() error = %v", err)
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

func TestGenAIPartToA2APart_FunctionCall(t *testing.T) {
	genaiPart := &genai.Part{
		FunctionCall: &genai.FunctionCall{
			Name: "search",
			Args: map[string]interface{}{"query": "test"},
		},
	}

	result, err := GenAIPartToA2APart(genaiPart)
	if err != nil {
		t.Fatalf("GenAIPartToA2APart() error = %v", err)
	}

	dataPart, ok := result.(*protocol.DataPart)
	if !ok {
		t.Fatalf("Expected DataPart, got %T", result)
	}
	metadataKey := a2a.GetKAgentMetadataKey(a2a.A2ADataPartMetadataTypeKey)
	if partType, ok := dataPart.Metadata[metadataKey].(string); !ok {
		t.Errorf("Expected metadata type key, got %v", dataPart.Metadata)
	} else if partType != a2a.A2ADataPartMetadataTypeFunctionCall {
		t.Errorf("Expected type = %q, got %q", a2a.A2ADataPartMetadataTypeFunctionCall, partType)
	}
	if functionCall, ok := dataPart.Data.(map[string]interface{}); !ok {
		t.Errorf("Expected function_call data, got %T", dataPart.Data)
	} else if name, ok := functionCall[a2a.PartKeyName].(string); !ok || name != "search" {
		t.Errorf("Expected name = %q, got %v", "search", functionCall[a2a.PartKeyName])
	}
}

func TestGenAIPartToA2APart_FunctionResponse(t *testing.T) {
	genaiPart := &genai.Part{
		FunctionResponse: &genai.FunctionResponse{
			Name:     "search",
			Response: map[string]interface{}{"result": "search results"},
		},
	}

	result, err := GenAIPartToA2APart(genaiPart)
	if err != nil {
		t.Fatalf("GenAIPartToA2APart() error = %v", err)
	}

	dataPart, ok := result.(*protocol.DataPart)
	if !ok {
		t.Fatalf("Expected DataPart, got %T", result)
	}
	metadataKey := a2a.GetKAgentMetadataKey(a2a.A2ADataPartMetadataTypeKey)
	if partType, ok := dataPart.Metadata[metadataKey].(string); !ok {
		t.Errorf("Expected metadata type key, got %v", dataPart.Metadata)
	} else if partType != a2a.A2ADataPartMetadataTypeFunctionResponse {
		t.Errorf("Expected type = %q, got %q", a2a.A2ADataPartMetadataTypeFunctionResponse, partType)
	}
}

func TestGenAIPartToA2APart_FunctionResponseMCPContent(t *testing.T) {
	contentArr := []interface{}{
		map[string]interface{}{"type": "text", "text": "72°F and sunny"},
	}
	genaiPart := &genai.Part{
		FunctionResponse: &genai.FunctionResponse{
			ID:   "call_1",
			Name: "get_weather",
			Response: map[string]interface{}{
				"content": contentArr,
			},
		},
	}

	result, err := GenAIPartToA2APart(genaiPart)
	if err != nil {
		t.Fatalf("GenAIPartToA2APart() error = %v", err)
	}

	dataPart, ok := result.(*protocol.DataPart)
	if !ok {
		t.Fatalf("Expected DataPart, got %T", result)
	}
	data, ok := dataPart.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected Data map, got %T", dataPart.Data)
	}
	resp, ok := data[a2a.PartKeyResponse].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected response map, got %T", data[a2a.PartKeyResponse])
	}
	resultObj, ok := resp["result"].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected response.result object, got %T: %v", resp["result"], resp["result"])
	}
	resultContent, ok := resultObj["content"].([]interface{})
	if !ok || len(resultContent) == 0 {
		t.Fatalf("Expected result.content array, got %v", resultObj["content"])
	}
	first, ok := resultContent[0].(map[string]interface{})
	if !ok {
		t.Fatalf("Expected content[0] map, got %T", resultContent[0])
	}
	if first[a2a.PartKeyText] != "72°F and sunny" {
		t.Errorf("Expected text = %q, got %v", "72°F and sunny", first[a2a.PartKeyText])
	}
}

func TestGenAIPartToA2APart_EmptyPart(t *testing.T) {
	genaiPart := &genai.Part{}
	_, err := GenAIPartToA2APart(genaiPart)
	if err == nil {
		t.Error("Expected error for empty genai part, got nil")
	}
}

func TestGenAIPartToA2APart_NilPart(t *testing.T) {
	_, err := GenAIPartToA2APart(nil)
	if err == nil {
		t.Error("Expected error for nil genai part, got nil")
	}
}
