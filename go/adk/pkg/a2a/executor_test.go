package a2a

import (
	"context"
	"encoding/base64"
	"iter"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/go-logr/logr"
	adkagent "google.golang.org/adk/agent"
	adkartifact "google.golang.org/adk/artifact"
	"google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
)

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
// persisted to the artifact service via SaveInputBlobsAsArtifacts.
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
// the task for an oversized inbound file.
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
