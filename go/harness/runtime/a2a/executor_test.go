package a2a

import (
	"context"
	"errors"
	"iter"
	"reflect"
	"strings"
	"sync"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/kagent-dev/kagent/go/harness/runtime"
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
	}}, continuation)
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

func TestExecuteRejectsMalformedToolActivity(t *testing.T) {
	tests := []func(runtime.EventSink) error{
		func(sink runtime.EventSink) error { return sink.ToolCall(runtime.ToolCall{Name: "Read"}) },
		func(sink runtime.EventSink) error { return sink.ToolResult(runtime.ToolResult{ID: "tool-1"}) },
	}
	for _, emit := range tests {
		executor, err := New(fakeRunner{run: func(_ context.Context, _ runtime.Turn, sink runtime.EventSink) (runtime.Outcome, error) {
			return runtime.Outcome{}, emit(sink)
		}}, &fakeContinuation{})
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
		update, ok := event.(*a2atype.TaskStatusUpdateEvent)
		if !ok || update.Status.Message == nil || len(update.Status.Message.Parts) != 1 {
			continue
		}
		part := update.Status.Message.Parts[0]
		if part.Metadata["kagent_type"] != partType {
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
	}}, &fakeContinuation{})
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
	}}, &fakeContinuation{})
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
		active := executor.active
		executor.mu.Unlock()
		if active != nil {
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
	}}, &fakeContinuation{})
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

func requestContext(taskID a2atype.TaskID, text string) *a2asrv.ExecutorContext {
	message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(text))
	message.TaskID, message.ContextID = taskID, testContextID
	return &a2asrv.ExecutorContext{TaskID: taskID, ContextID: testContextID, Message: message}
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
