package a2a

import (
	"context"
	"errors"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/a2a"
)

// mockEventQueue is a mock implementation of EventQueue for testing
type mockEventQueue struct {
	events []a2atype.Event
	err    error
}

func (m *mockEventQueue) EnqueueEvent(ctx context.Context, event a2atype.Event) error {
	if m.err != nil {
		return m.err
	}
	m.events = append(m.events, event)
	return nil
}

// mockTaskStore is a mock implementation of TaskStore for testing
type mockTaskStore struct {
	waitForSaveFunc func(ctx context.Context, taskID string, timeout time.Duration) error
}

func (m *mockTaskStore) WaitForSave(ctx context.Context, taskID string, timeout time.Duration) error {
	if m.waitForSaveFunc != nil {
		return m.waitForSaveFunc(ctx, taskID, timeout)
	}
	return nil
}

func TestHandleToolApprovalInterrupt_SingleAction(t *testing.T) {
	eventQueue := &mockEventQueue{}
	taskStore := &mockTaskStore{}

	actionRequests := []ToolApprovalRequest{
		{Name: "search", Args: map[string]interface{}{"query": "test"}},
	}

	err := HandleToolApprovalInterrupt(
		context.Background(),
		actionRequests,
		"task123",
		"ctx456",
		eventQueue,
		taskStore,
		"test_app",
	)

	if err != nil {
		t.Fatalf("HandleToolApprovalInterrupt() error = %v, want nil", err)
	}

	if len(eventQueue.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(eventQueue.events))
	}

	event, ok := eventQueue.events[0].(*a2atype.TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("Expected TaskStatusUpdateEvent, got %T", eventQueue.events[0])
	}

	if event.TaskID != "task123" {
		t.Errorf("event.TaskID = %q, want %q", event.TaskID, "task123")
	}
	if event.ContextID != "ctx456" {
		t.Errorf("event.ContextID = %q, want %q", event.ContextID, "ctx456")
	}
	if event.Status.State != a2atype.TaskStateInputRequired {
		t.Errorf("event.Status.State = %v, want %v", event.Status.State, a2atype.TaskStateInputRequired)
	}
	if event.Final {
		t.Error("event.Final = true, want false")
	}
	if event.Metadata["interrupt_type"] != KAgentHitlInterruptTypeToolApproval {
		t.Errorf("event.Metadata[interrupt_type] = %v, want %q", event.Metadata["interrupt_type"], KAgentHitlInterruptTypeToolApproval)
	}
}

func TestHandleToolApprovalInterrupt_MultipleActions(t *testing.T) {
	eventQueue := &mockEventQueue{}
	taskStore := &mockTaskStore{}

	actionRequests := []ToolApprovalRequest{
		{Name: "tool1", Args: map[string]interface{}{"a": 1}},
		{Name: "tool2", Args: map[string]interface{}{"b": 2}},
	}

	err := HandleToolApprovalInterrupt(
		context.Background(),
		actionRequests,
		"task123",
		"ctx456",
		eventQueue,
		taskStore,
		"test_app",
	)

	if err != nil {
		t.Fatalf("HandleToolApprovalInterrupt() error = %v, want nil", err)
	}

	if len(eventQueue.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(eventQueue.events))
	}

	event, ok := eventQueue.events[0].(*a2atype.TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("Expected TaskStatusUpdateEvent, got %T", eventQueue.events[0])
	}

	if event.TaskID != "task123" {
		t.Errorf("event.TaskID = %q, want %q", event.TaskID, "task123")
	}
}

func TestHandleToolApprovalInterrupt_WithTaskStore(t *testing.T) {
	eventQueue := &mockEventQueue{}
	saveCalled := false
	taskStore := &mockTaskStore{
		waitForSaveFunc: func(ctx context.Context, taskID string, timeout time.Duration) error {
			saveCalled = true
			if taskID != "task123" {
				t.Errorf("WaitForSave taskID = %q, want task123", taskID)
			}
			return nil
		},
	}

	actionRequests := []ToolApprovalRequest{
		{Name: "run", Args: map[string]interface{}{}},
	}

	err := HandleToolApprovalInterrupt(
		context.Background(),
		actionRequests,
		"task123",
		"ctx456",
		eventQueue,
		taskStore,
		"",
	)

	if err != nil {
		t.Fatalf("HandleToolApprovalInterrupt() error = %v, want nil", err)
	}
	if !saveCalled {
		t.Error("Expected WaitForSave to be called")
	}
}

func TestHandleToolApprovalInterrupt_EventQueueError(t *testing.T) {
	eventQueue := &mockEventQueue{
		err: errors.New("enqueue failed"),
	}
	taskStore := &mockTaskStore{}

	actionRequests := []ToolApprovalRequest{
		{Name: "tool1", Args: map[string]interface{}{}},
	}

	err := HandleToolApprovalInterrupt(
		context.Background(),
		actionRequests,
		"task123",
		"ctx456",
		eventQueue,
		taskStore,
		"",
	)

	if err == nil {
		t.Fatal("HandleToolApprovalInterrupt() error = nil, want non-nil")
	}
	if !errors.Is(err, eventQueue.err) && err.Error() == "" {
		t.Errorf("Expected error about enqueue failure, got %v", err)
	}
}

func TestHandleToolApprovalInterrupt_NoTaskStore(t *testing.T) {
	eventQueue := &mockEventQueue{}

	actionRequests := []ToolApprovalRequest{
		{Name: "tool1", Args: map[string]interface{}{}},
	}

	err := HandleToolApprovalInterrupt(
		context.Background(),
		actionRequests,
		"task123",
		"ctx456",
		eventQueue,
		nil,
		"",
	)

	if err != nil {
		t.Fatalf("HandleToolApprovalInterrupt() with nil taskStore error = %v, want nil", err)
	}
}
