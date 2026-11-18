package a2a

import "testing"

func TestToolActivityParts(t *testing.T) {
	part, err := NewToolCallPart("call-1", "lookup", "", map[string]any{"query": "pods"})
	if err != nil {
		t.Fatal(err)
	}
	call, ok := ParseToolActivity(part)
	if !ok || call.Kind != ToolCallKind || call.ID != "call-1" || call.Name != "lookup" {
		t.Fatalf("tool call = %#v, %v", call, ok)
	}

	response, err := NewToolResponse(map[string]any{"result": "ok"}, false)
	if err != nil {
		t.Fatal(err)
	}
	part, err = NewToolResultPart("call-1", "lookup", "", response)
	if err != nil {
		t.Fatal(err)
	}
	result, ok := ParseToolActivity(part)
	if !ok || result.Kind != ToolResultKind || result.Response == nil {
		t.Fatalf("tool result = %#v, %v", result, ok)
	}
}
