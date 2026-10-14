package a2a

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

const (
	testContextID = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	testSessionID = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
)

type fakeRunner struct {
	run func(context.Context, runtime.Turn, runtime.EventSink) (runtime.Outcome, error)
}

func (f fakeRunner) Run(ctx context.Context, turn runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
	return f.run(ctx, turn, sink)
}

type fakePendingTurn struct {
	request runtime.InputRequest
	resume  func(context.Context, runtime.InputResponse, runtime.EventSink) (runtime.Outcome, error)
	cancel  func(context.Context, runtime.ModelCallSink) error
}

func (f *fakePendingTurn) Request() runtime.InputRequest { return f.request }

func (f *fakePendingTurn) Resume(ctx context.Context, response runtime.InputResponse, sink runtime.EventSink) (runtime.Outcome, error) {
	return f.resume(ctx, response, sink)
}

func (f *fakePendingTurn) Cancel(ctx context.Context, sink runtime.ModelCallSink) error {
	if f.cancel == nil {
		return nil
	}
	return f.cancel(ctx, sink)
}

type fakeContinuation struct {
	mu   sync.Mutex
	data string
}

func (s *fakeContinuation) Load() (string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.data, s.data != "", nil
}

func (s *fakeContinuation) Bind(continuationID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = continuationID
	return nil
}

func TestExecuteStreamsCompletesAndPersistsSession(t *testing.T) {
	continuation := &fakeContinuation{data: testSessionID}
	var turn runtime.Turn
	executor, err := New(fakeRunner{run: func(_ context.Context, got runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		turn = got
		if err := sink.SessionStarted(runtime.SessionStarted{ContinuationID: testSessionID}); err != nil {
			return runtime.Outcome{}, err
		}
		if err := sink.ToolCall(runtime.ToolCall{ID: "tool-1", Name: "Read", Arguments: map[string]any{"file_path": "/data/workspace/README.md"}}); err != nil {
			return runtime.Outcome{}, err
		}
		if err := sink.ToolResult(runtime.ToolResult{ID: "tool-1", Name: "Read", Result: "contents"}); err != nil {
			return runtime.Outcome{}, err
		}
		if err := sink.ToolCall(runtime.ToolCall{ID: "tool-2", Name: "Edit", Arguments: map[string]any{"file_path": "/data/workspace/missing.md"}}); err != nil {
			return runtime.Outcome{}, err
		}
		if err := sink.ToolResult(runtime.ToolResult{ID: "tool-2", Name: "Edit", Result: "file not found", IsError: true}); err != nil {
			return runtime.Outcome{}, err
		}
		if err := sink.TextDelta(runtime.TextDelta{Text: "hel"}); err != nil {
			return runtime.Outcome{}, err
		}
		if err := sink.TextDelta(runtime.TextDelta{Text: "lo"}); err != nil {
			return runtime.Outcome{}, err
		}
		return runtime.Outcome{}, nil
	}}, continuation, tracing.RuntimeTelemetry{})
	if err != nil {
		t.Fatal(err)
	}
	events, errs := collect(executor.Execute(t.Context(), requestContext("task-1", "hello")))
	if len(errs) != 0 {
		t.Fatalf("Execute() errors = %v", errs)
	}
	if turn.ContinuationID != testSessionID || turn.Prompt != "hello" {
		t.Errorf("turn = %#v", turn)
	}
	var text strings.Builder
	for _, event := range events {
		if update, ok := event.(*a2atype.TaskArtifactUpdateEvent); ok {
			text.WriteString(update.Artifact.Parts[0].Text())
		}
	}
	if text.String() != "hello" {
		t.Errorf("stream text = %q", text.String())
	}
	assertToolActivity(t, events, "function_call", map[string]any{
		"id": "tool-1", "name": "Read", "args": map[string]any{"file_path": "/data/workspace/README.md"},
	})
	assertToolActivity(t, events, "function_response", map[string]any{
		"id": "tool-1", "name": "Read", "response": map[string]any{"result": "contents"},
	})
	assertToolActivity(t, events, "function_response", map[string]any{
		"id": "tool-2", "name": "Edit", "response": map[string]any{"result": "file not found", "isError": true},
	})
	last := events[len(events)-1].(*a2atype.TaskStatusUpdateEvent)
	if last.Status.State != a2atype.TaskStateCompleted {
		t.Errorf("last state = %s", last.Status.State)
	}
}

