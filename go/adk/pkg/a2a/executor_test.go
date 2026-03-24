package a2a

import (
	"context"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/a2a"
	"github.com/a2aproject/a2a-go/a2asrv"
	"github.com/go-logr/logr"
	"github.com/kagent-dev/kagent/go/adk/pkg/session"
	"google.golang.org/adk/runner"
	adksession "google.golang.org/adk/session"
)

// ---------------------------------------------------------------------------
// Test helpers shared across test files in this package
// ---------------------------------------------------------------------------

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

// adka2aMetaKey mirrors adka2a.ToA2AMetaKey for tests (avoids importing the upstream package).
func adka2aMetaKey(key string) string {
	return "adk_" + key
}

// ---------------------------------------------------------------------------
// Compile-time interface check
// ---------------------------------------------------------------------------

var _ a2asrv.AgentExecutor = (*KAgentExecutor)(nil)

// ---------------------------------------------------------------------------
// KAgentExecutor.Execute: nil message
// ---------------------------------------------------------------------------

func TestKAgentExecutor_RejectsNilMessage(t *testing.T) {
	sessService := adksession.InMemoryService()
	exec := NewKAgentExecutor(KAgentExecutorConfig{
		RunnerConfig: runner.Config{
			AppName:        "test-app",
			SessionService: sessService,
		},
		SessionService: (*session.KAgentSessionService)(nil),
		Logger:         logr.Discard(),
	})

	reqCtx := &a2asrv.RequestContext{
		Message:   nil,
		TaskID:    "task-nil",
		ContextID: "ctx-nil",
	}

	err := exec.Execute(context.Background(), reqCtx, &noopQueue{})
	if err == nil {
		t.Fatal("expected error for nil message, got nil")
	}
}

// ---------------------------------------------------------------------------
// KAgentExecutor.Cancel
// ---------------------------------------------------------------------------

func TestKAgentExecutor_Cancel(t *testing.T) {
	exec := NewKAgentExecutor(KAgentExecutorConfig{
		RunnerConfig: runner.Config{AppName: "test-app"},
		Logger:       logr.Discard(),
	})

	rec := &recordingQueue{}
	reqCtx := newReqCtx()
	if err := exec.Cancel(context.Background(), reqCtx, rec); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}
	if len(rec.events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(rec.events))
	}
	ev, ok := rec.events[0].(*a2atype.TaskStatusUpdateEvent)
	if !ok {
		t.Fatalf("expected TaskStatusUpdateEvent, got %T", rec.events[0])
	}
	if ev.Status.State != a2atype.TaskStateCanceled {
		t.Errorf("expected Canceled state, got %q", ev.Status.State)
	}
	if !ev.Final {
		t.Error("cancel event should be final")
	}
}

// ---------------------------------------------------------------------------
// extractSessionName
// ---------------------------------------------------------------------------

func TestExtractSessionName(t *testing.T) {
	tests := []struct {
		name    string
		message *a2atype.Message
		want    string
	}{
		{
			name:    "nil message",
			message: nil,
			want:    "",
		},
		{
			name:    "empty parts",
			message: a2atype.NewMessage(a2atype.MessageRoleUser),
			want:    "",
		},
		{
			name:    "short text",
			message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.TextPart{Text: "hello"}),
			want:    "hello",
		},
		{
			name: "long text truncated",
			message: a2atype.NewMessage(a2atype.MessageRoleUser,
				a2atype.TextPart{Text: "this is a very long session name that should be truncated"}),
			want: "this is a very long " + "...",
		},
		{
			name:    "data part only",
			message: a2atype.NewMessage(a2atype.MessageRoleUser, &a2atype.DataPart{Data: map[string]any{"x": 1}}),
			want:    "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSessionName(tt.message)
			if got != tt.want {
				t.Errorf("extractSessionName() = %q, want %q", got, tt.want)
			}
		})
	}
}
