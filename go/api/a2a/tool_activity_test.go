package a2a

import "testing"

func TestToolActivityParts(t *testing.T) {
	call, ok := ParseToolActivity(NewToolCallPart("call-1", "lookup", map[string]any{"query": "pods"}))
	if !ok || call.Kind != ToolCallKind || call.ID != "call-1" || call.Name != "lookup" {
		t.Fatalf("tool call = %#v, %v", call, ok)
	}

	result, ok := ParseToolActivity(NewToolResultPart("call-1", "lookup", map[string]any{"result": "ok"}))
	if !ok || result.Kind != ToolResultKind || result.Response == nil {
		t.Fatalf("tool result = %#v, %v", result, ok)
	}
}
