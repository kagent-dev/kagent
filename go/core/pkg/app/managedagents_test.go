package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/managedagents"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

const managedSandboxID = "00000000-0000-4000-8000-000000000001"

type managedSandboxFixture struct {
	t        *testing.T
	owner    string
	name     string
	starts   int
	started  chan struct{}
	reads    atomic.Int32
	suspends atomic.Int32
	onCreate func(context.Context)
	onStart  func()
}

var _ managedagents.SandboxService = (*managedSandboxFixture)(nil)

func (f *managedSandboxFixture) checkOwner(ctx context.Context) {
	f.t.Helper()
	session, ok := auth.AuthSessionFrom(ctx)
	require.True(f.t, ok)
	assert.Equal(f.t, f.owner, session.Principal().ID)
	assert.Nil(f.t, session.Principal().RuntimeBinding)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://router.test", nil)
	require.NoError(f.t, err)
	require.NoError(f.t, auth.ForwardCredential(request, session))
	assert.Empty(f.t, request.Header.Get("Authorization"))
}

func (f *managedSandboxFixture) Create(ctx context.Context, req *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
	f.checkOwner(ctx)
	if f.onCreate != nil {
		f.onCreate(ctx)
	}
	f.name = req.Name
	return &apiv1.Sandbox{Id: managedSandboxID, State: apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED, Operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE}, nil
}

func (f *managedSandboxFixture) List(ctx context.Context, req *apiv1.ListSandboxesRequest) (*apiv1.ListSandboxesResponse, error) {
	f.checkOwner(ctx)
	assert.Equal(f.t, "test", req.GetSandboxTemplate().GetNamespace())
	assert.Equal(f.t, "claude", req.GetSandboxTemplate().GetName())
	page := &apiv1.ListSandboxesResponse{}
	if f.name != "" {
		instance, err := f.Get(ctx, managedSandboxID)
		if err != nil {
			return nil, err
		}
		page.Sandboxes = []*apiv1.Sandbox{instance}
	}
	return page, nil
}

func (f *managedSandboxFixture) Resume(ctx context.Context, id string) (*apiv1.Sandbox, error) {
	f.checkOwner(ctx)
	assert.Equal(f.t, managedSandboxID, id)
	return &apiv1.Sandbox{Id: id, State: apiv1.RuntimeState_RUNTIME_STATE_READY, Operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE}, nil
}

