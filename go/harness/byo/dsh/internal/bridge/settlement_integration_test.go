//go:build linux

package bridge

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	adka2a "github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	"github.com/kagent-dev/kagent/go/adk/pkg/controllerclient"
	"github.com/kagent-dev/kagent/go/adk/pkg/taskstore"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/harness/byo/dsh/internal/config"
	runtimea2a "github.com/kagent-dev/kagent/go/harness/runtime/a2a"
	"github.com/kagent-dev/kagent/go/harness/runtime/continuation"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

// The real runtime Store and settlement wrapper talk gRPC to this API fixture.
// It checks runtime ordering and version fences, not PostgreSQL or Substrate.
type taskStoreFixture struct {
	apiv1alpha1.UnimplementedTaskStoreServiceServer
	mu      sync.Mutex
	tasks   map[string]*apiv1alpha1.StoredTask
	settled chan *a2apb.Task
	check   func(*a2apb.Task) error
}

func (s *taskStoreFixture) CreateTask(_ context.Context, r *apiv1alpha1.TaskStoreServiceCreateTaskRequest) (*apiv1alpha1.TaskStoreServiceCreateTaskResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if r.Task == nil || r.Task.Id == "" || r.Task.ContextId == "" {
		return nil, status.Error(codes.InvalidArgument, "missing task identity")
	}
	if s.tasks[r.Task.Id] != nil {
		return nil, status.Error(codes.AlreadyExists, "task already exists")
	}
	s.tasks[r.Task.Id] = &apiv1alpha1.StoredTask{Task: proto.CloneOf(r.Task), Version: 1}
	return &apiv1alpha1.TaskStoreServiceCreateTaskResponse{Version: 1}, nil
}

func (s *taskStoreFixture) UpdateTask(_ context.Context, r *apiv1alpha1.TaskStoreServiceUpdateTaskRequest) (*apiv1alpha1.TaskStoreServiceUpdateTaskResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.tasks[r.Task.GetId()]
	if stored == nil || stored.Version != r.ExpectedVersion {
		return nil, status.Error(codes.Aborted, "task version conflict")
	}
	stored.Task = proto.CloneOf(r.Task)
	stored.Version++
	return &apiv1alpha1.TaskStoreServiceUpdateTaskResponse{Version: stored.Version}, nil
}

func (s *taskStoreFixture) GetTask(_ context.Context, r *apiv1alpha1.TaskStoreServiceGetTaskRequest) (*apiv1alpha1.TaskStoreServiceGetTaskResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.tasks[r.TaskId]
	if stored == nil {
		return nil, status.Error(codes.NotFound, "task not found")
	}
	return &apiv1alpha1.TaskStoreServiceGetTaskResponse{Stored: proto.CloneOf(stored)}, nil
}

func (s *taskStoreFixture) SettleTask(_ context.Context, r *apiv1alpha1.TaskStoreServiceSettleTaskRequest) (*apiv1alpha1.TaskStoreServiceSettleTaskResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := s.tasks[r.TaskId]
	if stored == nil || stored.Version != r.Version {
		return nil, status.Error(codes.Aborted, "settlement version conflict")
	}
	if s.check != nil {
		if err := s.check(stored.Task); err != nil {
			return nil, status.Error(codes.FailedPrecondition, err.Error())
		}
	}
	s.settled <- proto.CloneOf(stored.Task)
	return &apiv1alpha1.TaskStoreServiceSettleTaskResponse{}, nil
}

