package a2a

import (
	"context"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/a2aproject/a2a-go/a2asrv/eventqueue"
)

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

type captureExecutor struct {
	executed bool
	message  *a2atype.Message
	queue    eventqueue.Queue
}

func (e *captureExecutor) Execute(_ context.Context, reqCtx *a2asrv.RequestContext, queue eventqueue.Queue) error {
	e.executed = true
	e.message = reqCtx.Message
	e.queue = queue
	return nil
}

func (e *captureExecutor) Cancel(_ context.Context, _ *a2asrv.RequestContext, _ eventqueue.Queue) error {
	return nil
}

// recordingQueue captures all events written to it for later inspection.
type recordingQueue struct {
	events []a2atype.Event
}

func (q *recordingQueue) Write(_ context.Context, ev a2atype.Event) error {
	q.events = append(q.events, ev)
	return nil
}

func (q *recordingQueue) WriteVersioned(_ context.Context, ev a2atype.Event, _ a2atype.TaskVersion) error {
	q.events = append(q.events, ev)
	return nil
}

func (q *recordingQueue) Read(_ context.Context) (a2atype.Event, a2atype.TaskVersion, error) {
	return nil, a2atype.TaskVersion(0), nil
}

func (q *recordingQueue) Close() error { return nil }

type noopQueue struct{}

func (q *noopQueue) Write(_ context.Context, _ a2atype.Event) error { return nil }
func (q *noopQueue) WriteVersioned(_ context.Context, _ a2atype.Event, _ a2atype.TaskVersion) error {
	return nil
}
func (q *noopQueue) Read(_ context.Context) (a2atype.Event, a2atype.TaskVersion, error) {
	return nil, a2atype.TaskVersion(0), nil
}
func (q *noopQueue) Close() error { return nil }

func newReqCtx() *a2asrv.RequestContext {
	return &a2asrv.RequestContext{
		Message:   a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.TextPart{Text: "hi"}),
		TaskID:    "task-1",
		ContextID: "ctx-1",
	}
}

// ---------------------------------------------------------------------------
// WrapExecutorQueue tests
// ---------------------------------------------------------------------------

