package a2a

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/streaming"
	"github.com/kagent-dev/kagent/go/adk/pkg/temporal"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/stretchr/testify/mock"
	temporalmocks "go.temporal.io/sdk/mocks"
)

// testEventQueue captures A2A events written during executor tests.
type testEventQueue struct {
	mu     sync.Mutex
	events []a2atype.Event
}

func (q *testEventQueue) Write(_ context.Context, event a2atype.Event) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.events = append(q.events, event)
	return nil
}

func (q *testEventQueue) WriteVersioned(_ context.Context, event a2atype.Event, _ a2atype.TaskVersion) error {
	return q.Write(context.Background(), event)
}

func (q *testEventQueue) Read(_ context.Context) (a2atype.Event, a2atype.TaskVersion, error) {
	return nil, 0, nil
}

func (q *testEventQueue) Close() error { return nil }

func (q *testEventQueue) getEvents() []a2atype.Event {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]a2atype.Event, len(q.events))
	copy(out, q.events)
	return out
}

func newTestReqCtx() *a2asrv.RequestContext {
	return &a2asrv.RequestContext{
		TaskID:    "task-123",
		ContextID: "session-456",
		Message:   a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.TextPart{Text: "hello"}),
	}
}

func startEmbeddedNATS(t *testing.T) (*natsserver.Server, *nats.Conn) {
	t.Helper()
	opts := &natsserver.Options{
		Host: "127.0.0.1",
		Port: -1,
	}
	ns, err := natsserver.NewServer(opts)
	if err != nil {
		t.Fatalf("Failed to create NATS server: %v", err)
	}
	ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("NATS server not ready")
	}
	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		ns.Shutdown()
		t.Fatalf("Failed to connect to NATS: %v", err)
	}
	t.Cleanup(func() {
		nc.Close()
		ns.Shutdown()
	})
	return ns, nc
}

func TestTemporalExecutor_NilMessage(t *testing.T) {
	exec := NewTemporalExecutor(nil, temporal.TemporalConfig{}, nil, "test-agent", "test-agent", nil, logr.Discard())
	reqCtx := &a2asrv.RequestContext{TaskID: "t1", ContextID: "s1"}
	queue := &testEventQueue{}
	err := exec.Execute(context.Background(), reqCtx, queue)
	if err == nil || err.Error() != "A2A request message cannot be nil" {
		t.Errorf("Expected nil message error, got: %v", err)
	}
}

func TestTemporalExecutor_WorkflowCompleted(t *testing.T) {
	mockClient := &temporalmocks.Client{}
	mockRun := &temporalmocks.WorkflowRun{}

	mockClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockRun, nil)
	mockRun.On("GetID").Return("wf-id")
	mockRun.On("GetRunID").Return("run-id")
	mockRun.On("Get", mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		result := args.Get(1).(*temporal.ExecutionResult)
		*result = temporal.ExecutionResult{
			SessionID: "session-456",
			Status:    "completed",
			Response:  []byte("Agent response text"),
		}
	})

	temporalClient := temporal.NewClientFromExisting(mockClient)
	exec := NewTemporalExecutor(temporalClient, temporal.DefaultTemporalConfig(), nil, "test-agent", "test-agent", []byte(`{}`), logr.Discard())

	reqCtx := newTestReqCtx()
	queue := &testEventQueue{}
	err := exec.Execute(context.Background(), reqCtx, queue)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	events := queue.getEvents()
	if len(events) < 3 {
		t.Fatalf("Expected at least 3 events (submitted, working, final), got %d", len(events))
	}

	// First event: submitted
	if se, ok := events[0].(*a2atype.TaskStatusUpdateEvent); ok {
		if se.Status.State != a2atype.TaskStateSubmitted {
			t.Errorf("Expected submitted state, got %v", se.Status.State)
		}
	} else {
		t.Error("First event is not TaskStatusUpdateEvent")
	}

	// Second: working
	if se, ok := events[1].(*a2atype.TaskStatusUpdateEvent); ok {
		if se.Status.State != a2atype.TaskStateWorking {
			t.Errorf("Expected working state, got %v", se.Status.State)
		}
	}

	// Last: completed final
	last := events[len(events)-1]
	if se, ok := last.(*a2atype.TaskStatusUpdateEvent); ok {
		if se.Status.State != a2atype.TaskStateCompleted {
			t.Errorf("Expected completed state, got %v", se.Status.State)
		}
		if !se.Final {
			t.Error("Expected final event")
		}
	} else {
		t.Error("Last event is not TaskStatusUpdateEvent")
	}

	mockClient.AssertExpectations(t)
	mockRun.AssertExpectations(t)
}

