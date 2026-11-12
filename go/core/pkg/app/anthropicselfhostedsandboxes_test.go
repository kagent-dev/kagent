package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
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
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/anthropicselfhostedsandboxes"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

const anthropicSelfHostedSandboxID = "00000000-0000-4000-8000-000000000001"

type anthropicSelfHostedSandboxFixture struct {
	t        *testing.T
	owner    string
	name     string
	starts   int
	creates  atomic.Int32
	scans    atomic.Int32
	suspends atomic.Int32
	onCreate func(context.Context)
	onScan   func() error
	onList   func(context.Context) error
}

var _ anthropicselfhostedsandboxes.SandboxService = (*anthropicSelfHostedSandboxFixture)(nil)

func (f *anthropicSelfHostedSandboxFixture) checkOwner(ctx context.Context) {
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

func (f *anthropicSelfHostedSandboxFixture) Create(ctx context.Context, req *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
	f.checkOwner(ctx)
	f.creates.Add(1)
	if f.onCreate != nil {
		f.onCreate(ctx)
	}
	f.name = req.Name
	return &apiv1.Sandbox{Id: anthropicSelfHostedSandboxID, State: apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED, Operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE}, nil
}

func (f *anthropicSelfHostedSandboxFixture) List(ctx context.Context, req *apiv1.ListSandboxesRequest) (*apiv1.ListSandboxesResponse, error) {
	f.checkOwner(ctx)
	assert.Equal(f.t, "test", req.GetSandboxTemplate().GetNamespace())
	assert.Equal(f.t, "claude", req.GetSandboxTemplate().GetName())
	if f.onList != nil {
		if err := f.onList(ctx); err != nil {
			return nil, err
		}
	}
	page := &apiv1.ListSandboxesResponse{}
	if f.name != "" {
		instance, err := f.Get(ctx, anthropicSelfHostedSandboxID)
		if err != nil {
			return nil, err
		}
		page.Sandboxes = []*apiv1.Sandbox{instance}
	}
	return page, nil
}

func (f *anthropicSelfHostedSandboxFixture) Resume(ctx context.Context, id string) (*apiv1.Sandbox, error) {
	f.checkOwner(ctx)
	assert.Equal(f.t, anthropicSelfHostedSandboxID, id)
	return &apiv1.Sandbox{Id: id, State: apiv1.RuntimeState_RUNTIME_STATE_READY, Operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE}, nil
}

func (f *anthropicSelfHostedSandboxFixture) StartProcess(ctx context.Context, id string, req *guestpb.StartProcessRequest) (*guestpb.Process, error) {
	f.checkOwner(ctx)
	if req.GetCommand()[0] == "ps" {
		return &guestpb.Process{ProcessId: "ps", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
	}
	assert.NoError(f.t, ctx.Err(), "shutdown must drain the in-flight launch")
	assert.Equal(f.t, anthropicSelfHostedSandboxID, id)
	f.starts++
	return &guestpb.Process{ProcessId: "worker", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
}

func (f *anthropicSelfHostedSandboxFixture) StreamProcessOutput(ctx context.Context, id string, req *guestpb.StreamProcessOutputRequest, send func(*guestpb.ProcessOutput) error) error {
	f.checkOwner(ctx)
	assert.Equal(f.t, anthropicSelfHostedSandboxID, id)
	assert.Equal(f.t, "ps", req.GetProcessId())
	f.scans.Add(1)
	if f.onScan != nil {
		if err := f.onScan(); err != nil {
			return err
		}
	}
	if err := send(&guestpb.ProcessOutput{Output: &guestpb.ProcessOutput_Stdout{Stdout: []byte("guest-server\nant\nps\n")}}); err != nil {
		return err
	}
	return send(&guestpb.ProcessOutput{Output: &guestpb.ProcessOutput_Exit{Exit: &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_EXITED}}})
}

func (f *anthropicSelfHostedSandboxFixture) Get(ctx context.Context, id string) (*apiv1.Sandbox, error) {
	f.checkOwner(ctx)
	assert.Equal(f.t, anthropicSelfHostedSandboxID, id)
	return &apiv1.Sandbox{
		Id: id, Creator: f.owner, Name: f.name, State: apiv1.RuntimeState_RUNTIME_STATE_READY,
		Operation:       apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE,
		SandboxTemplate: &apiv1.ResourceReference{Namespace: "test", Name: "claude"},
		ExpiresAt:       timestamppb.New(time.Now().Add(time.Hour)),
	}, nil
}

func (f *anthropicSelfHostedSandboxFixture) Suspend(ctx context.Context, id string) (*apiv1.Sandbox, error) {
	f.checkOwner(ctx)
	assert.Equal(f.t, anthropicSelfHostedSandboxID, id)
	f.t.Error("controller restart must leave the running worker alone")
	f.suspends.Add(1)
	return &apiv1.Sandbox{
		Id: id, State: apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED,
		Operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE,
	}, nil
}

func TestAnthropicSelfHostedSandboxesAdoptsWorkerAcrossControllerRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	var polls atomic.Int32
	var stops atomic.Int32
	var recovering atomic.Bool
	var recoveryPolls atomic.Int32
	provisioning := make(chan struct{})
	service := &anthropicSelfHostedSandboxFixture{
		t: t, owner: auth.PrincipalID("kagent:anthropic-self-hosted-sandboxes", "env_test"),
		onCreate: func(launchCtx context.Context) {
			close(provisioning)
			<-ctx.Done()
			assert.NoError(t, launchCtx.Err(), "normal shutdown must not cancel provisioning")
		},
	}
	recoveryCtx, finishRecovery := context.WithTimeout(t.Context(), 5*time.Second)
	defer finishRecovery()
	service.onScan = func() error {
		if service.scans.Load() >= 3 {
			finishRecovery()
			return errors.New("end bounded recovery inspection")
		}
		return nil
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer environment-key", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/poll"):
			if recovering.Load() {
				if recoveryPolls.Add(1) == 1 {
					assert.EqualValues(t, 1, service.scans.Load(), "inspect the adopted worker before polling")
					_, _ = io.WriteString(w, `{"id":"work_next","environment_id":"env_test","secret":"scoped-secret","data":{"id":"session_test","type":"session"}}`)
					return
				}
				<-recoveryCtx.Done()
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
	cfg := AnthropicSelfHostedSandboxesConfig{EnvironmentID: "env_test", EnvironmentKey: "environment-key", BaseURL: server.URL, TemplateNamespace: "test", TemplateName: "claude", SandboxTTL: 12 * time.Hour}
	first, err := newAnthropicSelfHostedSandboxesRunnable(cfg, service, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	require.True(t, first.NeedLeaderElection(), "polling and collection must run only on the manager leader")
	second, err := newAnthropicSelfHostedSandboxesRunnable(cfg, service, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	assert.Equal(t, first.session.Principal().ID, second.session.Principal().ID, "replicas and restarts must reuse the same owner")
	require.NoError(t, first.Start(ctx))
	assert.Equal(t, 1, service.starts)
	recovering.Store(true)
	require.NoError(t, second.Start(recoveryCtx))
	assert.EqualValues(t, 3, service.scans.Load(), "incoming work must inspect and wait for the live adopted worker")
	assert.EqualValues(t, 1, service.creates.Load(), "recovery must reuse the original sandbox")
	assert.Equal(t, 1, service.starts, "new leader must adopt rather than start a duplicate worker")
	assert.Zero(t, stops.Load())
	assert.Zero(t, service.suspends.Load())
}

func TestAnthropicSelfHostedSandboxesFailuresDoNotStopManager(t *testing.T) {
	for _, phase := range []string{"discovery unavailable", "discovery forbidden", "polling unauthorized", "blocked discovery", "shutdown during backoff"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			var attempts, failures, successfulPolls atomic.Int32
			var failing atomic.Bool
			failing.Store(true)
			failed, recovered := make(chan struct{}), make(chan struct{})
			markFailure := func() {
				if failures.Add(1) == 1 {
					close(failed)
				}
			}
			service := &anthropicSelfHostedSandboxFixture{
				t: t, owner: auth.PrincipalID("kagent:anthropic-self-hosted-sandboxes", "env_test"),
				onList: func(callCtx context.Context) error {
					attempts.Add(1)
					if !failing.Load() || phase == "polling unauthorized" {
						return nil
					}
					markFailure()
					if phase == "blocked discovery" {
						<-callCtx.Done()
						return callCtx.Err()
					}
					if phase == "discovery forbidden" {
						return serviceerrors.NewPermissionDenied("secret-body", nil)
					}
					return serviceerrors.NewUnavailable("secret-body", nil)
				},
			}
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if !strings.HasSuffix(r.URL.Path, "/poll") {
					t.Errorf("unexpected upstream request: %s", r.URL.Path)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if failing.Load() {
					markFailure()
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusUnauthorized)
					_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"secret-body"}}`)
					return
				}
				if successfulPolls.Add(1) == 1 {
					close(recovered)
				}
				w.WriteHeader(http.StatusNoContent)
			}))
			defer upstream.Close()
			cfg := AnthropicSelfHostedSandboxesConfig{
				EnvironmentID: "env_test", EnvironmentKey: "environment-key", BaseURL: upstream.URL,
				TemplateNamespace: "test", TemplateName: "claude", SandboxTTL: time.Hour,
			}
			var logs bytes.Buffer
			launcher, err := newAnthropicSelfHostedSandboxesRunnable(cfg, service, slog.New(slog.NewTextHandler(&logs, nil)))
			require.NoError(t, err)

			reservation, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
			require.NoError(t, err)
			address := reservation.Addr().String()
			require.NoError(t, reservation.Close())
			mgr, err := manager.New(&rest.Config{Host: "http://127.0.0.1"}, manager.Options{
				Scheme: runtime.NewScheme(),
				MapperProvider: func(*rest.Config, *http.Client) (meta.RESTMapper, error) {
					return meta.NewDefaultRESTMapper(nil), nil
				},
				Metrics: metricsserver.Options{BindAddress: "0"}, HealthProbeBindAddress: address,
				Logger: logging.AsLogr(slog.New(slog.DiscardHandler)),
			})
			require.NoError(t, err)
			require.NoError(t, mgr.AddReadyzCheck("ping", healthz.Ping))
			require.NoError(t, mgr.Add(launcher))
			done := make(chan error, 1)
			go func() { done <- mgr.Start(ctx) }()
			t.Cleanup(func() {
				cancel()
				select {
				case err := <-done:
					assert.NoError(t, err, "optional launcher failures must not terminate the manager")
					assert.NotContains(t, logs.String(), "secret-body")
					assert.NotContains(t, logs.String(), "environment-key")
				case <-time.After(time.Second):
					t.Error("manager did not stop promptly")
				}
			})
			select {
			case <-failed:
			case <-ctx.Done():
				t.Fatal("launcher did not reach the injected failure")
			}
			client := &http.Client{Timeout: 250 * time.Millisecond}
			checkReady := func() bool {
				response, err := client.Get("http://" + address + "/readyz")
				if err != nil {
					return false
				}
				defer response.Body.Close()
				return response.StatusCode == http.StatusOK
			}
			require.Eventually(t, checkReady, time.Second, 10*time.Millisecond, "manager must remain ready during launcher failure")
			assert.Never(t, func() bool { return attempts.Load() > 1 }, 100*time.Millisecond, 10*time.Millisecond,
				"failed attempts must back off rather than spin")
			if phase == "blocked discovery" || phase == "shutdown during backoff" {
				return
			}
			failing.Store(false)
			select {
			case <-recovered:
			case <-ctx.Done():
				t.Fatal("launcher did not recover independently")
			}
			assert.GreaterOrEqual(t, attempts.Load(), int32(2), "retries must rediscover sandboxes before polling")
			assert.True(t, checkReady(), "manager must remain ready after recovery")
			assert.Zero(t, service.creates.Load())
			assert.Zero(t, service.suspends.Load())
		})
	}
}
