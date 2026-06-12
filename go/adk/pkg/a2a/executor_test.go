package a2a

import (
	"context"
	"encoding/base64"
	"iter"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/go-logr/logr"
	adkagent "google.golang.org/adk/v2/agent"
	adkartifact "google.golang.org/adk/v2/artifact"
	"google.golang.org/adk/v2/runner"
	adksession "google.golang.org/adk/v2/session"
)

// TestNewAgentMessage_StampsContextAndTaskID verifies agent messages carry the
// request's context and task ids. A2A allows omitting them (the task is the
// canonical carrier), but stamping them lets consumers that flatten task.history
// key each message to its task without backfilling.
func TestNewAgentMessage_StampsContextAndTaskID(t *testing.T) {
	reqCtx := &a2asrv.RequestContext{
		ContextID: "ctx-xyz",
		TaskID:    a2atype.TaskID("task-xyz"),
	}

	msg := newAgentMessage(reqCtx, a2atype.TextPart{Text: "hello"})

	if msg.ContextID != "ctx-xyz" {
		t.Errorf("ContextID = %q, want %q", msg.ContextID, "ctx-xyz")
	}
	if msg.TaskID != a2atype.TaskID("task-xyz") {
		t.Errorf("TaskID = %q, want %q", msg.TaskID, a2atype.TaskID("task-xyz"))
	}
	if msg.Role != a2atype.MessageRoleAgent {
		t.Errorf("Role = %q, want %q", msg.Role, a2atype.MessageRoleAgent)
	}
}

// TestNewAgentStatusEvent_MessageCarriesIDs verifies the per-event emission seam:
// the working status event the executor writes (and that is persisted into
// task.history) carries an agent message stamped with the request's context/task
// ids. This is the property the send guard depends on — without it the persisted
// message keys differently from its locally-streamed counterpart and falsely
// blocks the next send. Mirrors the Python converter test.
func TestNewAgentStatusEvent_MessageCarriesIDs(t *testing.T) {
	reqCtx := &a2asrv.RequestContext{
		ContextID: "ctx-xyz",
		TaskID:    a2atype.TaskID("task-xyz"),
	}
	meta := map[string]any{"k": "v"}

	ev := newAgentStatusEvent(reqCtx, a2atype.ContentParts{a2atype.TextPart{Text: "hi"}}, meta)

	if ev.Status.State != a2atype.TaskStateWorking {
		t.Errorf("State = %q, want %q", ev.Status.State, a2atype.TaskStateWorking)
	}
	if ev.Status.Message == nil {
		t.Fatal("status message is nil")
	}
	if ev.Status.Message.ContextID != "ctx-xyz" {
		t.Errorf("message ContextID = %q, want %q", ev.Status.Message.ContextID, "ctx-xyz")
	}
	if ev.Status.Message.TaskID != a2atype.TaskID("task-xyz") {
		t.Errorf("message TaskID = %q, want %q", ev.Status.Message.TaskID, a2atype.TaskID("task-xyz"))
	}
	// The event itself also carries the ids (from reqCtx), matching the message.
	if ev.ContextID != "ctx-xyz" {
		t.Errorf("event ContextID = %q, want %q", ev.ContextID, "ctx-xyz")
	}
	if ev.TaskID != a2atype.TaskID("task-xyz") {
		t.Errorf("event TaskID = %q, want %q", ev.TaskID, a2atype.TaskID("task-xyz"))
	}
}

// noopAgent returns an agent that emits no events (logic before agent run is
// what we exercise — e.g. SaveInputBlobsAsArtifacts).
func noopAgent(t *testing.T, name string) adkagent.Agent {
	t.Helper()
	a, err := adkagent.New(adkagent.Config{
		Name: name,
		Run: func(_ adkagent.InvocationContext) iter.Seq2[*adksession.Event, error] {
			return func(yield func(*adksession.Event, error) bool) {}
		},
	})
	if err != nil {
		t.Fatalf("agent.New() error = %v", err)
	}
	return a
}