func TestTemporalExecutor_WorkflowFailed(t *testing.T) {
	mockClient := &temporalmocks.Client{}
	mockRun := &temporalmocks.WorkflowRun{}

	mockClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockRun, nil)
	mockRun.On("GetID").Return("wf-id")
	mockRun.On("GetRunID").Return("run-id")
	mockRun.On("Get", mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		result := args.Get(1).(*temporal.ExecutionResult)
		*result = temporal.ExecutionResult{
			SessionID: "session-456",
			Status:    "failed",
			Reason:    "LLM timeout",
		}
	})

	temporalClient := temporal.NewClientFromExisting(mockClient)
	exec := NewTemporalExecutor(temporalClient, temporal.DefaultTemporalConfig(), nil, "test-agent", "test-agent", []byte(`{}`), logr.Discard())

	reqCtx := newTestReqCtx()
	queue := &testEventQueue{}
	err := exec.Execute(context.Background(), reqCtx, queue)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	events := queue.getEvents()
	last := events[len(events)-1]
	if se, ok := last.(*a2atype.TaskStatusUpdateEvent); ok {
		if se.Status.State != a2atype.TaskStateFailed {
			t.Errorf("Expected failed state, got %v", se.Status.State)
		}
		if !se.Final {
			t.Error("Expected final event")
		}
	}
}

func TestTemporalExecutor_WorkflowRejected(t *testing.T) {
	mockClient := &temporalmocks.Client{}
	mockRun := &temporalmocks.WorkflowRun{}

	mockClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockRun, nil)
	mockRun.On("GetID").Return("wf-id")
	mockRun.On("GetRunID").Return("run-id")
	mockRun.On("Get", mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		result := args.Get(1).(*temporal.ExecutionResult)
		*result = temporal.ExecutionResult{
			SessionID: "session-456",
			Status:    "rejected",
			Reason:    "User declined",
		}
	})

	temporalClient := temporal.NewClientFromExisting(mockClient)
	exec := NewTemporalExecutor(temporalClient, temporal.DefaultTemporalConfig(), nil, "test-agent", "test-agent", []byte(`{}`), logr.Discard())

	reqCtx := newTestReqCtx()
	queue := &testEventQueue{}
	err := exec.Execute(context.Background(), reqCtx, queue)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	events := queue.getEvents()
	last := events[len(events)-1]
	if se, ok := last.(*a2atype.TaskStatusUpdateEvent); ok {
		if se.Status.State != a2atype.TaskStateCanceled {
			t.Errorf("Expected canceled state for rejection, got %v", se.Status.State)
		}
	}
}

func TestTemporalExecutor_StartWorkflowError(t *testing.T) {
	mockClient := &temporalmocks.Client{}
	mockClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(nil, errors.New("connection refused"))

	temporalClient := temporal.NewClientFromExisting(mockClient)
	exec := NewTemporalExecutor(temporalClient, temporal.DefaultTemporalConfig(), nil, "test-agent", "test-agent", []byte(`{}`), logr.Discard())

	reqCtx := newTestReqCtx()
	queue := &testEventQueue{}
	err := exec.Execute(context.Background(), reqCtx, queue)
	if err == nil {
		t.Fatal("Expected error when workflow start fails")
	}

	events := queue.getEvents()
	// Should have submitted + failed events
	foundFailed := false
	for _, ev := range events {
		if se, ok := ev.(*a2atype.TaskStatusUpdateEvent); ok && se.Status.State == a2atype.TaskStateFailed {
			foundFailed = true
		}
	}
	if !foundFailed {
		t.Error("Expected a failed status event")
	}
}

func TestTemporalExecutor_WorkflowGetError(t *testing.T) {
	mockClient := &temporalmocks.Client{}
	mockRun := &temporalmocks.WorkflowRun{}

	mockClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockRun, nil)
	mockRun.On("GetID").Return("wf-id")
	mockRun.On("GetRunID").Return("run-id")
	mockRun.On("Get", mock.Anything, mock.Anything).Return(errors.New("workflow timeout"))

	temporalClient := temporal.NewClientFromExisting(mockClient)
	exec := NewTemporalExecutor(temporalClient, temporal.DefaultTemporalConfig(), nil, "test-agent", "test-agent", []byte(`{}`), logr.Discard())

	reqCtx := newTestReqCtx()
	queue := &testEventQueue{}
	err := exec.Execute(context.Background(), reqCtx, queue)
	if err == nil {
		t.Fatal("Expected error when workflow Get fails")
	}

	events := queue.getEvents()
	foundFailed := false
	for _, ev := range events {
		if se, ok := ev.(*a2atype.TaskStatusUpdateEvent); ok && se.Status.State == a2atype.TaskStateFailed {
			foundFailed = true
		}
	}
	if !foundFailed {
		t.Error("Expected a failed status event")
	}
}