func newSettlementClient(t *testing.T, executor a2asrv.AgentExecutor, check func(*a2apb.Task) error) (a2apb.A2AServiceClient, *taskStoreFixture) {
	t.Helper()
	sessionID := uuid.NewString()
	identityDir := t.TempDir()
	for field, value := range map[string]string{"name": "session-" + sessionID, "atespace": "acceptance", "uid": "test-actor"} {
		require.NoError(t, os.WriteFile(filepath.Join(identityDir, field), []byte(value), 0600))
	}
	api := &taskStoreFixture{tasks: make(map[string]*apiv1alpha1.StoredTask), settled: make(chan *a2apb.Task, 16), check: check}
	controlListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	control := grpc.NewServer(grpc.UnaryInterceptor(func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		md, _ := metadata.FromIncomingContext(ctx)
		if strings.Join(md.Get(apia2a.InsecureRuntimeIdentityHeader), "") != "acceptance/session-"+sessionID+"/test-actor" || req.(interface{ GetSessionId() string }).GetSessionId() != sessionID {
			return nil, status.Error(codes.PermissionDenied, "runtime identity mismatch")
		}
		return handler(ctx, req)
	}))
	apiv1alpha1.RegisterTaskStoreServiceServer(control, api)
	controlDone := make(chan error, 1)
	go func() { controlDone <- control.Serve(controlListener) }()
	t.Cleanup(func() { control.Stop(); require.NoError(t, <-controlDone) })
	controller, err := controllerclient.New(controllerclient.Config{APIURL: "http://" + controlListener.Addr().String()})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, controller.Close()) })
	tasks := taskstore.New(controller, filepath.Join(identityDir, "name"))
	wrapped := tasks.WrapExecutor(executor, "", nil)
	handler := a2asrv.NewHandler(wrapped, a2asrv.WithTaskStore(tasks), a2asrv.WithCallInterceptors(adka2a.HITLActivationInterceptor(), adka2a.UserIDCallInterceptor(), wrapped))
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	a2agrpc.NewHandler(handler).RegisterWith(server)
	serveDone := make(chan error, 1)
	go func() { serveDone <- server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); require.NoError(t, <-serveDone) })
	connection, err := grpc.NewClient(listener.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, connection.Close()) })
	return a2apb.NewA2AServiceClient(connection), api
}

func TestSettlementWaitsForDetachedToolCleanup(t *testing.T) {
	for _, mode := range []string{"normal", "forced", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			executable, err := os.Executable()
			require.NoError(t, err)
			dir := t.TempDir()
			script, pidFile := filepath.Join(dir, "dsh"), filepath.Join(dir, "tool.pid")
			require.NoError(t, os.WriteFile(script, []byte("#!/bin/sh\nexec '"+strings.ReplaceAll(executable, "'", "'\\''")+"' -test.run=TestDetachedDSHProcess\n"), 0700))
			cfg, err := config.Parse([]byte(`{"model":{"type":"openai","model":"mock"},"runtime":{"control_plane_url":"http://controller:8083"}}`))
			require.NoError(t, err)
			exitMode := mode
			if mode == "cancel" {
				exitMode = "forced"
			}
			runner, err := New(cfg, script, filepath.Join(dir, "data"), append(os.Environ(), "DSH_AUDIT_DETACHED="+pidFile, "DSH_AUDIT_EXIT="+exitMode))
			require.NoError(t, err)
			store, err := continuation.New(filepath.Join(dir, "data", "adapter"), "dsh-byo", ValidateSessionID)
			require.NoError(t, err)
			executor, err := runtimea2a.New(runner, store, tracing.RuntimeTelemetry{})
			require.NoError(t, err)
			client, api := newSettlementClient(t, executor, func(*a2apb.Task) error {
				for _, path := range []string{pidFile, pidFile + ".dsh"} {
					data, err := os.ReadFile(path)
					if err != nil {
						return err
					}
					pid, err := strconv.Atoi(string(data))
					if err != nil {
						return err
					}
					if syscall.Kill(pid, 0) != syscall.ESRCH {
						return fmt.Errorf("settlement preceded process cleanup")
					}
				}
				return nil
			})
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
			defer cancel()
			msg := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("work"))
			msg.ContextID = uuid.NewString()
			req, err := pbconv.ToProtoSendMessageRequest(&a2atype.SendMessageRequest{Message: msg})
			require.NoError(t, err)
			if mode == "cancel" {
				stream, err := client.SendStreamingMessage(ctx, req)
				require.NoError(t, err)
				initial, err := stream.Recv()
				require.NoError(t, err)
				require.NotNil(t, initial.GetTask())
				require.Eventually(t, func() bool { _, err := os.Stat(pidFile); return err == nil }, 3*time.Second, 10*time.Millisecond)
				cancelRequest, err := pbconv.ToProtoCancelTaskRequest(&a2atype.CancelTaskRequest{ID: a2atype.TaskID(initial.GetTask().Id)})
				require.NoError(t, err)
				canceled, err := client.CancelTask(ctx, cancelRequest)
				require.NoError(t, err)
				require.Equal(t, a2apb.TaskState_TASK_STATE_CANCELED, canceled.Status.State)
			} else {
				_, err = client.SendMessage(ctx, req)
				require.NoError(t, err)
			}
			select {
			case task := <-api.settled:
				want := a2apb.TaskState_TASK_STATE_COMPLETED
				switch mode {
				case "forced":
					want = a2apb.TaskState_TASK_STATE_FAILED
				case "cancel":
					want = a2apb.TaskState_TASK_STATE_CANCELED
				}
				require.Equal(t, want, task.Status.State)
			case <-ctx.Done():
				t.Fatal("task never settled", ctx.Err())
			}
		})
	}
}