// TestExecute_PersistsInboundUploads verifies that an inbound file upload is
// persisted to the artifact service via SaveInputBlobsAsArtifacts (AC3).
func TestExecute_PersistsInboundUploads(t *testing.T) {
	ctx := context.Background()
	const (
		appName   = "test-app"
		contextID = "ctx-1"
	)
	userID := "A2A_USER_" + contextID
	sessionID := contextID

	sessionSvc := adksession.InMemoryService()
	if _, err := sessionSvc.Create(ctx, &adksession.CreateRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	}); err != nil {
		t.Fatalf("session create error = %v", err)
	}

	artifactSvc := adkartifact.InMemoryService()

	e := NewKAgentExecutor(KAgentExecutorConfig{
		RunnerConfig: runner.Config{
			AppName:         appName,
			Agent:           noopAgent(t, "test_agent"),
			SessionService:  sessionSvc,
			ArtifactService: artifactSvc,
		},
		AppName: appName,
		Logger:  logr.Discard(),
	})

	msg := a2atype.NewMessage(a2atype.MessageRoleUser,
		a2atype.TextPart{Text: "here is a file"},
		a2atype.FilePart{File: a2atype.FileBytes{
			FileMeta: a2atype.FileMeta{Name: "note.txt", MimeType: "text/plain"},
			Bytes:    base64.StdEncoding.EncodeToString([]byte("hello world")),
		}},
	)
	reqCtx := &a2asrv.RequestContext{
		Message:   msg,
		TaskID:    a2atype.NewTaskID(),
		ContextID: contextID,
	}

	if err := e.Execute(ctx, reqCtx, &fakeQueue{}); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}

	listResp, err := artifactSvc.List(ctx, &adkartifact.ListRequest{
		AppName:   appName,
		UserID:    userID,
		SessionID: sessionID,
	})
	if err != nil {
		t.Fatalf("artifact List() error = %v", err)
	}
	if len(listResp.FileNames) != 1 {
		t.Fatalf("expected 1 persisted artifact, got %d (%v)", len(listResp.FileNames), listResp.FileNames)
	}
}

// TestExecute_RejectsOversizedUpload verifies the server-side size guard fails
// the task for an oversized inbound file (AC2).
func TestExecute_RejectsOversizedUpload(t *testing.T) {
	ctx := context.Background()
	const (
		appName   = "test-app"
		contextID = "ctx-2"
	)
	t.Setenv(envMaxArtifactBytes, "8")

	e := NewKAgentExecutor(KAgentExecutorConfig{
		RunnerConfig: runner.Config{
			AppName:         appName,
			Agent:           noopAgent(t, "test_agent"),
			SessionService:  adksession.InMemoryService(),
			ArtifactService: adkartifact.InMemoryService(),
		},
		AppName: appName,
		Logger:  logr.Discard(),
	})

	msg := a2atype.NewMessage(a2atype.MessageRoleUser,
		a2atype.FilePart{File: a2atype.FileBytes{
			FileMeta: a2atype.FileMeta{Name: "big.txt"},
			Bytes:    base64.StdEncoding.EncodeToString([]byte("way too many bytes")),
		}},
	)
	reqCtx := &a2asrv.RequestContext{
		Message:   msg,
		TaskID:    a2atype.NewTaskID(),
		ContextID: contextID,
	}

	queue := &fakeQueue{}
	if err := e.Execute(ctx, reqCtx, queue); err != nil {
		t.Fatalf("Execute() unexpected error = %v", err)
	}

	// The guard should emit a single failed status update.
	if len(queue.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(queue.events))
	}
	statusEvent, ok := queue.events[0].(*a2atype.TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("event type = %T, want *a2atype.TaskStatusUpdateEvent", queue.events[0])
	}
	if statusEvent.Status.State != a2atype.TaskStateFailed {
		t.Errorf("state = %q, want %q", statusEvent.Status.State, a2atype.TaskStateFailed)
	}
}
