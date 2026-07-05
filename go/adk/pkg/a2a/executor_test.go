package a2a

import (
	"context"
	"iter"
	"testing"
	"unicode/utf8"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/go-logr/logr"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/model"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
	"google.golang.org/genai"
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
	const appName = "test-app"
	decision := hitlDecisionMessage(&ToolApprovalResponse{
		Type:      HITLTypeToolApprovalResponse,
		Approvals: []ToolApproval{{ID: "confirm-1", Approved: true}},
	})
	storedTask := &a2atype.Task{
		ID:        "task-1",
		ContextID: "ctx-1",
		Status: a2atype.TaskStatus{
			State: a2atype.TaskStateInputRequired,
			Message: AttachHitlExtension(a2atype.NewMessage(a2atype.MessageRoleAgent, a2atype.NewTextPart("Approval required")), &ToolApprovalRequest{
				Type: HITLTypeToolApprovalRequest,
				Tools: []HitlTool{{
					ID: "confirm-1", CallID: "call-1", Name: "delete_file",
					Args: map[string]any{"path": "/tmp/x"},
				}},
			}),
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
	executor := &KAgentExecutor{builtin: builtin, appName: appName, logger: logr.Discard()}

	ctx, callCtx := a2asrv.NewCallContext(context.Background(), a2asrv.NewServiceParams(map[string][]string{
		a2atype.SvcParamExtensions: {HITLExtensionURI},
	}))
	if _, _, err := HITLActivationInterceptor().Before(ctx, callCtx, &a2asrv.Request{}); err != nil {
		t.Fatalf("activate HITL: %v", err)
	}

	var events []a2atype.Event
	for event, err := range executor.Execute(ctx, reqCtx) {
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

func TestKAgentExecutor_TranslatesADKPauseAtA2ABoundary(t *testing.T) {
	reqCtx := &a2asrv.ExecutorContext{
		TaskID: "task-1", ContextID: "ctx-1",
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("delete it")),
	}
	internalMessage := a2atype.NewMessage(a2atype.MessageRoleAgent,
		confirmationPart("confirm-1", "delete_file", "call-1", map[string]any{"path": "/tmp/x"}, nil))
	builtin := &recordingExecutor{events: []a2atype.Event{
		a2atype.NewStatusUpdateEvent(reqCtx, a2atype.TaskStateInputRequired, internalMessage),
	}}
	executor := &KAgentExecutor{builtin: builtin, logger: logr.Discard()}

	ctx, callCtx := a2asrv.NewCallContext(context.Background(), a2asrv.NewServiceParams(map[string][]string{
		a2atype.SvcParamExtensions: {HITLExtensionURI},
	}))
	if _, _, err := HITLActivationInterceptor().Before(ctx, callCtx, &a2asrv.Request{}); err != nil {
		t.Fatalf("activate HITL: %v", err)
	}

	var got *a2atype.TaskStatusUpdateEvent
	for event, err := range executor.Execute(ctx, reqCtx) {
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		got, _ = event.(*a2atype.TaskStatusUpdateEvent)
	}
	if got == nil || got.Status.State != a2atype.TaskStateInputRequired {
		t.Fatalf("status update = %#v", got)
	}
	payload := GetToolApprovalRequest(got.Status.Message)
	if payload == nil {
		t.Fatalf("HITL payload missing on status message")
	}
	if len(got.Status.Message.Parts) != 1 || got.Status.Message.Parts[0].Text() == "" {
		t.Fatalf("public pause leaked non-text parts: %#v", got.Status.Message.Parts)
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

func TestKAgentExecutor_StreamsArtifactsThroughUpstreamExecutor(t *testing.T) {
	const (
		appName   = "test-app"
		contextID = "context-1"
	)

	agent, err := adkagent.New(adkagent.Config{
		Name: "streaming-agent",
		Run: func(ic adkagent.InvocationContext) iter.Seq2[*adksession.Event, error] {
			return func(yield func(*adksession.Event, error) bool) {
				partial := &adksession.Event{
					Author:       ic.Agent().Name(),
					InvocationID: ic.InvocationID(),
					Branch:       ic.Branch(),
					LLMResponse: model.LLMResponse{
						Content: genai.NewContentFromText("hel", genai.RoleModel),
						Partial: true,
					},
				}
				if !yield(partial, nil) {
					return
				}

				final := &adksession.Event{
					Author:       ic.Agent().Name(),
					InvocationID: ic.InvocationID(),
					Branch:       ic.Branch(),
					LLMResponse: model.LLMResponse{
						Content: genai.NewContentFromText("hello", genai.RoleModel),
					},
				}
				yield(final, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("agent.New() error = %v", err)
	}

	sessionService := adksession.InMemoryService()
	executor := NewKAgentExecutor(KAgentExecutorConfig{
		AppName:        appName,
		SessionService: sessionService,
		Logger:         logr.Discard(),
		RunnerConfig: runner.Config{
			AppName: appName,
			Agent:   agent,
		},
	})
	reqCtx := &a2asrv.ExecutorContext{
		TaskID:    "task-1",
		ContextID: contextID,
		Message:   a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("hi")),
	}

	var updates []*a2atype.TaskArtifactUpdateEvent
	var completed *a2atype.TaskStatusUpdateEvent
	for event, err := range executor.Execute(context.Background(), reqCtx) {
		if err != nil {
			t.Fatalf("Execute() error = %v", err)
		}
		switch event := event.(type) {
		case *a2atype.TaskArtifactUpdateEvent:
			updates = append(updates, event)
		case *a2atype.TaskStatusUpdateEvent:
			if event.Status.State == a2atype.TaskStateCompleted {
				completed = event
			}
		}
	}

	if len(updates) != 2 {
		t.Fatalf("artifact updates = %d, want 2", len(updates))
	}
	if updates[0].Append || updates[0].LastChunk || updates[0].Artifact.Parts[0].Text() != "hel" {
		t.Fatalf("first artifact update = %#v, want opening partial artifact", updates[0])
	}
	if updates[1].Append || !updates[1].LastChunk || updates[1].Artifact.ID != updates[0].Artifact.ID || updates[1].Artifact.Parts[0].Text() != "hello" {
		t.Fatalf("second artifact update = %#v, want content-bearing final replacement", updates[1])
	}
	if completed == nil || completed.Status.Message != nil {
		t.Fatalf("completed status = %#v, want content-free completion", completed)
	}
}

func TestKAgentExecutor_HITLPauseAndResumeFlow(t *testing.T) {
	const (
		appName   = "hitl-app"
		contextID = "hitl-context"
	)
	invocations := 0
	agent, err := adkagent.New(adkagent.Config{
		Name: "hitl-agent",
		Run: func(ic adkagent.InvocationContext) iter.Seq2[*adksession.Event, error] {
			return func(yield func(*adksession.Event, error) bool) {
				invocations++
				if invocations == 1 {
					call := genai.NewPartFromFunctionCall("adk_request_confirmation", map[string]any{
						"originalFunctionCall": map[string]any{"name": "delete_file", "id": "tool-call", "args": map[string]any{"path": "/tmp/x"}},
						"toolConfirmation":     map[string]any{"hint": "Delete /tmp/x?", "confirmed": false, "payload": nil},
					})
					call.FunctionCall.ID = "confirmation-call"
					yield(&adksession.Event{
						Author: ic.Agent().Name(), InvocationID: ic.InvocationID(), Branch: ic.Branch(),
						LLMResponse:        model.LLMResponse{Content: &genai.Content{Role: genai.RoleModel, Parts: []*genai.Part{call}}},
						LongRunningToolIDs: []string{"confirmation-call"},
					}, nil)
					return
				}
				yield(&adksession.Event{
					Author: ic.Agent().Name(), InvocationID: ic.InvocationID(), Branch: ic.Branch(),
					LLMResponse: model.LLMResponse{Content: genai.NewContentFromText("resumed", genai.RoleModel)},
				}, nil)
			}
		},
	})
	if err != nil {
		t.Fatalf("agent.New() error = %v", err)
	}
	sessionService := adksession.InMemoryService()
	executor := NewKAgentExecutor(KAgentExecutorConfig{
		AppName: appName, SessionService: sessionService, Logger: logr.Discard(),
		RunnerConfig: runner.Config{AppName: appName, Agent: agent},
	})
	ctx, callCtx := a2asrv.NewCallContext(context.Background(), a2asrv.NewServiceParams(map[string][]string{
		a2atype.SvcParamExtensions: {HITLExtensionURI},
	}))
	if _, _, err := HITLActivationInterceptor().Before(ctx, callCtx, &a2asrv.Request{}); err != nil {
		t.Fatalf("activate HITL: %v", err)
	}

	first := &a2asrv.ExecutorContext{
		TaskID: "hitl-task", ContextID: contextID,
		Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("delete it")),
	}
	var pause *a2atype.TaskStatusUpdateEvent
	for event, err := range executor.Execute(ctx, first) {
		if err != nil {
			t.Fatalf("pause Execute() error = %v", err)
		}
		if update, ok := event.(*a2atype.TaskStatusUpdateEvent); ok && update.Status.State == a2atype.TaskStateInputRequired {
			pause = update
		}
	}
	req := GetToolApprovalRequest(pause.Status.Message)
	if pause == nil || req == nil {
		t.Fatalf("pause = %#v, want extension input-required", pause)
	}
	if len(req.Tools) != 1 || req.Tools[0].ID != "confirmation-call" {
		t.Fatalf("pause tools = %#v, want per-approval correlation", req.Tools)
	}

	decision := hitlDecisionMessage(&ToolApprovalResponse{
		Type:      HITLTypeToolApprovalResponse,
		Approvals: []ToolApproval{{ID: "confirmation-call", Approved: true}},
	})
	decision.TaskID, decision.ContextID = "hitl-task", contextID
	stored := &a2atype.Task{
		ID: "hitl-task", ContextID: contextID, Status: pause.Status, History: []*a2atype.Message{first.Message, decision},
	}
	resume := &a2asrv.ExecutorContext{
		TaskID: "hitl-task", ContextID: contextID, Message: decision, StoredTask: stored,
	}
	var resumedText string
	for event, err := range executor.Execute(ctx, resume) {
		if err != nil {
			t.Fatalf("resume Execute() error = %v", err)
		}
		if artifact, ok := event.(*a2atype.TaskArtifactUpdateEvent); ok && len(artifact.Artifact.Parts) > 0 {
			resumedText = artifact.Artifact.Parts[0].Text()
		}
	}
	if resumedText != "resumed" || invocations != 2 {
		t.Fatalf("resumed text = %q, invocations = %d", resumedText, invocations)
	}
}

// TestExtractSessionName verifies session names are truncated on rune boundaries.
// A byte-wise cut can split a multi-byte UTF-8 rune and produce an invalid-UTF-8
// name, which the Postgres session store rejects on session create. The result
// must always be valid UTF-8, and pure-ASCII behavior must be unchanged.
func TestExtractSessionName(t *testing.T) {
	textMsg := func(s string) *a2atype.Message {
		return &a2atype.Message{Parts: []a2atype.Part{a2atype.TextPart{Text: s}}}
	}

	tests := []struct {
		name string
		msg  *a2atype.Message
		want string
	}{
		{name: "nil message", msg: nil, want: ""},
		{name: "no parts", msg: &a2atype.Message{}, want: ""},
		{name: "short ascii", msg: textMsg("hello"), want: "hello"},
		{
			name: "ascii at limit is not truncated",
			msg:  textMsg("01234567890123456789"), // exactly 20 runes
			want: "01234567890123456789",
		},
		{
			name: "long ascii is truncated with ellipsis",
			msg:  textMsg("012345678901234567890"), // 21 runes
			want: "01234567890123456789...",
		},
		{
			// 7 CJK runes = 21 bytes: byte-slicing at 20 splits the 7th rune.
			name: "multibyte over byte limit stays valid utf8",
			msg:  textMsg("こんにちは世界"),
			want: "こんにちは世界",
		},
		{
			name: "long multibyte truncated on rune boundary",
			msg:  textMsg("あいうえおかきくけこさしすせそたちつてとな"), // 21 runes
			want: "あいうえおかきくけこさしすせそたちつてと...",
		},
		{
			name: "skips empty text part and uses first non-empty",
			msg: &a2atype.Message{Parts: []a2atype.Part{
				a2atype.TextPart{Text: ""},
				a2atype.TextPart{Text: "hi"},
			}},
			want: "hi",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSessionName(tt.msg)
			if got != tt.want {
				t.Errorf("extractSessionName() = %q, want %q", got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("extractSessionName() returned invalid UTF-8: %q", got)
			}
		})
	}
}
