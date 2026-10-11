package app

import (
	"context"
	"iter"
	"net"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"
	"github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	runtimetaskstore "github.com/kagent-dev/kagent/go/adk/pkg/taskstore"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/stretchr/testify/require"
	adkagent "google.golang.org/adk/v2/agent"
	"google.golang.org/adk/v2/session"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
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

func TestNew_RequiresController(t *testing.T) {
	t.Setenv(env.KagentAPIURL.Name(), "")
	app, err := New(AppConfig{
		AgentCard: a2atype.AgentCard{Name: "test-agent"},
	}, &fakeExecutor{})
	require.ErrorContains(t, err, "ControllerClient or KAGENT_API_URL is required")
	require.Nil(t, app)
}

func TestNew_ControllerFromEnv(t *testing.T) {
	t.Setenv(env.KagentAPIURL.Name(), "http://127.0.0.1:1")
	app, err := New(AppConfig{
		AgentCard: a2atype.AgentCard{Name: "test-agent"},
		Port:      "0",
	}, &fakeExecutor{})
	require.NoError(t, err)
	require.NotNil(t, app)
	require.NotNil(t, app.ownedController)
	t.Cleanup(func() { require.NoError(t, app.ownedController.Close()) })
}

func TestNew_ProvidedController(t *testing.T) {
	t.Setenv(env.KagentAPIURL.Name(), "")
	controller, err := controllerclient.New(controllerclient.Config{APIURL: "http://127.0.0.1:1"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, controller.Close()) })
	app, err := New(AppConfig{
		ControllerClient: controller,
		AgentCard:        a2atype.AgentCard{Name: "test-agent"},
		Port:             "0",
	}, &fakeExecutor{})
	require.NoError(t, err)
	require.NotNil(t, app)
	require.Nil(t, app.ownedController, "the caller retains ownership of its client")
}

func TestApplyDefaults_Port(t *testing.T) {
	t.Setenv("KAGENT_PORT", "")
	cfg := applyDefaults(AppConfig{})
	if cfg.Port != defaultPort {
		t.Errorf("expected port %q, got %q", defaultPort, cfg.Port)
	}
}

func TestApplyDefaults_PortFromEnv(t *testing.T) {
	t.Setenv("KAGENT_PORT", "9090")
	cfg := applyDefaults(AppConfig{})
	if cfg.Port != "9090" {
		t.Errorf("expected port %q, got %q", "9090", cfg.Port)
	}
}

func TestApplyDefaults_PortExplicit(t *testing.T) {
	t.Setenv("KAGENT_PORT", "9090")
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

func TestBuildAppName_UnsetNamespace(t *testing.T) {
	t.Setenv(env.KagentName.Name(), "my-agent")
	t.Setenv(env.KagentNamespace.Name(), "")
	require.NoError(t, os.Unsetenv(env.KagentNamespace.Name()))
	require.Equal(t, "card-name", buildAppName(&a2atype.AgentCard{Name: "card-name"}))
}

func TestBuildAppName_Default(t *testing.T) {
	t.Setenv("KAGENT_NAME", "")
	t.Setenv("KAGENT_NAMESPACE", "")
	name := buildAppName(&a2atype.AgentCard{})
	if name != defaultAppName {
		t.Errorf("expected %q, got %q", defaultAppName, name)
	}
}

func TestBuildAgentCard_DeclaresHITLWithoutAgent(t *testing.T) {
	card := buildAgentCard(AppConfig{AgentCard: a2atype.AgentCard{Name: "byo-agent"}})

	if !slices.ContainsFunc(card.Capabilities.Extensions, func(extension a2atype.AgentExtension) bool {
		return extension.URI == a2a.HITLExtensionURI
	}) {
		t.Fatalf("extensions = %#v, want the HITL extension", card.Capabilities.Extensions)
	}
}

func TestBuildAgentCard_DeclaresHITLWithAgent(t *testing.T) {
	agent, err := adkagent.New(adkagent.Config{
		Name:        "adk_agent",
		Description: "an ADK agent",
		Run: func(adkagent.InvocationContext) iter.Seq2[*session.Event, error] {
			return func(yield func(*session.Event, error) bool) {}
		},
	})
	if err != nil {
		t.Fatalf("adkagent.New: %v", err)
	}

	card := buildAgentCard(AppConfig{AgentCard: a2atype.AgentCard{Name: "adk-agent"}, Agent: agent})

	if !slices.ContainsFunc(card.Capabilities.Extensions, func(extension a2atype.AgentExtension) bool {
		return extension.URI == a2a.HITLExtensionURI
	}) {
		t.Fatalf("extensions = %#v, want the HITL extension", card.Capabilities.Extensions)
	}
	if card.Description != "an ADK agent" {
		t.Errorf("description = %q, want the agent description", card.Description)
	}
}

func TestBuildAgentCard_LeavesCallerCardUntouched(t *testing.T) {
	cfg := AppConfig{AgentCard: a2atype.AgentCard{Name: "byo-agent"}}

	buildAgentCard(cfg)

	if len(cfg.AgentCard.Capabilities.Extensions) != 0 {
		t.Errorf("caller extensions = %#v, want the caller's card untouched", cfg.AgentCard.Capabilities.Extensions)
	}
}

// admissionServer sends the next message during settlement: the first moment the
// gateway can report the previous task as terminal.
type admissionServer struct {
	apiv1alpha1.UnimplementedTaskStoreServiceServer
	mu      sync.Mutex
	tasks   map[string]*apiv1alpha1.StoredTask
	next    func() error
	nextErr chan error
}

func (s *admissionServer) save(task *a2apb.Task) int64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	version := s.tasks[task.GetId()].GetVersion() + 1
	s.tasks[task.GetId()] = &apiv1alpha1.StoredTask{Task: task, Version: version}
	return version
}

