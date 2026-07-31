package a2a

import (
	"context"
	"iter"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/go-logr/logr"
)

type recordingExecutor struct {
	message       *a2atype.Message
	cleanupCalled bool
	events        []a2atype.Event
}

func (e *recordingExecutor) Execute(_ context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		e.message = reqCtx.Message
		if e.events != nil {
			for _, event := range e.events {
				if !yield(event, nil) {
					return
				}
			}
			return
		}
		yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateWorking, nil), nil)
	}
}

func (e *recordingExecutor) Cancel(_ context.Context, reqCtx *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		yield(a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateCanceled, nil), nil)
	}
}

func (e *recordingExecutor) Cleanup(context.Context, *a2asrv.ExecutorContext, a2atype.SendMessageResult, error) {
	e.cleanupCalled = true
}

func TestKAgentExecutor_TransformsHITLDecisionBeforeDelegating(t *testing.T) {
	decision := a2atype.NewMessage(
		a2atype.MessageRoleUser,
		dataPart(map[string]any{KAgentHitlDecisionTypeKey: KAgentHitlDecisionTypeApprove}, nil),
	)
	storedTask := &a2atype.Task{
		ID:        "task-1",
		ContextID: "ctx-1",
		Status: a2atype.TaskStatus{
			State: a2atype.TaskStateInputRequired,
			Message: a2atype.NewMessage(
				a2atype.MessageRoleAgent,
				dataPart(
					map[string]any{
						"name": "adk_request_confirmation",
						"id":   "confirm-1",
						"args": map[string]any{
							"originalFunctionCall": map[string]any{
								"name": "delete_file",
								"args": map[string]any{"path": "/tmp/x"},
								"id":   "call-1",
							},
						},
					},
					map[string]any{
						"kagent_type":            "function_call",
						"kagent_is_long_running": true,
					},
				),
			),
		},
		History: []*a2atype.Message{decision},
	}
	reqCtx := &a2asrv.ExecutorContext{
		TaskID:     "task-1",
		ContextID:  "ctx-1",
		Message:    decision,
		StoredTask: storedTask,
	}
	builtin := &recordingExecutor{}
	executor := &KAgentExecutor{builtin: builtin, logger: logr.Discard()}

	var events []a2atype.Event
	for event, err := range executor.Execute(context.Background(), reqCtx) {
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		events = append(events, event)
	}

	if len(events) != 2 {
		t.Fatalf("Execute() emitted %d events, want decision acknowledgement and delegated event", len(events))
	}
	decisionAck, ok := events[0].(*a2atype.TaskStatusUpdateEvent)
	if !ok || decisionAck.Status.State != a2atype.TaskStateWorking || decisionAck.Status.Message != decision {
		t.Fatalf("first event = %#v, want original decision acknowledgement", events[0])
	}
	working, ok := events[1].(*a2atype.TaskStatusUpdateEvent)
	if !ok || working.Status.State != a2atype.TaskStateWorking || working.Status.Message != nil {
		t.Fatalf("delegated event = %#v, want content-free working status", events[1])
	}
	if len(storedTask.History) != 0 {
		t.Fatalf("stored task history len = %d, want pre-appended decision removed", len(storedTask.History))
	}
	if builtin.message == nil || len(builtin.message.Parts) != 1 {
		t.Fatalf("delegated message = %#v, want one FunctionResponse", builtin.message)
	}
	part := builtin.message.Parts[0]
	if got, _ := ReadMetadataValue(part.Metadata, A2ADataPartMetadataTypeKey); got != A2ADataPartMetadataTypeFunctionResponse {
		t.Fatalf("delegated part type = %#v, want function_response", got)
	}
	if got := asDataPart(part)[PartKeyID]; got != "confirm-1" {
		t.Fatalf("delegated FunctionResponse id = %#v, want confirm-1", got)
	}
}

func TestKAgentExecutor_ForwardsCleanup(t *testing.T) {
	builtin := &recordingExecutor{}
	executor := &KAgentExecutor{builtin: builtin}
	executor.Cleanup(context.Background(), &a2asrv.ExecutorContext{}, nil, nil)
	if !builtin.cleanupCalled {
		t.Fatal("Cleanup() was not forwarded to the upstream executor")
	}
}

func TestKAgentExecutor_PreservesContentBearingLastChunk(t *testing.T) {
	reqCtx := &a2asrv.ExecutorContext{TaskID: "task-1", ContextID: "ctx-1"}
	reqCtx.Message = a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi"))
	final := a2atype.NewArtifactEvent(reqCtx, a2atype.NewTextPart("hello"))
	final.LastChunk = true
	builtin := &recordingExecutor{events: []a2atype.Event{final}}
	executor := &KAgentExecutor{builtin: builtin, logger: logr.Discard()}

	var got []a2atype.Event
	for event, err := range executor.Execute(context.Background(), reqCtx) {
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got = append(got, event)
	}

	if len(got) != 1 || got[0] != final {
		t.Fatalf("Execute() events = %#v, want the original final artifact only", got)
	}
	update, ok := got[0].(*a2atype.TaskArtifactUpdateEvent)
	if !ok || !update.LastChunk || len(update.Artifact.Parts) != 1 || update.Artifact.Parts[0].Text() != "hello" {
		t.Fatalf("final artifact = %#v, want content-bearing lastChunk event", got[0])
	}
}