func TestWrapExecutorQueue_PassesThroughNonHitlMessages(t *testing.T) {
	original := a2atype.NewMessage(
		a2atype.MessageRoleUser,
		a2atype.TextPart{Text: "hello"},
	)
	reqCtx := &a2asrv.RequestContext{
		Message:   original,
		TaskID:    "task_plain",
		ContextID: "ctx_plain",
	}

	inner := &captureExecutor{}
	wrapped := WrapExecutorQueue(inner)
	if err := wrapped.Execute(context.Background(), reqCtx, &noopQueue{}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if inner.message != original {
		t.Fatalf("non-HITL message was unexpectedly rewritten: got %#v want %#v", inner.message, original)
	}
}

func TestWrapExecutorQueue_WrapsQueueAsEventQueue(t *testing.T) {
	reqCtx := &a2asrv.RequestContext{
		Message:   a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.TextPart{Text: "test"}),
		TaskID:    "task-wrap",
		ContextID: "ctx-wrap",
	}

	inner := &captureExecutor{}
	wrapped := WrapExecutorQueue(inner)
	if err := wrapped.Execute(context.Background(), reqCtx, &noopQueue{}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	if _, ok := inner.queue.(*EventQueue); !ok {
		t.Fatalf("wrapped queue type = %T, want *EventQueue", inner.queue)
	}
}

func TestWrapExecutorQueue_RejectsNilMessage(t *testing.T) {
	reqCtx := &a2asrv.RequestContext{
		Message:   nil,
		TaskID:    "task-nil",
		ContextID: "ctx-nil",
	}

	inner := &captureExecutor{}
	wrapped := WrapExecutorQueue(inner)
	err := wrapped.Execute(context.Background(), reqCtx, &noopQueue{})
	if err == nil {
		t.Fatal("expected error for nil message, got nil")
	}
}

// ---------------------------------------------------------------------------
// EventQueue.Write tests — artifact mirroring
// ---------------------------------------------------------------------------

func TestEventQueue_ArtifactMirroredAsStatus(t *testing.T) {
	rec := &recordingQueue{}
	q := NewEventQueue(rec, newReqCtx())

	artifact := &a2atype.TaskArtifactUpdateEvent{
		Artifact: &a2atype.Artifact{
			Parts: a2atype.ContentParts{
				a2atype.TextPart{Text: "streaming chunk"},
			},
		},
	}

	if err := q.Write(context.Background(), artifact); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Should produce 2 events: mirror status + original artifact
	if len(rec.events) != 2 {
		t.Fatalf("expected 2 events (mirror + artifact), got %d", len(rec.events))
	}

	// First event should be a status update (the mirror)
	if _, ok := rec.events[0].(*a2atype.TaskStatusUpdateEvent); !ok {
		t.Errorf("events[0] type = %T, want *TaskStatusUpdateEvent", rec.events[0])
	}

	// Second event should be the original artifact
	if _, ok := rec.events[1].(*a2atype.TaskArtifactUpdateEvent); !ok {
		t.Errorf("events[1] type = %T, want *TaskArtifactUpdateEvent", rec.events[1])
	}
}

func TestEventQueue_EmptyArtifactDropped(t *testing.T) {
	rec := &recordingQueue{}
	q := NewEventQueue(rec, newReqCtx())

	// An artifact with only empty DataParts should be silently dropped.
	artifact := &a2atype.TaskArtifactUpdateEvent{
		Artifact: &a2atype.Artifact{
			Parts: a2atype.ContentParts{
				a2atype.DataPart{Data: nil},
			},
		},
	}

	if err := q.Write(context.Background(), artifact); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if len(rec.events) != 0 {
		t.Fatalf("expected 0 events (empty artifact dropped), got %d", len(rec.events))
	}
}

func TestEventQueue_LastChunkArtifactNotMirrored(t *testing.T) {
	rec := &recordingQueue{}
	q := NewEventQueue(rec, newReqCtx())

	// LastChunk artifacts should be written but NOT mirrored as status.
	artifact := &a2atype.TaskArtifactUpdateEvent{
		LastChunk: true,
		Artifact: &a2atype.Artifact{
			Parts: a2atype.ContentParts{
				a2atype.TextPart{Text: "final"},
			},
		},
	}

	if err := q.Write(context.Background(), artifact); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	// Should produce only the original artifact, no mirror
	if len(rec.events) != 1 {
		t.Fatalf("expected 1 event (artifact only), got %d", len(rec.events))
	}
	if _, ok := rec.events[0].(*a2atype.TaskArtifactUpdateEvent); !ok {
		t.Errorf("events[0] type = %T, want *TaskArtifactUpdateEvent", rec.events[0])
	}
}

// ---------------------------------------------------------------------------
// EventQueue.Write tests — final status with last text injection
// ---------------------------------------------------------------------------

func TestEventQueue_FinalStatusInjectsLastText(t *testing.T) {
	rec := &recordingQueue{}
	q := NewEventQueue(rec, newReqCtx())

	// First, write a non-partial artifact to populate lastTextParts.
	artifact := &a2atype.TaskArtifactUpdateEvent{
		Artifact: &a2atype.Artifact{
			Parts: a2atype.ContentParts{
				a2atype.TextPart{Text: "the answer is 42"},
			},
		},
	}
	if err := q.Write(context.Background(), artifact); err != nil {
		t.Fatalf("Write(artifact) error = %v", err)
	}

	rec.events = nil // reset

	// Now write a final status with no message.
	finalStatus := &a2atype.TaskStatusUpdateEvent{
		Final:  true,
		Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted},
	}
	if err := q.Write(context.Background(), finalStatus); err != nil {
		t.Fatalf("Write(status) error = %v", err)
	}

	if len(rec.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(rec.events))
	}

	status := rec.events[0].(*a2atype.TaskStatusUpdateEvent)
	if status.Status.Message == nil {
		t.Fatal("expected final status to have injected message, got nil")
	}

	// The injected message should contain the last text.
	found := false
	for _, p := range status.Status.Message.Parts {
		if tp, ok := p.(a2atype.TextPart); ok && tp.Text == "the answer is 42" {
			found = true
		}
	}
	if !found {
		t.Error("final status message does not contain last artifact text")
	}
}

