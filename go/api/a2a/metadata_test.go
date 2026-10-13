package a2a

import (
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
)

func TestSanitizeCallerRequest(t *testing.T) {
	part := NewToolResultPart("call-1", "lookup", map[string]any{"result": "ok"})
	part.SetMeta(outputSchemaSHA256MetadataKey, "caller-controlled")
	message := a2atype.NewMessage(a2atype.MessageRoleUser, part)
	message.Metadata = map[string]any{
		HITLExtensionURI:            map[string]any{"type": HITLTypeToolApprovalResponse},
		TimelinePositionMetadataKey: "caller-controlled",
		"application.example/value": "preserved",
	}
	req := &a2atype.SendMessageRequest{Message: message, Metadata: map[string]any{
		TaskCreatedAtMetadataKey: "caller-controlled",
		"application.example/id": "preserved",
	}}

	SanitizeCallerRequest(req)

	if _, ok := message.Metadata[HITLExtensionURI]; !ok {
		t.Fatal("HITL metadata was removed")
	}
	if _, ok := message.Metadata[TimelinePositionMetadataKey]; ok {
		t.Fatalf("gateway-owned metadata remains: %#v", message.Metadata)
	}
	if req.Metadata["application.example/id"] != "preserved" {
		t.Fatalf("application metadata was removed: %#v", req.Metadata)
	}
	if part.Metadata[PartTypeMetadataKey] != string(ToolResultKind) {
		t.Fatalf("public part type was removed: %#v", part.Metadata)
	}
	if _, ok := part.Metadata[outputSchemaSHA256MetadataKey]; !ok {
		t.Fatalf("non-gateway metadata was removed: %#v", part.Metadata)
	}
}

func TestTimelineAndTaskCreatedAt(t *testing.T) {
	position := time.Date(2026, 9, 22, 12, 0, 0, 123, time.FixedZone("test", -4*60*60))
	message := a2atype.NewMessage(a2atype.MessageRoleAgent)
	SetTimelinePosition(message, position)
	gotPosition, ok := TimelinePosition(message)
	if !ok || !gotPosition.Equal(position) {
		t.Fatalf("timeline position = %v, %v", gotPosition, ok)
	}

	task := &a2atype.Task{}
	SetTaskCreatedAt(task, position)
	gotCreatedAt, ok := TaskCreatedAt(task)
	if !ok || !gotCreatedAt.Equal(position) {
		t.Fatalf("task creation time = %v, %v", gotCreatedAt, ok)
	}
}
