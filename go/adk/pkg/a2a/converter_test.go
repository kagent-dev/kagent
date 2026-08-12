package a2a

import (
	"context"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"google.golang.org/adk/v2/server/adka2a/v2"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
)

func convDataPart(data map[string]any, metadata map[string]any) *a2atype.Part {
	p := a2atype.NewDataPart(data)
	if metadata != nil {
		p.Metadata = metadata
	}
	return p
}

// ---------------------------------------------------------------------------
// convertDataPartToGenAI
// ---------------------------------------------------------------------------

func TestConvertDataPartToGenAI_FunctionCall_KagentPrefix(t *testing.T) {
	data := map[string]any{
		"name": "my_func",
		"args": map[string]any{"key": "value"},
		"id":   "call_1",
	}
	meta := map[string]any{
		GetKAgentMetadataKey(A2ADataPartMetadataTypeKey): A2ADataPartMetadataTypeFunctionCall,
	}

	part, err := convertDataPartToGenAI(data, meta, GetKAgentMetadataKey(A2ADataPartMetadataTypeKey))
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
	data := map[string]any{
		"name": "my_func",
		"args": map[string]any{"key": "value"},
		"id":   "call_1",
	}
	meta := map[string]any{
		adka2a.ToA2AMetaKey(A2ADataPartMetadataTypeKey): A2ADataPartMetadataTypeFunctionCall,
	}

	part, err := convertDataPartToGenAI(data, meta, adka2a.ToA2AMetaKey(A2ADataPartMetadataTypeKey))
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
	data := map[string]any{
		"name":     "my_func",
		"response": map[string]any{"result": "ok"},
		"id":       "call_2",
	}
	meta := map[string]any{
		GetKAgentMetadataKey(A2ADataPartMetadataTypeKey): A2ADataPartMetadataTypeFunctionResponse,
	}

	part, err := convertDataPartToGenAI(data, meta, GetKAgentMetadataKey(A2ADataPartMetadataTypeKey))
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
	part, err := convertDataPartToGenAI(nil, nil, GetKAgentMetadataKey(A2ADataPartMetadataTypeKey))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if part != nil {
		t.Fatalf("expected nil part, got %v", part)
	}
}

func TestConvertDataPartToGenAI_UnknownType(t *testing.T) {
	part, err := convertDataPartToGenAI(
		map[string]any{"foo": "bar"},
		map[string]any{"kagent_type": "unknown_type"},
		"kagent_type",
	)
	if err != nil {
		t.Fatalf("unexpected error for unknown part type: %v", err)
	}
	if part == nil {
		t.Fatal("expected fallback GenAI part for unknown type")
	}
}

// ---------------------------------------------------------------------------
// a2aPartConverter
// ---------------------------------------------------------------------------

func TestA2APartConverter_TextPart(t *testing.T) {
	part, err := a2aPartConverter(context.Background(), nil, a2atype.NewTextPart("hello"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if part == nil || part.Text != "hello" {
		t.Fatalf("converted part = %#v, want text hello", part)
	}
}

func TestA2APartConverter_DropsUnrecognisedDataPart(t *testing.T) {
	// A DataPart with no recognised kagent_type metadata (e.g. a HITL decision
	// payload like {decision_type: "approve"}) should be dropped silently.
	part, err := a2aPartConverter(
		context.Background(), nil,
		convDataPart(map[string]any{"decision_type": "approve"}, nil),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if part != nil {
		t.Fatalf("converted part = %#v, want nil", part)
	}
}

func TestA2APartConverter_KagentTypeFunctionResponse(t *testing.T) {
	// A DataPart with kagent_type=function_response should be converted to GenAI.
	dp := convDataPart(map[string]any{
		"name":     "my_func",
		"id":       "call_1",
		"response": map[string]any{"result": "ok"},
	}, map[string]any{
		GetKAgentMetadataKey(A2ADataPartMetadataTypeKey): A2ADataPartMetadataTypeFunctionResponse,
	})
	part, err := a2aPartConverter(context.Background(), nil, dp)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if part == nil || part.FunctionResponse == nil {
		t.Fatal("expected FunctionResponse, got nil")
	}
	if part.FunctionResponse.Name != "my_func" {
		t.Errorf("name = %q, want my_func", part.FunctionResponse.Name)
	}
}

func TestGenAIPartConverter_PreservesLongRunningMetadata(t *testing.T) {
	call := genai.NewPartFromFunctionCall("dangerous_tool", map[string]any{"path": "/tmp/x"})
	call.FunctionCall.ID = "call-1"
	part, err := genAIPartConverter(
		context.Background(),
		&adksession.Event{LongRunningToolIDs: []string{"call-1"}},
		call,
	)
	if err != nil {
		t.Fatalf("genAIPartConverter() error = %v", err)
	}
	if part == nil {
		t.Fatal("genAIPartConverter() returned nil")
	}
	if got, _ := ReadMetadataValue(part.Metadata, A2ADataPartMetadataIsLongRunningKey); got != true {
		t.Fatalf("long-running metadata = %#v, want true", got)
	}
}
