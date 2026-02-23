package a2a

import (
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"google.golang.org/adk/server/adka2a"
)

func TestConvertDataPartToGenAI_FunctionCall_KagentPrefix(t *testing.T) {
	dp := &a2atype.DataPart{
		Data: map[string]any{
			"name": "my_func",
			"args": map[string]any{"key": "value"},
			"id":   "call_1",
		},
		Metadata: map[string]any{
			GetKAgentMetadataKey(A2ADataPartMetadataTypeKey): A2ADataPartMetadataTypeFunctionCall,
		},
	}

	part, err := convertDataPartToGenAI(dp, GetKAgentMetadataKey(A2ADataPartMetadataTypeKey))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if part.FunctionCall == nil {
		t.Fatal("expected FunctionCall to be set")
	}
	if part.FunctionCall.Name != "my_func" {
		t.Errorf("name = %q, want %q", part.FunctionCall.Name, "my_func")
	}
	if part.FunctionCall.ID != "call_1" {
		t.Errorf("id = %q, want %q", part.FunctionCall.ID, "call_1")
	}
}

func TestConvertDataPartToGenAI_FunctionCall_AdkPrefix(t *testing.T) {
	dp := &a2atype.DataPart{
		Data: map[string]any{
			"name": "my_func",
			"args": map[string]any{"key": "value"},
			"id":   "call_1",
		},
		Metadata: map[string]any{
			adka2a.ToA2AMetaKey(A2ADataPartMetadataTypeKey): A2ADataPartMetadataTypeFunctionCall,
		},
	}

	part, err := convertDataPartToGenAI(dp, adka2a.ToA2AMetaKey(A2ADataPartMetadataTypeKey))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if part.FunctionCall == nil {
		t.Fatal("expected FunctionCall to be set")
	}
	if part.FunctionCall.Name != "my_func" {
		t.Errorf("name = %q, want %q", part.FunctionCall.Name, "my_func")
	}
}

func TestConvertDataPartToGenAI_FunctionResponse(t *testing.T) {
	dp := &a2atype.DataPart{
		Data: map[string]any{
			"name":     "my_func",
			"response": map[string]any{"result": "ok"},
			"id":       "call_2",
		},
		Metadata: map[string]any{
			GetKAgentMetadataKey(A2ADataPartMetadataTypeKey): A2ADataPartMetadataTypeFunctionResponse,
		},
	}

	part, err := convertDataPartToGenAI(dp, GetKAgentMetadataKey(A2ADataPartMetadataTypeKey))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if part.FunctionResponse == nil {
		t.Fatal("expected FunctionResponse to be set")
	}
	if part.FunctionResponse.Name != "my_func" {
		t.Errorf("name = %q, want %q", part.FunctionResponse.Name, "my_func")
	}
	if part.FunctionResponse.ID != "call_2" {
		t.Errorf("id = %q, want %q", part.FunctionResponse.ID, "call_2")
	}
}

func TestConvertDataPartToGenAI_Nil(t *testing.T) {
	part, err := convertDataPartToGenAI(nil, GetKAgentMetadataKey(A2ADataPartMetadataTypeKey))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if part != nil {
		t.Fatalf("expected nil part, got %v", part)
	}
}

func TestConvertDataPartToGenAI_UnknownType(t *testing.T) {
	dp := &a2atype.DataPart{
		Data:     map[string]any{"foo": "bar"},
		Metadata: map[string]any{"kagent_type": "unknown_type"},
	}

	_, err := convertDataPartToGenAI(dp, "kagent_type")
	if err == nil {
		t.Fatal("expected error for unknown part type")
	}
}