func TestTemporalExecutor_NATSStreaming(t *testing.T) {
	_, nc := startEmbeddedNATS(t)

	mockClient := &temporalmocks.Client{}
	mockRun := &temporalmocks.WorkflowRun{}

	subject := streaming.SubjectForAgent("test-agent", "session-456")

	mockClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockRun, nil)
	mockRun.On("GetID").Return("wf-id")
	mockRun.On("GetRunID").Return("run-id")
	mockRun.On("Get", mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		// Simulate publishing NATS events before workflow completes.
		pub := streaming.NewStreamPublisher(nc)
		tokenEvent := streaming.NewStreamEvent(streaming.EventTypeToken, "Hello")
		_ = pub.PublishToken(subject, tokenEvent)
		toolStart := streaming.NewStreamEvent(streaming.EventTypeToolStart, "search")
		_ = pub.PublishToolProgress(subject, toolStart)
		toolEnd := streaming.NewStreamEvent(streaming.EventTypeToolEnd, "search")
		_ = pub.PublishToolProgress(subject, toolEnd)
		// Give time for NATS messages to be delivered.
		time.Sleep(50 * time.Millisecond)

		result := args.Get(1).(*temporal.ExecutionResult)
		*result = temporal.ExecutionResult{
			SessionID: "session-456",
			Status:    "completed",
			Response:  []byte("done"),
		}
	})

	temporalClient := temporal.NewClientFromExisting(mockClient)
	exec := NewTemporalExecutor(temporalClient, temporal.DefaultTemporalConfig(), nc, "test-agent", "test-agent", []byte(`{}`), logr.Discard())

	reqCtx := newTestReqCtx()
	queue := &testEventQueue{}
	err := exec.Execute(context.Background(), reqCtx, queue)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	events := queue.getEvents()
	// Should have: submitted, working, token, tool_start, tool_end, completed
	if len(events) < 4 {
		t.Errorf("Expected at least 4 events with streaming, got %d", len(events))
	}

	// Verify we got streaming events (working state with content)
	workingCount := 0
	for _, ev := range events {
		if se, ok := ev.(*a2atype.TaskStatusUpdateEvent); ok && se.Status.State == a2atype.TaskStateWorking && se.Status.Message != nil {
			workingCount++
		}
	}
	if workingCount < 1 {
		t.Error("Expected at least 1 streaming working event from NATS")
	}
}

func TestTemporalExecutor_Cancel(t *testing.T) {
	exec := NewTemporalExecutor(nil, temporal.TemporalConfig{}, nil, "test-agent", "test-agent", nil, logr.Discard())
	reqCtx := newTestReqCtx()
	queue := &testEventQueue{}
	err := exec.Cancel(context.Background(), reqCtx, queue)
	if err != nil {
		t.Fatalf("Cancel returned error: %v", err)
	}

	events := queue.getEvents()
	if len(events) != 1 {
		t.Fatalf("Expected 1 cancel event, got %d", len(events))
	}
	if se, ok := events[0].(*a2atype.TaskStatusUpdateEvent); ok {
		if se.Status.State != a2atype.TaskStateCanceled {
			t.Errorf("Expected canceled state, got %v", se.Status.State)
		}
		if !se.Final {
			t.Error("Expected final event")
		}
	}
}

func TestTemporalExecutor_ForwardApprovalRequest(t *testing.T) {
	_, nc := startEmbeddedNATS(t)

	mockClient := &temporalmocks.Client{}
	mockRun := &temporalmocks.WorkflowRun{}

	subject := streaming.SubjectForAgent("test-agent", "session-456")

	mockClient.On("ExecuteWorkflow", mock.Anything, mock.Anything, mock.Anything, mock.Anything).Return(mockRun, nil)
	mockRun.On("GetID").Return("wf-id")
	mockRun.On("GetRunID").Return("run-id")
	mockRun.On("Get", mock.Anything, mock.Anything).Return(nil).Run(func(args mock.Arguments) {
		pub := streaming.NewStreamPublisher(nc)
		approvalData, _ := json.Marshal(map[string]string{"tool": "dangerous_tool"})
		approvalEvent := streaming.NewStreamEvent(streaming.EventTypeApprovalRequest, string(approvalData))
		_ = pub.PublishToken(subject, approvalEvent)
		time.Sleep(50 * time.Millisecond)

		result := args.Get(1).(*temporal.ExecutionResult)
		*result = temporal.ExecutionResult{Status: "completed"}
	})

	temporalClient := temporal.NewClientFromExisting(mockClient)
	exec := NewTemporalExecutor(temporalClient, temporal.DefaultTemporalConfig(), nc, "test-agent", "test-agent", []byte(`{}`), logr.Discard())

	reqCtx := newTestReqCtx()
	queue := &testEventQueue{}
	err := exec.Execute(context.Background(), reqCtx, queue)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}

	events := queue.getEvents()
	foundApproval := false
	for _, ev := range events {
		if se, ok := ev.(*a2atype.TaskStatusUpdateEvent); ok && se.Status.State == a2atype.TaskStateInputRequired {
			foundApproval = true
		}
	}
	if !foundApproval {
		t.Error("Expected an input_required event for approval request")
	}
}

