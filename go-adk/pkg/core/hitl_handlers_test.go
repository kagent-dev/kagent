package core

import (
	"context"
	"errors"
	"testing"
	"time"

	"trpc.group/trpc-go/trpc-a2a-go/protocol"
)

// mockEventQueue is a mock implementation of EventQueue for testing
type mockEventQueue struct {
	events []protocol.Event
	err    error
}

func (m *mockEventQueue) EnqueueEvent(ctx context.Context, event protocol.Event) error {
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
	// Setup mocks
	eventQueue := &mockEventQueue{}
	taskStore := &mockTaskStore{}

	// Test single action
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

	// Verify event was enqueued
	if len(eventQueue.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(eventQueue.events))
	}

	event, ok := eventQueue.events[0].(*protocol.TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("Expected TaskStatusUpdateEvent, got %T", eventQueue.events[0])
	}

	if event.TaskID != "task123" {
		t.Errorf("event.TaskID = %q, want %q", event.TaskID, "task123")
	}
	if event.ContextID != "ctx456" {
		t.Errorf("event.ContextID = %q, want %q", event.ContextID, "ctx456")
	}
	if event.Status.State != protocol.TaskStateInputRequired {
		t.Errorf("event.Status.State = %v, want %v", event.Status.State, protocol.TaskStateInputRequired)
	}
	if event.Final {
		t.Error("event.Final = true, want false")
	}
	if event.Metadata["interrupt_type"] != KAgentHitlInterruptTypeToolApproval {
		t.Errorf("event.Metadata[interrupt_type] = %v, want %q", event.Metadata["interrupt_type"], KAgentHitlInterruptTypeToolApproval)
	}
}

func TestHandleToolApprovalInterrupt_MultipleActions(t *testing.T) {
	// Setup mocks
	eventQueue := &mockEventQueue{}
	taskStore := &mockTaskStore{}

	// Test multiple actions
	actionRequests := []ToolApprovalRequest{
		{Name: "tool1", Args: map[string]interface{}{"a": 1}},
		{Name: "tool2", Args: map[string]interface{}{"b": 2}},
	}

	err := HandleToolApprovalInterrupt(
		context.Background(),
		actionRequests,
		"task456",
		"ctx789",
		eventQueue,
		taskStore,
		"",
	)

	if err != nil {
		t.Fatalf("HandleToolApprovalInterrupt() error = %v, want nil", err)
	}

	// Verify event contains all actions
	if len(eventQueue.events) != 1 {
		t.Fatalf("Expected 1 event, got %d", len(eventQueue.events))
	}

	event, ok := eventQueue.events[0].(*protocol.TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("Expected TaskStatusUpdateEvent, got %T", eventQueue.events[0])
	}

	// Find DataPart with action_requests
	var dataPart *protocol.DataPart
	for _, part := range event.Status.Message.Parts {
		if dp, ok := part.(*protocol.DataPart); ok {
			dataPart = dp
			break
		}
	}

	if dataPart == nil {
		t.Fatal("Expected DataPart with action_requests, got none")
	}

	data, ok := dataPart.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("Expected DataPart.Data to be map, got %T", dataPart.Data)
	}

	actionRequestsData, ok := data["action_requests"].([]map[string]interface{})
	if !ok {
		// Try to convert from []interface{}
		if arr, ok := data["action_requests"].([]interface{}); ok {
			actionRequestsData = make([]map[string]interface{}, len(arr))
			for i, v := range arr {
				if m, ok := v.(map[string]interface{}); ok {
					actionRequestsData[i] = m
				}
			}
		} else {
			t.Fatalf("Expected action_requests to be []map[string]interface{}, got %T", data["action_requests"])
		}
	}

	if len(actionRequestsData) != 2 {
		t.Errorf("Expected 2 action requests, got %d", len(actionRequestsData))
	}
}

func TestHandleToolApprovalInterrupt_Timeout(t *testing.T) {
	// Setup mocks
	eventQueue := &mockEventQueue{}
	taskStore := &mockTaskStore{
		waitForSaveFunc: func(ctx context.Context, taskID string, timeout time.Duration) error {
			return errors.New("timeout")
		},
	}

	actionRequests := []ToolApprovalRequest{
		{Name: "test", Args: map[string]interface{}{}},
	}

	// Should not return error - timeout is caught and logged but not returned
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
		t.Errorf("HandleToolApprovalInterrupt() error = %v, want nil (timeout should be handled gracefully)", err)
	}

	// Event should still be sent even if save times out
	if len(eventQueue.events) != 1 {
		t.Errorf("Expected 1 event even after timeout, got %d", len(eventQueue.events))
	}
}

func TestHandleToolApprovalInterrupt_NoTaskStore(t *testing.T) {
	// Setup mocks
	eventQueue := &mockEventQueue{}
	// No task store (nil)

	actionRequests := []ToolApprovalRequest{
		{Name: "test", Args: map[string]interface{}{}},
	}

	// Should work fine without task store
	err := HandleToolApprovalInterrupt(
		context.Background(),
		actionRequests,
		"task123",
		"ctx456",
		eventQueue,
		nil, // No task store
		"",
	)

	if err != nil {
		t.Fatalf("HandleToolApprovalInterrupt() error = %v, want nil", err)
	}

	// Event should still be sent
	if len(eventQueue.events) != 1 {
		t.Errorf("Expected 1 event, got %d", len(eventQueue.events))
	}
}

func TestHandleToolApprovalInterrupt_EventQueueError(t *testing.T) {
	// Setup mocks
	eventQueue := &mockEventQueue{
		err: errors.New("enqueue failed"),
	}
	taskStore := &mockTaskStore{}

	actionRequests := []ToolApprovalRequest{
		{Name: "test", Args: map[string]interface{}{}},
	}

	// Should return error if event queue fails
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
		t.Error("HandleToolApprovalInterrupt() error = nil, want error")
	}
}