func TestExecutePreservesTextAndToolOrder(t *testing.T) {
	executor, err := New(fakeRunner{run: func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
		steps := []func() error{
			func() error { return sink.TextDelta(runtime.TextDelta{Text: "before "}) },
			func() error { return sink.TextDelta(runtime.TextDelta{Text: "tool"}) },
			func() error { return sink.ToolCall(runtime.ToolCall{ID: "tool-1", Name: "Read"}) },
			func() error {
				return sink.ToolResult(runtime.ToolResult{ID: "tool-1", Name: "Read", Result: "contents"})
			},
			func() error { return sink.TextDelta(runtime.TextDelta{Text: "after "}) },
			func() error { return sink.TextDelta(runtime.TextDelta{Text: "tool"}) },
		}
		for _, step := range steps {
			if err := step(); err != nil {
				return runtime.Outcome{}, err
			}
		}
		return runtime.Outcome{}, nil
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	if err != nil {
		t.Fatal(err)
	}

	events, errs := collect(executor.Execute(t.Context(), requestContext("task-1", "hello")))
	if len(errs) != 0 {
		t.Fatalf("Execute() errors = %v", errs)
	}
	if len(events) != 8 {
		t.Fatalf("events = %d, want working, two text runs, tool call/result, and completion", len(events))
	}

	first := events[1].(*a2atype.TaskArtifactUpdateEvent)
	firstAppend := events[2].(*a2atype.TaskArtifactUpdateEvent)
	call := events[3].(*a2atype.TaskArtifactUpdateEvent)
	result := events[4].(*a2atype.TaskArtifactUpdateEvent)
	second := events[5].(*a2atype.TaskArtifactUpdateEvent)
	secondAppend := events[6].(*a2atype.TaskArtifactUpdateEvent)
	if first.Artifact.ID == second.Artifact.ID {
		t.Fatalf("text around tool activity reused artifact %q", first.Artifact.ID)
	}
	ids := map[a2atype.ArtifactID]struct{}{
		first.Artifact.ID: {}, call.Artifact.ID: {}, result.Artifact.ID: {}, second.Artifact.ID: {},
	}
	if len(ids) != 4 {
		t.Fatalf("ordered outputs reused artifact IDs: %#v", ids)
	}
	if !call.LastChunk || !result.LastChunk {
		t.Fatalf("tool artifacts are not closed: call=%v result=%v", call.LastChunk, result.LastChunk)
	}
	if firstAppend.Artifact.ID != first.Artifact.ID || !firstAppend.Append {
		t.Fatalf("first text run append = %#v, want artifact %q append", firstAppend, first.Artifact.ID)
	}
	if secondAppend.Artifact.ID != second.Artifact.ID || !secondAppend.Append {
		t.Fatalf("second text run append = %#v, want artifact %q append", secondAppend, second.Artifact.ID)
	}

	positioned := []map[string]any{
		first.Artifact.Metadata,
		call.Artifact.Metadata,
		result.Artifact.Metadata,
		second.Artifact.Metadata,
	}
	var previous time.Time
	for index, metadata := range positioned {
		value, ok := metadata[apia2a.TimelinePositionMetadataKey].(string)
		position, parseErr := time.Parse(time.RFC3339Nano, value)
		if !ok || parseErr != nil {
			t.Fatalf("event %d timeline position = %#v: %v", index, metadata, parseErr)
		}
		if index > 0 && !position.After(previous) {
			t.Fatalf("event %d position %s is not after %s", index, position, previous)
		}
		previous = position
	}
}

func TestExecuteRejectsMalformedToolActivity(t *testing.T) {
	tests := []func(runtime.EventSink) error{
		func(sink runtime.EventSink) error { return sink.ToolCall(runtime.ToolCall{Name: "Read"}) },
		func(sink runtime.EventSink) error { return sink.ToolResult(runtime.ToolResult{ID: "tool-1"}) },
	}
	for _, emit := range tests {
		executor, err := New(fakeRunner{run: func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
			return runtime.Outcome{}, emit(sink)
		}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
		if err != nil {
			t.Fatal(err)
		}
		events, errs := collect(executor.Execute(t.Context(), requestContext("task-1", "hello")))
		if len(errs) != 0 {
			t.Fatalf("Execute() errors = %v", errs)
		}
		last := events[len(events)-1].(*a2atype.TaskStatusUpdateEvent)
		if last.Status.State != a2atype.TaskStateFailed {
			t.Fatalf("last state = %s, want FAILED", last.Status.State)
		}
	}
}

func assertToolActivity(t *testing.T, events []a2atype.Event, partType string, want map[string]any) {
	t.Helper()
	for _, event := range events {
		update, ok := event.(*a2atype.TaskArtifactUpdateEvent)
		if !ok || update.Artifact == nil || len(update.Artifact.Parts) != 1 {
			continue
		}
		part := update.Artifact.Parts[0]
		if part.Metadata[apia2a.PartTypeMetadataKey] != partType {
			continue
		}
		if got := part.Data(); !reflect.DeepEqual(got, want) {
			continue
		}
		return
	}
	t.Fatalf("did not find %s tool activity", partType)
}

func TestExecuteFailureBoundary(t *testing.T) {
	executor, err := New(fakeRunner{run: func(context.Context, runtime.Turn, runtime.EventSink) (runtime.Outcome, error) {
		return runtime.Outcome{Failure: &runtime.Failure{Message: "budget limit reached"}}, nil
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	if err != nil {
		t.Fatal(err)
	}
	events, errs := collect(executor.Execute(t.Context(), requestContext("task-1", "hello")))
	if len(errs) != 0 {
		t.Fatal(errs)
	}
	last := events[len(events)-1].(*a2atype.TaskStatusUpdateEvent)
	if last.Status.State != a2atype.TaskStateFailed || last.Status.Message.Parts[0].Text() != "budget limit reached" {
		t.Fatalf("failure event = %#v", last)
	}
}

func TestExecuteReleasesActiveTaskBeforeTerminalEvent(t *testing.T) {
	runnerReturned := make(chan struct{})
	executor, err := New(fakeRunner{run: func(context.Context, runtime.Turn, runtime.EventSink) (runtime.Outcome, error) {
		close(runnerReturned)
		return runtime.Outcome{}, nil
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	if err != nil {
		t.Fatal(err)
	}

	for event, err := range executor.Execute(t.Context(), requestContext("task-1", "hello")) {
		if err != nil {
			t.Fatal(err)
		}
		update, ok := event.(*a2atype.TaskStatusUpdateEvent)
		if !ok || !update.Status.State.Terminal() {
			continue
		}
		select {
		case <-runnerReturned:
		default:
			t.Fatal("terminal event was published before the runtime runner returned")
		}
		executor.mu.Lock()
		state := executor.state
		executor.mu.Unlock()
		if state != nil {
			t.Fatal("terminal event was published before the active task was released")
		}
	}
}

func TestBusyAndCancellation(t *testing.T) {
	started := make(chan struct{})
	executor, err := New(fakeRunner{run: func(ctx context.Context, _ runtime.Turn, _ runtime.EventSink) (runtime.Outcome, error) {
		close(started)
		<-ctx.Done()
		return runtime.Outcome{}, ctx.Err()
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	if err != nil {
		t.Fatal(err)
	}
	firstDone := make(chan struct{})
	go func() {
		defer close(firstDone)
		_, _ = collect(executor.Execute(context.Background(), requestContext("task-1", "first")))
	}()
	<-started
	_, errs := collect(executor.Execute(t.Context(), requestContext("task-2", "second")))
	if len(errs) != 1 || !errors.Is(errs[0], errBusy) {
		t.Fatalf("busy errors = %v", errs)
	}
	cancelEvents, cancelErrs := collect(executor.Cancel(t.Context(), requestContext("task-1", "ignored")))
	if len(cancelErrs) != 0 || len(cancelEvents) != 1 {
		t.Fatalf("Cancel() events/errors = %v/%v", cancelEvents, cancelErrs)
	}
	<-firstDone
}

func TestCancellationWinsPendingTurnRace(t *testing.T) {
	started := make(chan struct{})
	pendingCancelStarted := make(chan struct{})
	releasePendingCancel := make(chan struct{})
	executor, err := New(fakeRunner{run: func(ctx context.Context, _ runtime.Turn, _ runtime.EventSink) (runtime.Outcome, error) {
		close(started)
		<-ctx.Done()
		return runtime.Outcome{Pending: &fakePendingTurn{
			request: &runtime.ApprovalRequest{ID: "approval-1", CallID: "call-1", Name: "protected.write"},
			cancel: func(context.Context, runtime.ModelCallSink) error {
				close(pendingCancelStarted)
				<-releasePendingCancel
				return nil
			},
		}}, nil
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	if err != nil {
		t.Fatal(err)
	}

	type executionResult struct {
		events []a2atype.Event
		errs   []error
	}
	executionDone := make(chan executionResult, 1)
	go func() {
		events, errs := collect(executor.Execute(context.Background(), requestContext("task-1", "write")))
		executionDone <- executionResult{events: events, errs: errs}
	}()
	<-started

	cancellationDone := make(chan executionResult, 1)
	go func() {
		events, errs := collect(executor.Cancel(t.Context(), requestContext("task-1", "ignored")))
		cancellationDone <- executionResult{events: events, errs: errs}
	}()
	<-pendingCancelStarted
	select {
	case result := <-cancellationDone:
		t.Fatalf("Cancel() returned before pending cleanup: %#v/%v", result.events, result.errs)
	default:
	}
	_, busyErrs := collect(executor.Execute(t.Context(), requestContext("task-2", "new")))
	if len(busyErrs) != 1 || !errors.Is(busyErrs[0], errBusy) {
		t.Fatalf("execution during pending cleanup errors = %v, want errBusy", busyErrs)
	}
	close(releasePendingCancel)
	cancellation := <-cancellationDone
	result := <-executionDone
	if len(cancellation.errs) != 0 || len(cancellation.events) != 1 {
		t.Fatalf("Cancel() events/errors = %#v/%v", cancellation.events, cancellation.errs)
	}
	if len(result.errs) != 0 || len(result.events) != 1 {
		t.Fatalf("Execute() events/errors = %#v/%v, want only WORKING", result.events, result.errs)
	}
	executor.mu.Lock()
	state := executor.state
	executor.mu.Unlock()
	if state != nil {
		t.Fatalf("executor retained task state: %#v", state)
	}
}

func TestCancelParkedTurn(t *testing.T) {
	canceled := false
	executor, err := New(fakeRunner{
		run: func(context.Context, runtime.Turn, runtime.EventSink) (runtime.Outcome, error) {
			return runtime.Outcome{Pending: &fakePendingTurn{
				request: &runtime.ApprovalRequest{
					ID: "approval-1", CallID: "call-1", Name: "protected.write", Hint: "Approve?",
				},
				cancel: func(context.Context, runtime.ModelCallSink) error {
					canceled = true
					return nil
				},
			}}, nil
		},
	}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	if err != nil {
		t.Fatal(err)
	}
	events, errs := collect(executor.Execute(t.Context(), requestContext("task-1", "write")))
	if len(errs) != 0 || len(events) != 2 {
		t.Fatalf("Execute() events/errors = %#v/%v", events, errs)
	}

	cancelEvents, cancelErrs := collect(executor.Cancel(t.Context(), requestContext("task-1", "ignored")))
	if len(cancelErrs) != 0 || len(cancelEvents) != 1 || !canceled {
		t.Fatalf("Cancel() events/errors/called = %#v/%v/%v", cancelEvents, cancelErrs, canceled)
	}
	update, ok := cancelEvents[0].(*a2atype.TaskStatusUpdateEvent)
	if !ok || update.Status.State != a2atype.TaskStateCanceled {
		t.Fatalf("cancel event = %#v", cancelEvents[0])
	}
	executor.mu.Lock()
	state := executor.state
	executor.mu.Unlock()
	if state != nil {
		t.Fatalf("executor retained task state: %#v", state)
	}
}

func requestContext(taskID a2atype.TaskID, text string) *a2asrv.ExecutorContext {
	message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(text))
	message.TaskID, message.ContextID = taskID, testContextID
	return &a2asrv.ExecutorContext{TaskID: taskID, ContextID: testContextID, Message: message}
}

func TestExecutePublishesAndConsumesStructuredApproval(t *testing.T) {
	var decision *runtime.ApprovalDecision
	executor, err := New(fakeRunner{run: func(_ context.Context, _ runtime.Turn, _ runtime.EventSink) (runtime.Outcome, error) {
		return runtime.Outcome{Pending: &fakePendingTurn{
			request: &runtime.ApprovalRequest{ID: "7", CallID: "call-1", Name: "tools.write", Args: map[string]any{"value": 7}, Hint: "Allow tools.write?"},
			resume: func(_ context.Context, response runtime.InputResponse, sink runtime.EventSink) (runtime.Outcome, error) {
				decision, _ = response.(*runtime.ApprovalDecision)
				return runtime.Outcome{}, sink.ToolResult(runtime.ToolResult{ID: "call-1", Name: "tools.write", IsError: true,
					Result: &mcp.CallToolResult{IsError: true, Content: []mcp.Content{&mcp.TextContent{Text: "denied"}}, StructuredContent: map[string]any{"count": 7, "items": []string{"pod-1"}}}})
			},
		}}, nil
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	if err != nil {
		t.Fatal(err)
	}
	first := requestContext("task-approval", "write")
	if reservedTaskID(t, executor) != "" {
		t.Fatal("idle session is reserved")
	}
	events, errs := collect(executor.Execute(t.Context(), first))
	if len(errs) != 0 || len(events) != 2 {
		t.Fatalf("first execution events/errors = %#v/%v", events, errs)
	}
	update, ok := events[1].(*a2atype.TaskStatusUpdateEvent)
	if !ok || update.Status.State != a2atype.TaskStateInputRequired || update.Status.Message == nil || !slices.Contains(update.Status.Message.Extensions, apia2a.HITLExtensionURI) {
		t.Fatalf("input-required event = %#v", events[1])
	}
	decisionMessage := a2atype.NewMessage(a2atype.MessageRoleUser)
	if reservedTaskID(t, executor) != first.TaskID {
		t.Fatal("pending approval did not reserve its native session")
	}
	decisionMessage.TaskID, decisionMessage.ContextID = first.TaskID, first.ContextID
	if err := apia2a.AttachHITL(decisionMessage, apia2a.ToolApprovalResponse{Type: apia2a.HITLTypeToolApprovalResponse, Approvals: []apia2a.ToolApproval{{ID: "7", Approved: false, RejectionReason: "production is still serving traffic"}}}); err != nil {
		t.Fatal(err)
	}
	second := &a2asrv.ExecutorContext{TaskID: first.TaskID, ContextID: first.ContextID, Message: decisionMessage, StoredTask: &a2atype.Task{ID: first.TaskID, ContextID: first.ContextID, Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired, Message: update.Status.Message}}}
	resumedEvents, errs := collect(executor.Execute(t.Context(), second))
	if len(errs) != 0 || decision == nil || decision.ID != "7" || decision.Approved || decision.RejectionReason != "production is still serving traffic" {
		t.Fatalf("approval decision = %#v, errors = %v", decision, errs)
	}
	for _, event := range resumedEvents {
		if update, ok := event.(*a2atype.TaskArtifactUpdateEvent); ok {
			assertArtifactPersistence(t, update.Artifact)
		}
	}
	assertToolActivity(t, resumedEvents, "function_response", map[string]any{
		"id": "call-1", "name": "tools.write", "response": map[string]any{"isError": true,
			"result": map[string]any{"isError": true, "content": []any{map[string]any{"type": "text", "text": "denied"}}, "structuredContent": map[string]any{"count": float64(7), "items": []any{"pod-1"}}}},
	})
	if reservedTaskID(t, executor) != "" {
		t.Fatal("completed approval kept its native session reserved")
	}
}

func TestExecutePublishesAndConsumesAskUser(t *testing.T) {
	var response *runtime.AskUserResponse
	executor, err := New(fakeRunner{run: func(_ context.Context, _ runtime.Turn, _ runtime.EventSink) (runtime.Outcome, error) {
		return runtime.Outcome{Pending: &fakePendingTurn{
			request: &runtime.AskUserRequest{
				ID: "ask-1", Hint: "Choose a target.",
				Questions: []runtime.AskUserQuestion{{
					ID: "namespace", Question: "Which namespace?", Choices: []string{"default"},
				}},
			},
			resume: func(_ context.Context, input runtime.InputResponse, _ runtime.EventSink) (runtime.Outcome, error) {
				response, _ = input.(*runtime.AskUserResponse)
				return runtime.Outcome{}, nil
			},
		}}, nil
	}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	if err != nil {
		t.Fatal(err)
	}
	first := requestContext("task-ask", "deploy")
	events, errs := collect(executor.Execute(t.Context(), first))
	if len(errs) != 0 || len(events) != 2 {
		t.Fatalf("first execution events/errors = %#v/%v", events, errs)
	}
	update, ok := events[1].(*a2atype.TaskStatusUpdateEvent)
	if !ok || update.Status.State != a2atype.TaskStateInputRequired {
		t.Fatalf("input-required event = %#v", events[1])
	}
	request, err := apia2a.ParseAskUserRequest(update.Status.Message)
	if err != nil || request == nil || request.ID != "ask-1" || len(request.Questions) != 1 {
		t.Fatalf("ask-user request = %#v, %v", request, err)
	}
	if got := request.Questions[0].Choices; !reflect.DeepEqual(got, []string{"default"}) {
		t.Fatalf("ask-user choices = %#v", got)
	}
	answer := a2atype.NewMessage(a2atype.MessageRoleUser)
	answer.TaskID, answer.ContextID = first.TaskID, first.ContextID
	if err := apia2a.AttachHITL(answer, apia2a.AskUserResponse{
		Type: apia2a.HITLTypeAskUserResponse, ID: "ask-1",
		Answers: []apia2a.AskUserAnswer{{Answer: []string{"default"}}},
	}); err != nil {
		t.Fatal(err)
	}
	second := &a2asrv.ExecutorContext{
		TaskID: first.TaskID, ContextID: first.ContextID, Message: answer,
		StoredTask: &a2atype.Task{ID: first.TaskID, ContextID: first.ContextID, Status: a2atype.TaskStatus{
			State: a2atype.TaskStateInputRequired, Message: update.Status.Message,
		}},
	}
	invalid := a2atype.NewMessage(a2atype.MessageRoleUser)
	require.NoError(t, apia2a.AttachHITL(invalid, apia2a.AskUserResponse{
		Type: apia2a.HITLTypeAskUserResponse, ID: "ask-1", Answers: []apia2a.AskUserAnswer{{}},
	}))
	second.Message = invalid
	invalidEvents, invalidErrs := collect(executor.Execute(t.Context(), second))
	require.Empty(t, invalidEvents, "invalid response must not move the task to WORKING or FAILED")
	require.Len(t, invalidErrs, 1)
	require.Nil(t, response, "invalid response must not start native replay")
	reserved, err := executor.ReservedTaskID()
	require.NoError(t, err)
	require.Equal(t, first.TaskID, reserved)
	second.Message = answer
	_, errs = collect(executor.Execute(t.Context(), second))
	if len(errs) != 0 || response == nil || response.ID != "ask-1" || !reflect.DeepEqual(response.Answers, [][]string{{"default"}}) {
		t.Fatalf("ask-user response = %#v, errors = %v", response, errs)
	}
}

func collect(seq iter.Seq2[a2atype.Event, error]) ([]a2atype.Event, []error) {
	var events []a2atype.Event
	var errs []error
	for event, err := range seq {
		if event != nil {
			events = append(events, event)
		}
		if err != nil {
			errs = append(errs, err)
		}
	}
	return events, errs
}

// DATA can be restored into an executor that has already been constructed.
type restoringRunner struct {
	fakeRunner
	taskID, contextID string
	pending           runtime.PendingTurn
	err               error
}

func (r *restoringRunner) RestorePending() (string, string, runtime.PendingTurn, error) {
	return r.taskID, r.contextID, r.pending, r.err
}

func TestExecutorRecoversTaskOwnershipAfterConstruction(t *testing.T) {
	resumed := false
	runner := &restoringRunner{}
	executor, err := New(runner, &fakeContinuation{data: testSessionID}, tracing.RuntimeTelemetry{})
	require.NoError(t, err)
	require.Empty(t, reservedTaskID(t, executor))
	request := &runtime.ApprovalRequest{ID: "approval-1", CallID: "call-1", Name: "protected.write", Args: map[string]any{}}
	runner.taskID, runner.contextID = "task-1", testContextID
	runner.pending = &fakePendingTurn{request: request, resume: func(_ context.Context, response runtime.InputResponse, _ runtime.EventSink) (runtime.Outcome, error) {
		decision := response.(*runtime.ApprovalDecision)
		require.Equal(t, request.ID, decision.ID)
		require.True(t, decision.Approved)
		resumed = true
		runner.taskID, runner.contextID, runner.pending = "", "", nil
		return runtime.Outcome{}, nil
	}}
	require.Equal(t, a2atype.TaskID("task-1"), reservedTaskID(t, executor))
	_, errs := collect(executor.Execute(t.Context(), requestContext("unrelated-task", "hello")))
	require.NotEmpty(t, errs)
	status, err := inputRequiredMessage(requestContext("task-1", "write"), request)
	require.NoError(t, err)
	message := a2atype.NewMessage(a2atype.MessageRoleUser)
	require.NoError(t, apia2a.AttachHITL(message, apia2a.ToolApprovalResponse{Type: apia2a.HITLTypeToolApprovalResponse, Approvals: []apia2a.ToolApproval{{ID: request.ID, Approved: true}}}))
	ctx := &a2asrv.ExecutorContext{TaskID: "task-1", ContextID: testContextID, Message: message, StoredTask: &a2atype.Task{ID: "task-1", ContextID: testContextID, Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired, Message: status}}}
	events, errs := collect(executor.Execute(t.Context(), ctx))
	require.Empty(t, errs)
	require.True(t, resumed)
	require.NotEmpty(t, events)
	require.Equal(t, a2atype.TaskStateCompleted, events[len(events)-1].(*a2atype.TaskStatusUpdateEvent).Status.State)
	require.Empty(t, reservedTaskID(t, executor))
}

func TestExecutorRejectsCorruptRestoration(t *testing.T) {
	runner := &restoringRunner{err: errors.New("corrupt durable data")}
	executor, err := New(runner, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	require.NoError(t, err)
	id, restoreErr := executor.ReservedTaskID()
	require.Empty(t, id)
	require.ErrorContains(t, restoreErr, "corrupt durable data")
	_, errs := collect(executor.Execute(t.Context(), requestContext("task-1", "hello")))
	require.Len(t, errs, 1)
	require.ErrorContains(t, errs[0], "corrupt durable data")
}

// Covers both failure boundaries seen with SDK values in interface fields:
// protobuf persistence and gob-based artifact copying.
func assertArtifactPersistence(t *testing.T, artifact *a2atype.Artifact) {
	t.Helper()
	task := &a2atype.Task{ID: "task-result", ContextID: testContextID, Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}, Artifacts: []*a2atype.Artifact{artifact}}
	_, err := pbconv.ToProtoTask(task)
	require.NoError(t, err)
	store := taskstore.NewInMemory(nil)
	_, err = store.Create(t.Context(), task)
	require.NoError(t, err)
	stored, err := store.Get(t.Context(), task.ID)
	require.NoError(t, err)
	require.Equal(t, task, stored.Task)
}

func TestToolResultPartRejectsUnserializableResult(t *testing.T) {
	part, err := toolResultPart(runtime.ToolResult{ID: "call", Name: "lookup", Result: map[string]any{"invalid": make(chan int)}})
	require.Error(t, err)
	require.Nil(t, part)
}

func reservedTaskID(t *testing.T, executor *Executor) a2atype.TaskID {
	t.Helper()
	id, err := executor.ReservedTaskID()
	require.NoError(t, err)
	return id
}

func TestDependencyCancellationReportsTaskFailure(t *testing.T) {
	for _, test := range []struct {
		name    string
		err     error
		message string
	}{
		{name: "checkpoint timeout", err: fmt.Errorf("interrupt Codex before checkpoint: %w", context.DeadlineExceeded), message: "Harness runtime execution failed"},
		{name: "dependency canceled", err: fmt.Errorf("dependency: %w", context.Canceled), message: "Harness runtime execution failed"},
		{name: "abandoned tool timeout", err: errors.Join(runtime.ErrSessionAbandoned, context.DeadlineExceeded), message: "start a new session"},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor, err := New(fakeRunner{run: func(ctx context.Context, _ runtime.Turn, _ runtime.EventSink) (runtime.Outcome, error) {
				require.NoError(t, ctx.Err())
				return runtime.Outcome{}, test.err
			}}, &fakeContinuation{}, tracing.RuntimeTelemetry{})
			require.NoError(t, err)
			events, errs := collect(executor.Execute(t.Context(), requestContext("task-1", "hello")))
			require.Empty(t, errs)
			require.NotEmpty(t, events)
			last := events[len(events)-1].(*a2atype.TaskStatusUpdateEvent)
			require.Equal(t, a2atype.TaskStateFailed, last.Status.State)
			require.Contains(t, last.Status.Message.Parts[0].Text(), test.message)
		})
	}
}
