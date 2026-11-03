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

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/managedagents"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type managedSandboxFixture struct {
	t        *testing.T
	owner    string
	starts   int
	started  chan struct{}
	stopping *atomic.Bool
	suspends atomic.Int32
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

func (f *managedSandboxFixture) Create(ctx context.Context, _ *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
	f.checkOwner(ctx)
	return &apiv1.Sandbox{Id: "sandbox", State: apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED, Operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE}, nil
}

func (f *managedSandboxFixture) Resume(ctx context.Context, id string) (*apiv1.Sandbox, error) {
	f.checkOwner(ctx)
	assert.Equal(f.t, "sandbox", id)
	return &apiv1.Sandbox{Id: id, State: apiv1.RuntimeState_RUNTIME_STATE_READY, Operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE}, nil
}

func (f *managedSandboxFixture) StartProcess(ctx context.Context, id string, _ *guestpb.StartProcessRequest) (*guestpb.Process, error) {
	f.checkOwner(ctx)
	assert.Equal(f.t, "sandbox", id)
	f.starts++
	close(f.started)
	return &guestpb.Process{ProcessId: "worker", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil }

func (f *managedSandboxFixture) GetProcess(ctx context.Context, id string, req *guestpb.GetProcessRequest) (*guestpb.Process, error) {
	f.checkOwner(ctx)
	assert.Equal(f.t, "sandbox", id)
	assert.Equal(f.t, "worker", req.ProcessId)
	state := guestpb.ProcessState_PROCESS_STATE_RUNNING
	if f.stopping.Load() {
		state = guestpb.ProcessState_PROCESS_STATE_EXITED
	}
	return &guestpb.Process{ProcessId: req.ProcessId, State: state}, nil
}

func (f *managedSandboxFixture) Suspend(ctx context.Context, id string) (*apiv1.Sandbox, error) {
	f.checkOwner(ctx)
	assert.Equal(f.t, "sandbox", id)
	assert.True(f.t, f.stopping.Load(), "must request worker shutdown before suspension")
	f.suspends.Add(1)
	return &apiv1.Sandbox{
		Id: id, State: apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED,
		Operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE,
	}, nil
}

func TestManagedAgentsRunsInProcessWithStableOwner(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var polls atomic.Int32
	var stops atomic.Int32
	var stopping atomic.Bool
	started := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/poll"):
			if polls.Add(1) > 1 {
				select {
				case <-started:
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
			assert.Equal(t, "Bearer environment-key", r.Header.Get("Authorization"))
			body, err := io.ReadAll(r.Body)
			assert.NoError(t, err)
			assert.JSONEq(t, `{"force":false}`, string(body))
			stops.Add(1)
			stopping.Store(true)
			_, _ = io.WriteString(w, `{}`)
		default:
			t.Errorf("unexpected upstream request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	cfg := ManagedAgentsConfig{EnvironmentID: "env_test", EnvironmentKey: "environment-key", BaseURL: server.URL, TemplateNamespace: "test", TemplateName: "claude"}
	service := &managedSandboxFixture{t: t, owner: auth.PrincipalID("kagent:managed-agents", cfg.EnvironmentID), started: started, stopping: &stopping}
	first, err := newManagedAgentsRunnable(cfg, service, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	require.True(t, first.NeedLeaderElection(), "polling and collection must run only on the manager leader")
	second, err := newManagedAgentsRunnable(cfg, service, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	assert.Equal(t, first.session.Principal().ID, second.session.Principal().ID, "replicas and restarts must reuse the same owner")
	require.NoError(t, first.Start(ctx))
	assert.Equal(t, 1, service.starts)
	assert.Equal(t, int32(1), stops.Load())
	assert.Equal(t, int32(1), service.suspends.Load())
}