func (f *managedSandboxFixture) StartProcess(ctx context.Context, id string, req *guestpb.StartProcessRequest) (*guestpb.Process, error) {
	f.checkOwner(ctx)
	if req.GetCommand()[0] == "ps" {
		return &guestpb.Process{ProcessId: "ps", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
	}
	assert.NoError(f.t, ctx.Err(), "shutdown must drain the in-flight launch")
	assert.Equal(f.t, managedSandboxID, id)
	if f.onStart != nil {
		f.onStart()
	}
	f.starts++
	close(f.started)
	return &guestpb.Process{ProcessId: "worker", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
}

func (f *managedSandboxFixture) StreamProcessOutput(ctx context.Context, id string, req *guestpb.StreamProcessOutputRequest, send func(*guestpb.ProcessOutput) error) error {
	f.checkOwner(ctx)
	assert.Equal(f.t, managedSandboxID, id)
	assert.Equal(f.t, "ps", req.GetProcessId())
	if err := send(&guestpb.ProcessOutput{Output: &guestpb.ProcessOutput_Stdout{Stdout: []byte("guest-server\nant\nps\n")}}); err != nil {
		return err
	}
	return send(&guestpb.ProcessOutput{Output: &guestpb.ProcessOutput_Exit{Exit: &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_EXITED}}})
}

func (f *managedSandboxFixture) Get(ctx context.Context, id string) (*apiv1.Sandbox, error) {
	f.checkOwner(ctx)
	assert.Equal(f.t, managedSandboxID, id)
	f.reads.Add(1)
	return &apiv1.Sandbox{
		Id: id, Creator: f.owner, Name: f.name, State: apiv1.RuntimeState_RUNTIME_STATE_READY,
		Operation:       apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE,
		SandboxTemplate: &apiv1.ResourceReference{Namespace: "test", Name: "claude"},
		ExpiresAt:       timestamppb.New(time.Now().Add(time.Hour)),
	}, nil
}

func (f *managedSandboxFixture) GetProcess(ctx context.Context, id string, req *guestpb.GetProcessRequest) (*guestpb.Process, error) {
	f.checkOwner(ctx)
	assert.Equal(f.t, managedSandboxID, id)
	assert.Equal(f.t, "worker", req.ProcessId)
	return &guestpb.Process{ProcessId: req.ProcessId, State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
}

func (f *managedSandboxFixture) Suspend(ctx context.Context, id string) (*apiv1.Sandbox, error) {
	f.checkOwner(ctx)
	assert.Equal(f.t, managedSandboxID, id)
	f.t.Error("controller restart must leave the running worker alone")
	f.suspends.Add(1)
	return &apiv1.Sandbox{
		Id: id, State: apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED,
		Operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE,
	}, nil
}

func TestManagedAgentsAdoptsWorkerAcrossControllerRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var polls atomic.Int32
	var stops atomic.Int32
	var recovering atomic.Bool
	started := make(chan struct{})
	provisioning := make(chan struct{})
	service := &managedSandboxFixture{
		t: t, owner: auth.PrincipalID("kagent:managed-agents", "env_test"), started: started,
		onCreate: func(launchCtx context.Context) {
			close(provisioning)
			<-ctx.Done()
			assert.NoError(t, launchCtx.Err(), "normal shutdown must not cancel provisioning")
		},
	}
	recoveryCtx, finishRecovery := context.WithTimeout(t.Context(), 5*time.Second)
	defer finishRecovery()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer environment-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/poll"):
			if recovering.Load() {
				assert.Positive(t, service.reads.Load(), "restore sandbox tracking before polling")
				finishRecovery()
				w.WriteHeader(http.StatusNoContent)
				return
			}
			if polls.Add(1) > 1 {
				select {
				case <-provisioning:
					cancel()
				case <-ctx.Done():
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			_, _ = io.WriteString(w, `{"id":"work_test","environment_id":"env_test","secret":"scoped-secret","data":{"id":"session_test","type":"session"}}`)
		case strings.HasSuffix(r.URL.Path, "/ack"):
			_, _ = io.WriteString(w, `{}`)
		case strings.HasSuffix(r.URL.Path, "/heartbeat"):
			_, _ = io.WriteString(w, `{"last_heartbeat":"exact-token","ttl_seconds":60,"lease_extended":true,"state":"active"}`)
		case strings.HasSuffix(r.URL.Path, "/stop"):
			assert.NoError(t, r.Context().Err())
			stops.Add(1)
			t.Error("controller shutdown must not stop established Anthropic work")
			_, _ = io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected upstream request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	cfg := ManagedAgentsConfig{EnvironmentID: "env_test", EnvironmentKey: "environment-key", BaseURL: server.URL, TemplateNamespace: "test", TemplateName: "claude"}
	first, err := newManagedAgentsRunnable(cfg, service, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	require.True(t, first.NeedLeaderElection(), "polling and collection must run only on the manager leader")
	second, err := newManagedAgentsRunnable(cfg, service, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	assert.Equal(t, first.session.Principal().ID, second.session.Principal().ID, "replicas and restarts must reuse the same owner")
	require.NoError(t, first.Start(ctx))
	assert.Equal(t, 1, service.starts)
	recovering.Store(true)
	require.NoError(t, second.Start(recoveryCtx))
	assert.Positive(t, service.reads.Load())
	assert.Equal(t, 1, service.starts, "new leader must adopt rather than start a duplicate worker")
	assert.Zero(t, stops.Load())
	assert.Zero(t, service.suspends.Load())
}