func TestEventQueue_FinalStatusWithExistingMessageNotOverwritten(t *testing.T) {
	rec := &recordingQueue{}
	q := NewEventQueue(rec, newReqCtx())

	// Write an artifact first.
	artifact := &a2atype.TaskArtifactUpdateEvent{
		Artifact: &a2atype.Artifact{
			Parts: a2atype.ContentParts{
				a2atype.TextPart{Text: "should not appear"},
			},
		},
	}
	if err := q.Write(context.Background(), artifact); err != nil {
		t.Fatalf("Write(artifact) error = %v", err)
	}

	rec.events = nil

	// Final status already has a message — should NOT be overwritten.
	existingMsg := a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.TextPart{Text: "explicit message"})
	finalStatus := &a2atype.TaskStatusUpdateEvent{
		Final:  true,
		Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted, Message: existingMsg},
	}
	if err := q.Write(context.Background(), finalStatus); err != nil {
		t.Fatalf("Write(status) error = %v", err)
	}

	status := rec.events[0].(*a2atype.TaskStatusUpdateEvent)
	if status.Status.Message != existingMsg {
		t.Error("final status message was overwritten despite already having one")
	}
}

// ---------------------------------------------------------------------------
// EventQueue.Write tests — passthrough for other event types
// ---------------------------------------------------------------------------

func TestEventQueue_StatusEventPassedThrough(t *testing.T) {
	rec := &recordingQueue{}
	q := NewEventQueue(rec, newReqCtx())

	status := &a2atype.TaskStatusUpdateEvent{
		Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking},
	}
	if err := q.Write(context.Background(), status); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	if len(rec.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(rec.events))
	}
	if rec.events[0] != status {
		t.Error("status event was not passed through as-is")
	}
}

// ---------------------------------------------------------------------------
// Part filter tests
// ---------------------------------------------------------------------------

func TestFilterNonEmptyParts(t *testing.T) {
	parts := a2atype.ContentParts{
		a2atype.TextPart{Text: "hello"},
		a2atype.DataPart{Data: nil},
		a2atype.DataPart{Data: map[string]any{"key": "value"}},
		a2atype.DataPart{Data: map[string]any{}},
	}

	filtered := filterNonEmptyParts(parts)
	if len(filtered) != 2 {
		t.Fatalf("expected 2 non-empty parts, got %d", len(filtered))
	}

	if tp, ok := filtered[0].(a2atype.TextPart); !ok || tp.Text != "hello" {
		t.Errorf("filtered[0] = %v, want TextPart{hello}", filtered[0])
	}
	if dp, ok := filtered[1].(a2atype.DataPart); !ok || dp.Data["key"] != "value" {
		t.Errorf("filtered[1] = %v, want DataPart with key=value", filtered[1])
	}
}

func TestFilterTextParts(t *testing.T) {
	parts := a2atype.ContentParts{
		a2atype.TextPart{Text: "hello"},
		a2atype.DataPart{Data: map[string]any{"x": 1}},
		a2atype.TextPart{Text: "world"},
	}

	textOnly := filterTextParts(parts)
	if len(textOnly) != 2 {
		t.Fatalf("expected 2 text parts, got %d", len(textOnly))
	}
	if tp, ok := textOnly[0].(a2atype.TextPart); !ok || tp.Text != "hello" {
		t.Errorf("textOnly[0] = %v, want TextPart{hello}", textOnly[0])
	}
	if tp, ok := textOnly[1].(a2atype.TextPart); !ok || tp.Text != "world" {
		t.Errorf("textOnly[1] = %v, want TextPart{world}", textOnly[1])
	}
}

func TestIsEmptyDataPart(t *testing.T) {
	tests := []struct {
		name string
		part a2atype.Part
		want bool
	}{
		{"nil data DataPart", a2atype.DataPart{Data: nil}, true},
		{"empty data DataPart", a2atype.DataPart{Data: map[string]any{}}, true},
		{"non-empty DataPart", a2atype.DataPart{Data: map[string]any{"k": "v"}}, false},
		{"TextPart", a2atype.TextPart{Text: "hi"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isEmptyDataPart(tt.part); got != tt.want {
				t.Errorf("isEmptyDataPart() = %v, want %v", got, tt.want)
			}
		})
	}
}
