package a2a

import (
	"context"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"google.golang.org/adk/server/adka2a"
	"google.golang.org/genai"
)

// ---------------------------------------------------------------------------
// convertDataPartToGenAI
// ---------------------------------------------------------------------------

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

// ---------------------------------------------------------------------------
// messageToGenAIContent
// ---------------------------------------------------------------------------

func TestMessageToGenAIContent_TextPart(t *testing.T) {
	msg := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.TextPart{Text: "hello"})
	content, err := messageToGenAIContent(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content == nil {
		t.Fatal("expected non-nil content")
		return
	}
	if len(content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(content.Parts))
	}
	if content.Parts[0].Text != "hello" {
		t.Errorf("text = %q, want %q", content.Parts[0].Text, "hello")
	}
}

func TestMessageToGenAIContent_DropsUnrecognisedDataPart(t *testing.T) {
	// A DataPart with no recognised kagent_type metadata (e.g. a HITL decision
	// payload like {decision_type: "approve"}) should be dropped silently.
	msg := a2atype.NewMessage(a2atype.MessageRoleUser,
		a2atype.TextPart{Text: "approving"},
		&a2atype.DataPart{Data: map[string]any{"decision_type": "approve"}},
	)
	content, err := messageToGenAIContent(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only the TextPart should survive; the unrecognised DataPart is dropped.
	if len(content.Parts) != 1 {
		t.Fatalf("expected 1 part (DataPart dropped), got %d", len(content.Parts))
	}
	if content.Parts[0].Text != "approving" {
		t.Errorf("remaining part text = %q, want %q", content.Parts[0].Text, "approving")
	}
}

func TestMessageToGenAIContent_KagentTypeFunctionResponse(t *testing.T) {
	// A DataPart with kagent_type=function_response should be converted to GenAI.
	dp := &a2atype.DataPart{
		Data: map[string]any{
			"name":     "my_func",
			"id":       "call_1",
			"response": map[string]any{"result": "ok"},
		},
		Metadata: map[string]any{
			GetKAgentMetadataKey(A2ADataPartMetadataTypeKey): A2ADataPartMetadataTypeFunctionResponse,
		},
	}
	msg := a2atype.NewMessage(a2atype.MessageRoleUser, dp)
	content, err := messageToGenAIContent(context.Background(), msg)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(content.Parts) != 1 {
		t.Fatalf("expected 1 part, got %d", len(content.Parts))
	}
	if content.Parts[0].FunctionResponse == nil {
		t.Fatal("expected FunctionResponse, got nil")
	}
	if content.Parts[0].FunctionResponse.Name != "my_func" {
		t.Errorf("name = %q, want my_func", content.Parts[0].FunctionResponse.Name)
	}
}

func TestMessageToGenAIContent_NilMessage(t *testing.T) {
	content, err := messageToGenAIContent(context.Background(), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if content != nil {
		t.Errorf("expected nil content for nil message, got %v", content)
	}
}

// ---------------------------------------------------------------------------
// stampSubagentSessionID
// ---------------------------------------------------------------------------

func TestStampSubagentSessionID_FunctionCallPart(t *testing.T) {
	subagentIDs := map[string]string{"k8s_agent": "session-abc"}

	dp := &a2atype.DataPart{
		Data: map[string]any{
			PartKeyName: "k8s_agent",
			PartKeyArgs: map[string]any{"request": "list pods"},
		},
		Metadata: map[string]any{
			adka2a.ToA2AMetaKey("type"): A2ADataPartMetadataTypeFunctionCall,
		},
	}
	updated := stampSubagentSessionID(dp, subagentIDs)
	updatedDP, ok := updated.(a2atype.DataPart)
	if !ok {
		t.Fatalf("updated part type = %T, want a2atype.DataPart", updated)
	}

	sessionID, has := updatedDP.Metadata[GetKAgentMetadataKey("subagent_session_id")]
	if !has {
		t.Fatal("expected kagent_subagent_session_id in metadata, not found")
	}
	if sessionID != "session-abc" {
		t.Errorf("session_id = %q, want session-abc", sessionID)
	}
}

func TestStampSubagentSessionID_UnknownTool(t *testing.T) {
	subagentIDs := map[string]string{"k8s_agent": "session-abc"}

	dp := &a2atype.DataPart{
		Data: map[string]any{
			PartKeyName: "unknown_tool",
		},
		Metadata: map[string]any{
			adka2a.ToA2AMetaKey("type"): A2ADataPartMetadataTypeFunctionCall,
		},
	}
	updated := stampSubagentSessionID(dp, subagentIDs)
	updatedDP, ok := updated.(a2atype.DataPart)
	if !ok {
		t.Fatalf("updated part type = %T, want a2atype.DataPart", updated)
	}

	if _, ok := updatedDP.Metadata[GetKAgentMetadataKey("subagent_session_id")]; ok {
		t.Error("expected no subagent_session_id for unknown tool")
	}
}

// ---------------------------------------------------------------------------
// toA2AMetadataMap
// ---------------------------------------------------------------------------

func TestToA2AMetadataMap(t *testing.T) {
	t.Parallel()
	um := &genai.GenerateContentResponseUsageMetadata{
		PromptTokenCount:     10,
		CandidatesTokenCount: 20,
	}
	m, err := toA2AMetadataMap(um)
	if err != nil {
		t.Fatalf("toA2AMetadataMap: %v", err)
	}
	if m == nil {
		t.Fatal("expected non-nil map")
	}
	pt, ok := m["promptTokenCount"].(float64)
	if !ok || pt != 10 {
		t.Fatalf("promptTokenCount: got %v (%T), want float64 10", m["promptTokenCount"], m["promptTokenCount"])
	}
	ct, ok := m["candidatesTokenCount"].(float64)
	if !ok || ct != 20 {
		t.Fatalf("candidatesTokenCount: got %v (%T), want float64 20", m["candidatesTokenCount"], m["candidatesTokenCount"])
	}
}

func TestToA2AMetadataMap_nil(t *testing.T) {
	t.Parallel()
	m, err := toA2AMetadataMap(nil)
	if err != nil {
		t.Fatalf("toA2AMetadataMap(nil): %v", err)
	}
	if m != nil {
		t.Fatalf("expected nil map, got %#v", m)
	}
}