func (s *admissionServer) GetTask(_ context.Context, request *apiv1alpha1.TaskStoreServiceGetTaskRequest) (*apiv1alpha1.TaskStoreServiceGetTaskResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if stored, ok := s.tasks[request.TaskId]; ok {
		return &apiv1alpha1.TaskStoreServiceGetTaskResponse{Stored: stored}, nil
	}
	return nil, status.Error(codes.NotFound, "task not found")
}

func (s *admissionServer) CreateTask(_ context.Context, request *apiv1alpha1.TaskStoreServiceCreateTaskRequest) (*apiv1alpha1.TaskStoreServiceCreateTaskResponse, error) {
	return &apiv1alpha1.TaskStoreServiceCreateTaskResponse{Version: s.save(request.Task)}, nil
}

func (s *admissionServer) UpdateTask(_ context.Context, request *apiv1alpha1.TaskStoreServiceUpdateTaskRequest) (*apiv1alpha1.TaskStoreServiceUpdateTaskResponse, error) {
	return &apiv1alpha1.TaskStoreServiceUpdateTaskResponse{Version: s.save(request.Task)}, nil
}

func (s *admissionServer) SettleTask(context.Context, *apiv1alpha1.TaskStoreServiceSettleTaskRequest) (*apiv1alpha1.TaskStoreServiceSettleTaskResponse, error) {
	if next := s.next; next != nil {
		s.next = nil
		s.nextErr <- next()
	}
	return &apiv1alpha1.TaskStoreServiceSettleTaskResponse{}, nil
}

func TestSettledTaskAdmitsNextSend(t *testing.T) {
	api := &admissionServer{tasks: map[string]*apiv1alpha1.StoredTask{}, nextErr: make(chan error, 1)}
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	apiv1alpha1.RegisterTaskStoreServiceServer(server, api)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(server.Stop)
	client, err := controllerclient.New(controllerclient.Config{
		APIURL: "http://api.test", DialOptions: []grpc.DialOption{
			grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }),
		},
	})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })
	dir := t.TempDir()
	for field, value := range map[string]string{"name": "session-" + uuid.NewString(), "atespace": "team-a", "uid": "actor-uid"} {
		require.NoError(t, os.WriteFile(filepath.Join(dir, field), []byte(value), 0o600))
	}
	store := runtimetaskstore.New(client, filepath.Join(dir, "name"))
	started, release := make(chan struct{}), make(chan struct{})
	var executions atomic.Int32
	wrapper := store.WrapExecutor(a2asrv.AgentExecutorFunc(func(_ context.Context, input *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
		return func(yield func(a2atype.Event, error) bool) {
			if executions.Add(1) == 1 {
				close(started)
				<-release
			}
			yield(a2atype.NewStatusUpdateEvent(input, a2atype.TaskStateCompleted, nil), nil)
		}
	}), "", nil)
	handler := a2asrv.NewHandler(wrapper, handlerOptions(store, wrapper)...)
	send := func(text string) error {
		_, err := handler.SendMessage(t.Context(), &a2atype.SendMessageRequest{Message: a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(text))})
		return err
	}
	api.next = func() error { return send("second") }
	first := make(chan error, 1)
	go func() { first <- send("first") }()
	<-started
	require.ErrorIs(t, send("concurrent"), a2atype.ErrUnsupportedOperation, "active work must reject another send")
	close(release)
	require.NoError(t, <-first)
	require.NoError(t, <-api.nextErr, "settlement must not precede the slot becoming free")
	require.Equal(t, int32(2), executions.Load())
}
