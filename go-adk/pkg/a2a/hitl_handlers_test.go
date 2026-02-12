package a2a

import (
	"context"
	"errors"
	"testing"

	a2aschema "github.com/a2aproject/a2a-go/a2a"
)

type mockEventWriter struct {
	events []a2aschema.Event
	err    error
}

func (m *mockEventWriter) Write(ctx context.Context, event a2aschema.Event) error {
	if m.err != nil {
		return m.err
	}
	m.events = append(m.events, event)
	return nil
}

func TestHandleToolApprovalInterrupt_SingleAction(t *testing.T) {
	eventWriter := &mockEventWriter{}
	infoProvider := &mockTaskInfoProvider{taskID: "task123", contextID: "ctx456"}

	actionRequests := []ToolApprovalRequest{
		{Name: "search", Args: map[string]any{"query": "test"}},
	}

	err := HandleToolApprovalInterrupt(
		context.Background(),
		actionRequests,
		infoProvider,
		eventWriter,
		"test_app",
	)

	if err != nil {
		t.Fatalf("HandleToolApprovalInterrupt() error = %v, want nil", err)
	}

	if len(eventWriter.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(eventWriter.events))
	}

	event, ok := eventWriter.events[0].(*a2aschema.TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("Expected TaskStatusUpdateEvent, got %T", eventWriter.events[0])
	}

	if event.TaskID != "task123" {
		t.Errorf("event.TaskID = %q, want %q", event.TaskID, "task123")
	}
	if event.ContextID != "ctx456" {
		t.Errorf("event.ContextID = %q, want %q", event.ContextID, "ctx456")
	}
	if event.Status.State != a2aschema.TaskStateInputRequired {
		t.Errorf("event.Status.State = %v, want %v", event.Status.State, a2aschema.TaskStateInputRequired)
	}
	if event.Final {
		t.Error("event.Final = true, want false")
	}
	if event.Metadata["interrupt_type"] != KAgentHitlInterruptTypeToolApproval {
		t.Errorf("event.Metadata[interrupt_type] = %v, want %q", event.Metadata["interrupt_type"], KAgentHitlInterruptTypeToolApproval)
	}
}

func TestHandleToolApprovalInterrupt_MultipleActions(t *testing.T) {
	eventWriter := &mockEventWriter{}
	infoProvider := &mockTaskInfoProvider{taskID: "task456", contextID: "ctx789"}

	actionRequests := []ToolApprovalRequest{
		{Name: "tool1", Args: map[string]any{"a": 1}},
		{Name: "tool2", Args: map[string]any{"b": 2}},
	}

	err := HandleToolApprovalInterrupt(
		context.Background(),
		actionRequests,
		infoProvider,
		eventWriter,
		"",
	)

	if err != nil {
		t.Fatalf("HandleToolApprovalInterrupt() error = %v, want nil", err)
	}

	if len(eventWriter.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(eventWriter.events))
	}

	event, ok := eventWriter.events[0].(*a2aschema.TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("Expected TaskStatusUpdateEvent, got %T", eventWriter.events[0])
	}

	var dataPart *a2aschema.DataPart
	for _, part := range event.Status.Message.Parts {
		if dp, ok := part.(*a2aschema.DataPart); ok {
			dataPart = dp
			break
		}
	}

	if dataPart == nil {
		t.Fatal("Expected DataPart with action_requests, got none")
	}

	actionRequestsData, ok := dataPart.Data["action_requests"].([]map[string]any)
	if !ok {
		if arr, ok := dataPart.Data["action_requests"].([]any); ok {
			actionRequestsData = make([]map[string]any, len(arr))
			for i, v := range arr {
				if m, ok := v.(map[string]any); ok {
					actionRequestsData[i] = m
				}
			}
		} else {
			t.Fatalf("Expected action_requests to be []map[string]any, got %T", dataPart.Data["action_requests"])
		}
	}

	if len(actionRequestsData) != 2 {
		t.Errorf("Expected 2 action requests, got %d", len(actionRequestsData))
	}
}

func TestHandleToolApprovalInterrupt_EventWriterError(t *testing.T) {
	eventWriter := &mockEventWriter{
		err: errors.New("write failed"),
	}
	infoProvider := &mockTaskInfoProvider{taskID: "task123", contextID: "ctx456"}

	actionRequests := []ToolApprovalRequest{
		{Name: "test", Args: map[string]any{}},
	}

	err := HandleToolApprovalInterrupt(
		context.Background(),
		actionRequests,
		infoProvider,
		eventWriter,
		"",
	)

	if err == nil {
		t.Error("HandleToolApprovalInterrupt() error = nil, want error")
	}
}
