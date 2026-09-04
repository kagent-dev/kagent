package app

import (
	"context"
	"iter"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	a2ataskstore "github.com/a2aproject/a2a-go/v2/a2asrv/taskstore"
	"github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

// fakeExecutor implements a2asrv.AgentExecutor for testing.
type fakeExecutor struct{}

func (f *fakeExecutor) Execute(_ context.Context, _ *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {}
}

func (f *fakeExecutor) Cancel(_ context.Context, _ *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {}
}

var _ a2asrv.AgentExecutor = (*fakeExecutor)(nil)

func TestNew_NilExecutor(t *testing.T) {
	_, err := New(AppConfig{
		AgentCard: a2atype.AgentCard{Name: "test"},
	}, nil)
	if err == nil {
		t.Fatal("expected error for nil executor, got nil")
	}
}

func TestNew_Success(t *testing.T) {
	app, err := New(AppConfig{
		AgentCard: a2atype.AgentCard{Name: "test-agent"},
		Port:      "0",
	}, &fakeExecutor{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if app == nil {
		t.Fatal("expected non-nil app")
	}
}

func TestSeedTaskInterceptor(t *testing.T) {
	store := a2ataskstore.NewInMemory(nil)
	message := &a2atype.Message{ID: "message-1", TaskID: "task-1", ContextID: "instance-1", Role: a2atype.MessageRoleUser}
	interceptor := seedTaskInterceptor{store: store}
	_, _, err := interceptor.Before(t.Context(), nil, &a2asrv.Request{Payload: &a2atype.SendMessageRequest{Message: message}})
	if err != nil {
		t.Fatalf("Before() error = %v", err)
	}
	stored, err := store.Get(t.Context(), message.TaskID)
	if err != nil || stored.Task.ID != message.TaskID || stored.Task.ContextID != message.ContextID || len(stored.Task.History) != 1 {
		t.Fatalf("stored task = %#v, error = %v", stored, err)
	}
}

func TestSeedTaskInterceptorRestoresWaitingTask(t *testing.T) {
	store := a2ataskstore.NewInMemory(nil)
	status := a2a.AttachHitlExtension(a2atype.NewMessage(a2atype.MessageRoleAgent), &a2a.AskUserRequest{
		Type: a2a.HITLTypeAskUserRequest, ID: "question-1",
	})
	waiting := &a2atype.Task{
		ID: "task-1", ContextID: "instance-1",
		Status: a2atype.TaskStatus{State: a2atype.TaskStateInputRequired, Message: status},
	}
	reply := a2atype.NewMessage(a2atype.MessageRoleUser)
	reply.TaskID, reply.ContextID = waiting.ID, waiting.ContextID
	if err := apia2a.AttachStoredTask(reply, waiting); err != nil {
		t.Fatal(err)
	}
	interceptor := seedTaskInterceptor{store: store}
	if _, _, err := interceptor.Before(t.Context(), nil, &a2asrv.Request{Payload: &a2atype.SendMessageRequest{Message: reply}}); err != nil {
		t.Fatal(err)
	}
	stored, err := store.Get(t.Context(), waiting.ID)
	if err != nil || stored.Task.Status.State != a2atype.TaskStateInputRequired || a2a.GetAskUserRequest(stored.Task.Status.Message) == nil {
		t.Fatalf("restored task = %#v, error = %v", stored, err)
	}
	if restored, err := apia2a.TakeStoredTask(reply); err != nil || restored != nil {
		t.Fatalf("private state was not consumed: task = %#v, error = %v", restored, err)
	}
}

func TestStoredTaskMetadataSupportsAuthRequired(t *testing.T) {
	waiting := &a2atype.Task{
		ID: "task-1", ContextID: "instance-1",
		Status: a2atype.TaskStatus{State: a2atype.TaskStateAuthRequired},
	}
	reply := &a2atype.Message{TaskID: waiting.ID, ContextID: waiting.ContextID}
	if err := apia2a.AttachStoredTask(reply, waiting); err != nil {
		t.Fatal(err)
	}
	restored, err := apia2a.TakeStoredTask(reply)
	if err != nil || restored == nil || restored.Status.State != a2atype.TaskStateAuthRequired || restored.Status.Message != nil {
		t.Fatalf("restored task = %#v, error = %v", restored, err)
	}
}

func TestApplyDefaults_Port(t *testing.T) {
	t.Setenv("PORT", "")
	cfg := applyDefaults(AppConfig{})
	if cfg.Port != defaultPort {
		t.Errorf("expected port %q, got %q", defaultPort, cfg.Port)
	}
}

func TestApplyDefaults_PortFromEnv(t *testing.T) {
	t.Setenv("PORT", "9090")
	cfg := applyDefaults(AppConfig{})
	if cfg.Port != "9090" {
		t.Errorf("expected port %q, got %q", "9090", cfg.Port)
	}
}

func TestApplyDefaults_PortExplicit(t *testing.T) {
	t.Setenv("PORT", "9090")
	cfg := applyDefaults(AppConfig{Port: "3000"})
	if cfg.Port != "3000" {
		t.Errorf("expected port %q, got %q", "3000", cfg.Port)
	}
}

func TestApplyDefaults_ShutdownTimeout(t *testing.T) {
	cfg := applyDefaults(AppConfig{})
	if cfg.ShutdownTimeout != defaultShutdownTimeout {
		t.Errorf("expected shutdown timeout %v, got %v", defaultShutdownTimeout, cfg.ShutdownTimeout)
	}
}

func TestApplyDefaults_ShutdownTimeoutExplicit(t *testing.T) {
	cfg := applyDefaults(AppConfig{ShutdownTimeout: 10 * time.Second})
	if cfg.ShutdownTimeout != 10*time.Second {
		t.Errorf("expected shutdown timeout %v, got %v", 10*time.Second, cfg.ShutdownTimeout)
	}
}

func TestBuildAppName_FromEnv(t *testing.T) {
	t.Setenv("KAGENT_NAME", "my-agent")
	t.Setenv("KAGENT_NAMESPACE", "my-ns")
	name := buildAppName(&a2atype.AgentCard{Name: "card-name"})
	if name != "my_ns__NS__my_agent" {
		t.Errorf("expected %q, got %q", "my_ns__NS__my_agent", name)
	}
}

func TestBuildAppName_FromAgentCard(t *testing.T) {
	t.Setenv("KAGENT_NAME", "")
	t.Setenv("KAGENT_NAMESPACE", "")
	name := buildAppName(&a2atype.AgentCard{Name: "card-name"})
	if name != "card-name" {
		t.Errorf("expected %q, got %q", "card-name", name)
	}
}

func TestBuildAppName_Default(t *testing.T) {
	t.Setenv("KAGENT_NAME", "")
	t.Setenv("KAGENT_NAMESPACE", "")
	name := buildAppName(&a2atype.AgentCard{})
	if name != defaultAppName {
		t.Errorf("expected %q, got %q", defaultAppName, name)
	}
}
