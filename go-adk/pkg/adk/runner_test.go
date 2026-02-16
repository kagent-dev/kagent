package adk

import (
	"testing"

	"github.com/kagent-dev/kagent/go-adk/pkg/adk/converter"
	"github.com/kagent-dev/kagent/go-adk/pkg/core/a2a"
	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

func TestA2AMessageToGenAIContent_FunctionCall(t *testing.T) {
	msg := &protocol.Message{
		Role: protocol.MessageRoleUser,
		Parts: []protocol.Part{
			&protocol.DataPart{
				Data: map[string]interface{}{
					"name": "my_func",
					"args": map[string]interface{}{"key": "value"},
				},
				Metadata: map[string]interface{}{
					a2a.MetadataKeyType: a2a.A2ADataPartMetadataTypeFunctionCall,
				},
			},
		},
	}

	content, err := converter.A2AMessageToGenAIContent(msg)
	if err != nil {
		t.Fatalf("A2AMessageToGenAIContent() error = %v", err)
	}
	if len(content.Parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(content.Parts))
	}
	part := content.Parts[0]
	if part.FunctionCall == nil {
		t.Fatal("Expected FunctionCall to be set")
	}
	if part.FunctionCall.Name != "my_func" {
		t.Errorf("Expected name = %q, got %q", "my_func", part.FunctionCall.Name)
	}
}

func TestA2AMessageToGenAIContent_FunctionResponse(t *testing.T) {
	msg := &protocol.Message{
		Role: protocol.MessageRoleAgent,
		Parts: []protocol.Part{
			&protocol.DataPart{
				Data: map[string]interface{}{
					"name":     "my_func",
					"response": map[string]interface{}{"result": "ok"},
				},
				Metadata: map[string]interface{}{
					a2a.MetadataKeyType: a2a.A2ADataPartMetadataTypeFunctionResponse,
				},
			},
		},
	}

	content, err := converter.A2AMessageToGenAIContent(msg)
	if err != nil {
		t.Fatalf("A2AMessageToGenAIContent() error = %v", err)
	}
	if len(content.Parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(content.Parts))
	}
	part := content.Parts[0]
	if part.FunctionResponse == nil {
		t.Fatal("Expected FunctionResponse to be set")
	}
	if part.FunctionResponse.Name != "my_func" {
		t.Errorf("Expected name = %q, got %q", "my_func", part.FunctionResponse.Name)
	}
}

func TestA2AMessageToGenAIContent_TextPart(t *testing.T) {
	msg := &protocol.Message{
		Role: protocol.MessageRoleUser,
		Parts: []protocol.Part{
			protocol.NewTextPart("hello world"),
		},
	}

	content, err := converter.A2AMessageToGenAIContent(msg)
	if err != nil {
		t.Fatalf("A2AMessageToGenAIContent() error = %v", err)
	}
	if content.Role != "user" {
		t.Errorf("Expected role = user, got %q", content.Role)
	}
	if len(content.Parts) != 1 {
		t.Fatalf("Expected 1 part, got %d", len(content.Parts))
	}
	if content.Parts[0].Text != "hello world" {
		t.Errorf("Expected text = %q, got %q", "hello world", content.Parts[0].Text)
	}
}

func TestA2AMessageToGenAIContent_AgentRole(t *testing.T) {
	msg := &protocol.Message{
		Role: protocol.MessageRoleAgent,
		Parts: []protocol.Part{
			protocol.NewTextPart("model response"),
		},
	}

	content, err := converter.A2AMessageToGenAIContent(msg)
	if err != nil {
		t.Fatalf("A2AMessageToGenAIContent() error = %v", err)
	}
	if content.Role != "model" {
		t.Errorf("Expected role = model, got %q", content.Role)
	}
}

func TestA2AMessageToGenAIContent_Nil(t *testing.T) {
	_, err := converter.A2AMessageToGenAIContent(nil)
	if err == nil {
		t.Fatal("Expected error for nil message")
	}
}

