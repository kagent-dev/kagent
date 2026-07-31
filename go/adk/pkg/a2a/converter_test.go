package a2a

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/kagent-dev/kagent/go/core/pkg/a2acompat/trpcv0"
	"google.golang.org/adk/server/adka2a" //nolint:staticcheck // kagent still uses a2a-go v1; this ADK package is the compatibility adapter.
	"google.golang.org/genai"
	trpc "trpc.group/trpc-go/trpc-a2a-go/protocol"
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

	part, err := convertDataPartToGenAI(dp, "kagent_type")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if part == nil || part.InlineData == nil {
		t.Fatalf("expected unknown DataPart to fall back to InlineData, got %#v", part)
	}
	if part.InlineData.MIMEType != "text/plain" {
		t.Errorf("mime type = %q, want text/plain", part.InlineData.MIMEType)
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

func TestPreserveEmptyJSONArraysAcrossTaskStatusGobCopy(t *testing.T) {
	part := a2atype.DataPart{Data: map[string]any{
		"response": map[string]any{
			"output": map[string]any{
				"messages":             []any{},
				"explanation_codes":    []any{},
				"matches":              []any{map[string]any{"matched_on": []any{}}},
				"unchanged_null":       nil,
				"unchanged_typed_null": []any(nil),
				"unchanged_values":     []any{"one"},
			},
		},
	}}

	unprotected := gobCopyMessage(t, a2atype.NewMessage(a2atype.MessageRoleAgent, part))
	unprotectedOutput := functionResponseOutput(t, unprotected)
	unprotectedMessages, ok := unprotectedOutput["messages"].([]any)
	if !ok || unprotectedMessages != nil {
		t.Fatalf("gob root-cause probe: messages = %#v (%T), want typed nil slice", unprotectedOutput["messages"], unprotectedOutput["messages"])
	}

	protectedPart := preserveEmptyJSONArrays(part)
	protected := gobCopyMessage(t, a2atype.NewMessage(a2atype.MessageRoleAgent, protectedPart))
	protected = gobCopyMessage(t, protected)
	encoded, err := json.Marshal(protected)
	if err != nil {
		t.Fatalf("marshal preserved message: %v", err)
	}
	var wire map[string]any
	if err := json.Unmarshal(encoded, &wire); err != nil {
		t.Fatalf("unmarshal preserved message: %v", err)
	}
	parts := wire["parts"].([]any)
	data := parts[0].(map[string]any)["data"].(map[string]any)
	output := data["response"].(map[string]any)["output"].(map[string]any)
	assertNonNilEmptyJSONArray(t, output, "messages")
	assertNonNilEmptyJSONArray(t, output, "explanation_codes")
	matches := output["matches"].([]any)
	assertNonNilEmptyJSONArray(t, matches[0].(map[string]any), "matched_on")
	if output["unchanged_null"] != nil {
		t.Fatalf("unchanged_null = %#v, want nil", output["unchanged_null"])
	}
	if output["unchanged_typed_null"] != nil {
		t.Fatalf("unchanged_typed_null = %#v, want JSON null", output["unchanged_typed_null"])
	}
	values := output["unchanged_values"].([]any)
	if len(values) != 1 || values[0] != "one" {
		t.Fatalf("unchanged_values = %#v, want [one]", values)
	}

	legacyJSON, err := json.Marshal(&a2atype.Task{
		ID:        "task-1",
		ContextID: "context-1",
		History:   []*a2atype.Message{protected},
		Status:    a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
	})
	if err != nil {
		t.Fatalf("marshal legacy task: %v", err)
	}
	var legacyTask trpc.Task
	if err := json.Unmarshal(legacyJSON, &legacyTask); err != nil {
		t.Fatalf("unmarshal legacy task: %v", err)
	}
	v1Task, err := trpcv0.ToV1Task(&legacyTask)
	if err != nil {
		t.Fatalf("convert legacy task to v1: %v", err)
	}
	roundTripTask, err := trpcv0.ToLegacyTask(v1Task)
	if err != nil {
		t.Fatalf("convert v1 task to legacy: %v", err)
	}
	roundTripJSON, err := json.Marshal(roundTripTask)
	if err != nil {
		t.Fatalf("marshal compatibility round trip: %v", err)
	}
	var roundTripWire map[string]any
	if err := json.Unmarshal(roundTripJSON, &roundTripWire); err != nil {
		t.Fatalf("unmarshal compatibility round trip: %v", err)
	}
	history := roundTripWire["history"].([]any)
	roundTripParts := history[0].(map[string]any)["parts"].([]any)
	roundTripData := roundTripParts[0].(map[string]any)["data"].(map[string]any)
	roundTripOutput := roundTripData["response"].(map[string]any)["output"].(map[string]any)
	assertNonNilEmptyJSONArray(t, roundTripOutput, "messages")
	assertNonNilEmptyJSONArray(t, roundTripOutput, "explanation_codes")
}

func gobCopyMessage(t *testing.T, message *a2atype.Message) *a2atype.Message {
	t.Helper()
	var buffer bytes.Buffer
	if err := gob.NewEncoder(&buffer).Encode(message); err != nil {
		t.Fatalf("gob encode message: %v", err)
	}
	var copied a2atype.Message
	if err := gob.NewDecoder(&buffer).Decode(&copied); err != nil {
		t.Fatalf("gob decode message: %v", err)
	}
	return &copied
}

func functionResponseOutput(t *testing.T, message *a2atype.Message) map[string]any {
	t.Helper()
	if len(message.Parts) != 1 {
		t.Fatalf("parts = %d, want 1", len(message.Parts))
	}
	part := asDataPart(message.Parts[0])
	if part == nil {
		t.Fatalf("part type = %T, want DataPart", message.Parts[0])
	}
	return part.Data["response"].(map[string]any)["output"].(map[string]any)
}

func assertNonNilEmptyJSONArray(t *testing.T, values map[string]any, key string) {
	t.Helper()
	value, ok := values[key].([]any)
	if !ok || value == nil || len(value) != 0 {
		t.Fatalf("%s = %#v (%T), want non-nil empty JSON array", key, values[key], values[key])
	}
}
