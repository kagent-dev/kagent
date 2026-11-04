package anthropicselfhostedsandboxes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

const testSandboxID = "ffed5612-907d-451a-86b8-24821da8e4ec"

func testConfig() Config {
	return Config{EnvironmentID: "env_test", EnvironmentKey: "standing-environment-secret", TemplateNamespace: "test", TemplateName: "claude", SandboxTTL: 12 * time.Hour}
}

func testWork(id string) *anthropic.BetaSelfHostedWork {
	return &anthropic.BetaSelfHostedWork{
		ID: id, EnvironmentID: "env_test", Secret: "opaque-scoped-work-secret",
		Data: anthropic.BetaSessionWorkData{ID: "session_test", Type: "session"},
	}
}

type sandboxFixture struct {
	list    func(context.Context, *apiv1.ListSandboxesRequest) (*apiv1.ListSandboxesResponse, error)
	output  func(context.Context, string, *guestpb.StreamProcessOutputRequest, func(*guestpb.ProcessOutput) error) error
	lookup  func(context.Context, string) (*apiv1.Sandbox, error)
	create  func(context.Context, *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error)
	resume  func(context.Context, string) (*apiv1.Sandbox, error)
	start   func(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error)
	scan    func(context.Context, string) (string, error)
	suspend func(context.Context, string) (*apiv1.Sandbox, error)
}

var _ SandboxService = (*sandboxFixture)(nil)

func (s *sandboxFixture) List(ctx context.Context, req *apiv1.ListSandboxesRequest) (*apiv1.ListSandboxesResponse, error) {
	if s.list != nil {
		return s.list(ctx, req)
	}
	return &apiv1.ListSandboxesResponse{}, nil
}

func (s *sandboxFixture) StreamProcessOutput(ctx context.Context, id string, req *guestpb.StreamProcessOutputRequest, send func(*guestpb.ProcessOutput) error) error {
	if s.output != nil {
		return s.output(ctx, id, req, send)
	}
	if s.scan != nil {
		output, err := s.scan(ctx, id)
		if err != nil {
			return err
		}
		if err := send(&guestpb.ProcessOutput{Output: &guestpb.ProcessOutput_Stdout{Stdout: []byte(output)}}); err != nil {
			return err
		}
	}
	return send(&guestpb.ProcessOutput{Output: &guestpb.ProcessOutput_Exit{Exit: &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_EXITED}}})
}

func (s *sandboxFixture) Get(ctx context.Context, id string) (*apiv1.Sandbox, error) {
	if s.lookup != nil {
		return s.lookup(ctx, id)
	}
	instance := readySandbox()
	instance.Id = id
	return instance, nil
}

func (s *sandboxFixture) Create(ctx context.Context, req *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
	if s.create != nil {
		return s.create(ctx, req)
	}
	return readySandbox(), nil
}

func (s *sandboxFixture) Resume(ctx context.Context, id string) (*apiv1.Sandbox, error) {
	return s.resume(ctx, id)
}

func (s *sandboxFixture) StartProcess(ctx context.Context, id string, req *guestpb.StartProcessRequest) (*guestpb.Process, error) {
	if req.Command[0] == "ps" && s.output == nil {
		return &guestpb.Process{ProcessId: "ps"}, nil
	}
	if s.start != nil {
		return s.start(ctx, id, req)
	}
	return &guestpb.Process{ProcessId: "process-test", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
}

func (s *sandboxFixture) Suspend(ctx context.Context, id string) (*apiv1.Sandbox, error) {
	if s.suspend != nil {
		return s.suspend(ctx, id)
	}
	sb := suspendedSandbox()
	sb.Id = id
	return sb, nil
}

func readySandbox() *apiv1.Sandbox {
	return &apiv1.Sandbox{Id: testSandboxID, Name: sandboxNamePrefix + "session_test", Creator: auth.PrincipalID("kagent:anthropic-self-hosted-sandboxes", testConfig().EnvironmentID),
		SandboxTemplate: &apiv1.ResourceReference{Namespace: "test", Name: "claude"},
		State:           apiv1.RuntimeState_RUNTIME_STATE_READY, Operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE}
}

func requireIdleSandbox(t *testing.T, launcher *Launcher, sessionID string) {
	t.Helper()
	require.Contains(t, launcher.processes, sessionID)
	entry := launcher.processes[sessionID]
	require.NotEmpty(t, entry.sandboxID)
	assert.Empty(t, entry.workID)
}

type workAPI struct {
	t                  *testing.T
	mu                 sync.Mutex
	queue              []*anthropic.BetaSelfHostedWork
	acks, stops, beats int
	tokens             map[string]string
	ttl                int64
	heartbeat          func(http.ResponseWriter, *http.Request, int)
	onEmpty            func()
	onStop             func()
	pollStatus         int
	stoppedWork        []string
	stopForces         []bool
	stop               func(http.ResponseWriter, *http.Request)
}

func (f *workAPI) serve(w http.ResponseWriter, r *http.Request) {
	assert.Equal(f.t, "Bearer "+testConfig().EnvironmentKey, r.Header.Get("Authorization"))
	assert.Empty(f.t, r.Header.Get("X-Api-Key"))
	assert.Contains(f.t, r.Header.Get("Anthropic-Beta"), "managed-agents-2026-04-01")
	w.Header().Set("Content-Type", "application/json")
	f.mu.Lock()
	switch {
	case strings.HasSuffix(r.URL.Path, "/poll"):
		assert.Equal(f.t, http.MethodGet, r.Method)
		assert.Equal(f.t, "30000", r.URL.Query().Get("reclaim_older_than_ms"))
		if len(f.queue) == 0 {
			f.mu.Unlock()
			if f.onEmpty != nil {
				f.onEmpty()
			}
			code := f.pollStatus
			if code == 0 {
				code = http.StatusNoContent
			}
			w.WriteHeader(code)
			if code != http.StatusNoContent {
				_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"rejected"}}`)
			}
			return
		}
		work := f.queue[0]
		f.queue = f.queue[1:]
		f.mu.Unlock()
		assert.NoError(f.t, json.NewEncoder(w).Encode(work))
	case strings.HasSuffix(r.URL.Path, "/ack"):
		f.acks++
		f.mu.Unlock()
		assert.Equal(f.t, http.MethodPost, r.Method)
		_, _ = io.WriteString(w, `{}`)
	case strings.HasSuffix(r.URL.Path, "/heartbeat"):
		f.beats++
		beat := f.beats
		if f.heartbeat != nil {
			f.mu.Unlock()
			f.heartbeat(w, r, beat)
			return
		}
		expected := noHeartbeat
		if token := f.tokens[r.URL.Path]; token != "" {
			expected = token
		}
		assert.Equal(f.t, expected, r.URL.Query().Get("expected_last_heartbeat"))
		assert.Equal(f.t, "60", r.URL.Query().Get("desired_ttl_seconds"))
		token := time.Now().UTC().Format(time.RFC3339Nano)
		if f.tokens == nil {
			f.tokens = make(map[string]string)
		}
		f.tokens[r.URL.Path] = token
		ttl := f.ttl
		f.mu.Unlock()
		assert.NoError(f.t, json.NewEncoder(w).Encode(anthropic.BetaSelfHostedWorkHeartbeatResponse{
			LastHeartbeat: token, TTLSeconds: ttl, LeaseExtended: true, State: "active",
		}))
	case strings.HasSuffix(r.URL.Path, "/stop"):
		var body struct {
			Force *bool `json:"force"`
		}
		assert.NoError(f.t, json.NewDecoder(r.Body).Decode(&body))
		if !assert.NotNil(f.t, body.Force) {
			f.mu.Unlock()
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.stops++
		parts := strings.Split(r.URL.Path, "/")
		f.stoppedWork = append(f.stoppedWork, parts[len(parts)-2])
		f.stopForces = append(f.stopForces, *body.Force)
		f.mu.Unlock()
		assert.Equal(f.t, http.MethodPost, r.Method)
		if f.onStop != nil {
			f.onStop()
		}
		if f.stop != nil {
			f.stop(w, r)
			return
		}
		_, _ = io.WriteString(w, `{}`)
	default:
		f.mu.Unlock()
		f.t.Errorf("unexpected anthropic self-hosted sandboxes request %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusNotFound)
	}
}

func newTestLauncher(t *testing.T, fixture SandboxService, api *workAPI) *Launcher {
	t.Helper()
	api.t = t
	if api.ttl == 0 {
		api.ttl = 60
	}
	server := httptest.NewServer(http.HandlerFunc(api.serve))
	t.Cleanup(server.Close)
	cfg := testConfig()
	cfg.BaseURL = server.URL
	launcher, err := New(cfg, fixture, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	return launcher
}

func TestServicePollAndHandoff(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "unrelated-organization-secret")
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	session := auth.NewCredentialSession(auth.Principal{ID: auth.PrincipalID("kagent:anthropic-self-hosted-sandboxes", testConfig().EnvironmentID)}, nil)
	ctx = auth.AuthSessionTo(ctx, session)
	assertSession := func(ctx context.Context) {
		t.Helper()
		actual, ok := auth.AuthSessionFrom(ctx)
		assert.True(t, ok)
		assert.Same(t, session, actual)
	}
	var creates []*apiv1.CreateSandboxRequest
	var starts []*guestpb.StartProcessRequest
	var mu sync.Mutex
	started := make(chan struct{}, 2)
	fixture := &sandboxFixture{
		create: func(ctx context.Context, req *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
			assertSession(ctx)
			mu.Lock()
			defer mu.Unlock()
			creates = append(creates, proto.Clone(req).(*apiv1.CreateSandboxRequest))
			return readySandbox(), nil
		},
		resume: func(ctx context.Context, id string) (*apiv1.Sandbox, error) {
			assertSession(ctx)
			assert.Equal(t, testSandboxID, id)
			return readySandbox(), nil
		},
		start: func(ctx context.Context, id string, req *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			assertSession(ctx)
			assert.Equal(t, testSandboxID, id)
			mu.Lock()
			starts = append(starts, proto.Clone(req).(*guestpb.StartProcessRequest))
			mu.Unlock()
			started <- struct{}{}
			return &guestpb.Process{ProcessId: req.Env["ANTHROPIC_WORK_ID"], State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
		},
	}
	var api *workAPI
	api = &workAPI{queue: []*anthropic.BetaSelfHostedWork{testWork("work_one"), testWork("work_two")}, onEmpty: func() {
		for range 2 {
			select {
			case <-started:
			case <-ctx.Done():
				return
			}
		}
		api.mu.Lock()
		assert.Zero(t, api.stops, "handed-off workers must not be stopped before shutdown")
		api.mu.Unlock()
		cancel()
	}}
	// Each assignment starts a distinct lease, even when it reuses a Sandbox.
	api.heartbeat = func(w http.ResponseWriter, r *http.Request, n int) {
		assert.Equal(t, noHeartbeat, r.URL.Query().Get("expected_last_heartbeat"))
		assert.NoError(t, json.NewEncoder(w).Encode(anthropic.BetaSelfHostedWorkHeartbeatResponse{
			LastHeartbeat: "exact-token", TTLSeconds: 60, LeaseExtended: true, State: "active",
		}))
	}
	launcher := newTestLauncher(t, fixture, api)
	launcher.config.SandboxTTL = 3 * time.Hour
	require.NoError(t, launcher.Start(ctx))
	require.Len(t, creates, 1, "later work reuses the saved session sandbox via Get")
	assert.Equal(t, 3*time.Hour, creates[0].GetTtl().AsDuration())
	require.Len(t, starts, 2)
	workIDs := make([]string, 0, len(starts))
	for _, start := range starts {
		workIDs = append(workIDs, start.Env["ANTHROPIC_WORK_ID"])
		assert.Equal(t, []string{"ant", "beta:worker", "run", "--workdir", "/data/workspace", "--log-format", "json", "--max-idle", "5m"}, start.Command)
		assert.Equal(t, "/data/workspace", start.Cwd)
		assert.Equal(t, 3*time.Hour, start.Timeout.AsDuration())
		assert.Equal(t, map[string]string{
			"ANTHROPIC_SESSION_ID": "session_test", "ANTHROPIC_ENVIRONMENT_ID": "env_test",
			"ANTHROPIC_WORK_ID":             start.Env["ANTHROPIC_WORK_ID"],
			"ANTHROPIC_WORK_SECRET":         "opaque-scoped-work-secret",
			"ANTHROPIC_WORK_LAST_HEARTBEAT": "exact-token", "ANTHROPIC_BASE_URL": launcher.config.BaseURL,
		}, start.Env)
	}
	assert.ElementsMatch(t, []string{"work_one", "work_two"}, workIDs)
	api.mu.Lock()
	defer api.mu.Unlock()
	assert.Equal(t, 2, api.acks)
	assert.Empty(t, api.stoppedWork, "workers survive shutdown")
}

func TestServiceConcurrentProvisioningShutdown(t *testing.T) {
	for _, tt := range []struct {
		name       string
		pollStatus int
	}{
		{name: "context canceled"},
		{name: "terminal poll failure", pollStatus: http.StatusUnauthorized},
	} {
		t.Run(tt.name, func(t *testing.T) {
			testCtx, timeout := context.WithTimeout(t.Context(), 5*time.Second)
			defer timeout()
			ctx, cancel := context.WithCancel(testCtx)
			defer cancel()
			firstProvisioning := make(chan struct{})
			releaseProvisioning := make(chan struct{})
			release := sync.OnceFunc(func() { close(releaseProvisioning) })
			defer release()
			started := make(chan string, 2)
			pollExit := make(chan struct{})
			firstWork, secondWork := testWork("work_first"), testWork("work_second")
			firstWork.Data.ID = "session_first"
			secondWork.Data.ID = "session_second"
			fixture := &sandboxFixture{
				create: func(ctx context.Context, req *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
					if req.Name == sandboxNamePrefix+firstWork.Data.ID {
						close(firstProvisioning)
						select {
						case <-releaseProvisioning:
						case <-testCtx.Done():
						}
						assert.NoError(t, ctx.Err(), "drain must not cancel accepted provisioning")
						return readySandbox(), nil
					}
					select {
					case <-firstProvisioning:
						return readySandbox(), nil
					case <-ctx.Done():
						return nil, ctx.Err()
					}
				},
				start: func(_ context.Context, _ string, req *guestpb.StartProcessRequest) (*guestpb.Process, error) {
					started <- req.Env["ANTHROPIC_WORK_ID"]
					return &guestpb.Process{ProcessId: "worker", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
				},
			}
			api := &workAPI{
				queue:      []*anthropic.BetaSelfHostedWork{firstWork, secondWork},
				pollStatus: tt.pollStatus,
				onEmpty: func() {
					select {
					case <-pollExit:
					case <-testCtx.Done():
					}
				},
			}
			launcher := newTestLauncher(t, fixture, api)
			done := make(chan error, 1)
			go func() { done <- launcher.Start(ctx) }()
			select {
			case workID := <-started:
				require.Equal(t, secondWork.ID, workID, "second worker must start while first Create is blocked")
			case <-testCtx.Done():
				t.Fatal("second assignment did not start while first Create was blocked")
			}
			require.NoError(t, ctx.Err())
			if tt.pollStatus == 0 {
				cancel()
			}
			close(pollExit)
			select {
			case err := <-done:
				t.Fatalf("launcher returned before pending provisioning finished: %v", err)
			case <-time.After(50 * time.Millisecond):
			}
			release()
			select {
			case err := <-done:
				if tt.pollStatus == 0 {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, "HTTP status 401")
					require.NoError(t, ctx.Err(), "terminal poll failure must cancel its derived context, not its caller")
				}
			case <-testCtx.Done():
				t.Fatal("launcher did not join draining provisioning")
			}
			select {
			case workID := <-started:
				assert.Equal(t, firstWork.ID, workID, "accepted provisioning must finish handoff")
			default:
				t.Fatal("draining provisioning did not hand off its worker")
			}
			api.mu.Lock()
			defer api.mu.Unlock()
			assert.Equal(t, 2, api.acks)
			assert.Empty(t, api.stoppedWork, "controller shutdown leaves leases and running workers alone")
		})
	}
}

func TestLaunchFailureCleanup(t *testing.T) {
	for _, tt := range []struct {
		name            string
		heartbeatStatus int
		startError      error
		createError     error
		heartbeatBody   string
		stops, starts   int
	}{
		{name: "definitive start failure", startError: status.Error(codes.InvalidArgument, "cannot exec"), stops: 1, starts: 1},
		{name: "independent canceled start", startError: context.Canceled, stops: 1, starts: 1},
		{name: "provisioning denied", createError: status.Error(codes.PermissionDenied, "denied"), stops: 1},
		{name: "independent canceled provisioning", createError: context.Canceled, stops: 1},
		{name: "service provisioning denied despite transient backend", createError: serviceerrors.NewPermissionDenied("denied", status.Error(codes.Unavailable, "backend")), stops: 1},
		{name: "service start unavailable is not retried", startError: serviceerrors.NewUnavailable("cannot start", nil), stops: 1, starts: 1},
		{name: "lease ownership lost", heartbeatStatus: http.StatusPreconditionFailed},
		{name: "uncertain heartbeat", heartbeatStatus: http.StatusServiceUnavailable},
		{name: "upstream stopping", heartbeatBody: `{"last_heartbeat":"last","ttl_seconds":60,"lease_extended":true,"state":"stopping"}`},
		{name: "lease not extended", heartbeatBody: `{"last_heartbeat":"last","ttl_seconds":60,"lease_extended":false,"state":"active"}`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			starts := 0
			fixture := &sandboxFixture{
				create: func(context.Context, *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
					if tt.createError != nil {
						return nil, tt.createError
					}
					return readySandbox(), nil
				},
				start: func(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error) {
					starts++
					return nil, tt.startError
				},
			}
			api := &workAPI{}
			if tt.heartbeatStatus != 0 {
				api.heartbeat = func(w http.ResponseWriter, _ *http.Request, _ int) {
					w.WriteHeader(tt.heartbeatStatus)
					_, _ = io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"rejected"}}`)
				}
			} else if tt.heartbeatBody != "" {
				api.heartbeat = func(w http.ResponseWriter, _ *http.Request, _ int) {
					_, _ = io.WriteString(w, tt.heartbeatBody)
				}
			}
			launcher := newTestLauncher(t, fixture, api)
			require.Error(t, launcher.launch(t.Context(), testWork("work_test")))
			assert.Equal(t, tt.stops, api.stops)
			for _, force := range api.stopForces {
				assert.True(t, force, "failed launches still require force cleanup")
			}
			assert.Equal(t, tt.starts, starts)
		})
	}
}

func TestShutdownLeavesProvisioningLeaseToExpire(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	fixture := &sandboxFixture{
		create: func(context.Context, *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
			cancel()
			return nil, context.Canceled
		},
		start: func(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			t.Error("must not hand off canceled provisioning")
			return nil, errors.New("unexpected start")
		},
	}
	api := &workAPI{}
	launcher := newTestLauncher(t, fixture, api)
	require.Error(t, launcher.launch(ctx, testWork("work_test")))
	assert.Zero(t, api.stops, "shutdown must not terminally stop a pending tool assignment")
}

func TestProvisionRejectsUnavailableSandbox(t *testing.T) {
	for _, tt := range []struct {
		name      string
		state     apiv1.RuntimeState
		operation apiv1.RuntimeOperation
	}{
		{name: "failed", state: apiv1.RuntimeState_RUNTIME_STATE_FAILED, operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE},
		{name: "expired", state: apiv1.RuntimeState_RUNTIME_STATE_DELETED, operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE},
		{name: "deleting", state: apiv1.RuntimeState_RUNTIME_STATE_DELETING, operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE},
		{name: "suspended but deleting", state: apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED, operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fixture := &sandboxFixture{
				create: func(context.Context, *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
					return &apiv1.Sandbox{Id: testSandboxID, State: tt.state, Operation: tt.operation}, nil
				},
			}
			launcher := newTestLauncher(t, fixture, &workAPI{})
			_, err := provisionTestSandbox(t, t.Context(), launcher)
			require.ErrorContains(t, err, "unavailable")
		})
	}
}

func TestProvisionReplacesExpiredReceiptsAcrossRestarts(t *testing.T) {
	base := createRequest(testConfig(), "session_test")
	original := readySandbox()
	original.State = apiv1.RuntimeState_RUNTIME_STATE_DELETED
	original.ExpiresAt = timestamppb.New(time.Now().Add(-2 * time.Hour))
	receipts := map[string]*apiv1.Sandbox{base.RequestId: original}
	instances := map[string]*apiv1.Sandbox{original.Id: original}
	inputs := map[string]*apiv1.CreateSandboxRequest{base.RequestId: proto.Clone(base).(*apiv1.CreateSandboxRequest)}
	replacementIDs := []string{"00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000003"}
	creations := 0
	fixture := &sandboxFixture{
		list: func(context.Context, *apiv1.ListSandboxesRequest) (*apiv1.ListSandboxesResponse, error) {
			page := &apiv1.ListSandboxesResponse{}
			for _, instance := range instances {
				if instance.GetState() != apiv1.RuntimeState_RUNTIME_STATE_DELETED {
					page.Sandboxes = append(page.Sandboxes, instance)
				}
			}
			return page, nil
		},
		lookup: func(_ context.Context, id string) (*apiv1.Sandbox, error) {
			return instances[id], nil
		},
		create: func(_ context.Context, req *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
			if existing := receipts[req.RequestId]; existing != nil {
				require.True(t, proto.Equal(inputs[req.RequestId], req), "receipt retries must preserve all inputs")
				if sandboxExpired(existing) {
					return nil, serviceerrors.NewFailedPrecondition("expired", nil)
				}
				return existing, nil
			}
			require.Less(t, creations, len(replacementIDs))
			assert.Equal(t, base.Name, req.Name, "replacement keeps the session's sandbox name")
			sb := readySandbox()
			sb.Id = replacementIDs[creations]
			sb.ExpiresAt = timestamppb.New(time.Now().Add(time.Hour))
			creations++
			receipts[req.RequestId] = sb
			instances[sb.Id] = sb
			inputs[req.RequestId] = proto.Clone(req).(*apiv1.CreateSandboxRequest)
			if creations == 1 {
				return nil, status.Error(codes.Unavailable, "replacement response lost after creation")
			}
			return sb, nil
		},
	}
	api := &workAPI{}
	launcher := newTestLauncher(t, fixture, api)
	require.NoError(t, launcher.adopt(t.Context()))
	first, err := provisionTestSandbox(t, t.Context(), launcher)
	require.NoError(t, err)
	require.Equal(t, replacementIDs[0], first.Id)
	assert.Len(t, receipts, 2, "creation retries must reuse one fresh receipt")

	launcher = newTestLauncher(t, fixture, api)
	require.NoError(t, launcher.adopt(t.Context()))
	recovered, err := provisionTestSandbox(t, t.Context(), launcher)
	require.NoError(t, err)
	assert.Equal(t, first.Id, recovered.Id)
	assert.Equal(t, 1, creations, "restart must discover the same replacement through Sandbox List")

	first.ExpiresAt = timestamppb.New(time.Now().Add(-time.Minute))
	first.State = apiv1.RuntimeState_RUNTIME_STATE_DELETED
	launcher = newTestLauncher(t, fixture, api)
	require.NoError(t, launcher.adopt(t.Context()))
	next, err := provisionTestSandbox(t, t.Context(), launcher)
	require.NoError(t, err)
	assert.Equal(t, replacementIDs[1], next.Id)
	assert.Equal(t, 2, creations)
	assert.Len(t, receipts, 3, "a deleted generation must not pin the session to its old receipt")
}

func TestLaunchReplacementDropsExpiredProcessTracking(t *testing.T) {
	expired := readySandbox()
	expired.ExpiresAt = timestamppb.New(time.Now().Add(-time.Minute))
	replacement := readySandbox()
	replacement.Id = "00000000-0000-4000-8000-000000000002"
	replacement.ExpiresAt = timestamppb.New(time.Now().Add(time.Hour))
	api := &workAPI{}
	launcher := newTestLauncher(t, &sandboxFixture{
		lookup: func(_ context.Context, id string) (*apiv1.Sandbox, error) {
			if id == expired.Id {
				return expired, nil
			}
			return replacement, nil
		},
		create: func(_ context.Context, req *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
			assert.Equal(t, expired.Name, req.Name)
			return replacement, nil
		},
		start: func(_ context.Context, id string, _ *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			assert.Equal(t, replacement.Id, id)
			return &guestpb.Process{ProcessId: "new-worker", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
		},
	}, api)
	trackTestProcesses(t, launcher, "old-worker")
	require.NoError(t, launcher.launch(t.Context(), testWork("new-work")))
	entry := launcher.processes["session_test"]
	require.NotNil(t, entry)
	assert.Equal(t, replacement.Id, entry.sandboxID)
	assert.Equal(t, "new-work", entry.workID)
}

func TestHeartbeatHandoffJoinsInFlightRequest(t *testing.T) {
	secondBeat := make(chan struct{})
	releaseBeat := make(chan struct{})
	started := make(chan string, 1)
	provisioned := make(chan struct{})
	api := &workAPI{}
	api.heartbeat = func(w http.ResponseWriter, r *http.Request, beat int) {
		token := "first-token"
		expected := noHeartbeat
		if beat == 2 {
			expected = token
			token = "final-token"
			close(secondBeat)
			select {
			case <-releaseBeat:
			case <-r.Context().Done():
				return
			}
		}
		assert.Equal(t, expected, r.URL.Query().Get("expected_last_heartbeat"))
		assert.NoError(t, json.NewEncoder(w).Encode(anthropic.BetaSelfHostedWorkHeartbeatResponse{
			LastHeartbeat: token, TTLSeconds: 4, LeaseExtended: true, State: "active",
		}))
	}
	fixture := &sandboxFixture{
		create: func(ctx context.Context, _ *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
			select {
			case <-secondBeat:
				close(provisioned)
				return readySandbox(), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		start: func(_ context.Context, _ string, req *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			started <- req.Env["ANTHROPIC_WORK_LAST_HEARTBEAT"]
			return &guestpb.Process{ProcessId: "worker"}, nil
		},
	}
	launcher := newTestLauncher(t, fixture, api)
	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	go func() { done <- launcher.launch(ctx, testWork("work_test")) }()
	select {
	case <-provisioned:
	case <-ctx.Done():
		t.Fatal("provisioning never completed")
	}
	select {
	case <-started:
		t.Fatal("started CLI before heartbeat handoff completed")
	default:
	}
	close(releaseBeat)
	require.NoError(t, <-done)
	assert.Equal(t, "final-token", <-started)
	assert.Equal(t, 2, api.beats)
	assert.Zero(t, api.stops)
}

func TestLeaseLossCancelsProvisioning(t *testing.T) {
	api := &workAPI{}
	api.heartbeat = func(w http.ResponseWriter, _ *http.Request, beat int) {
		if beat == 2 {
			w.WriteHeader(http.StatusPreconditionFailed)
			_, _ = io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"lost"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"last_heartbeat":"first","ttl_seconds":2,"lease_extended":true,"state":"active"}`)
	}
	fixture := &sandboxFixture{
		create: func(ctx context.Context, _ *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
		start: func(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			t.Error("must not start after losing lease")
			return nil, errors.New("unexpected start")
		},
	}
	launcher := newTestLauncher(t, fixture, api)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	require.Error(t, launcher.launch(ctx, testWork("work_test")))
	assert.Zero(t, api.stops)
	assert.Equal(t, 2, api.beats)
}

func TestProvisionRetriesMutations(t *testing.T) {
	for _, tt := range []struct {
		name                 string
		createErr, resumeErr error
	}{
		{name: "grpc", createErr: status.Error(codes.Unavailable, "retry"), resumeErr: status.Error(codes.Aborted, "retry resume")},
		{name: "service overrides backend", createErr: serviceerrors.NewUnavailable("retry", status.Error(codes.Internal, "backend")), resumeErr: serviceerrors.NewAborted("retry resume", status.Error(codes.FailedPrecondition, "backend"))},
		{name: "context deadline", createErr: fmt.Errorf("create timed out: %w", context.DeadlineExceeded), resumeErr: context.DeadlineExceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var requests []*apiv1.CreateSandboxRequest
			resumes := 0
			fixture := &sandboxFixture{
				create: func(_ context.Context, req *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
					requests = append(requests, proto.Clone(req).(*apiv1.CreateSandboxRequest))
					if len(requests) == 1 {
						return nil, tt.createErr
					}
					sb := readySandbox()
					sb.State = apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED
					return sb, nil
				},
				resume: func(context.Context, string) (*apiv1.Sandbox, error) {
					resumes++
					if resumes == 1 {
						return nil, tt.resumeErr
					}
					return readySandbox(), nil
				},
			}
			launcher := newTestLauncher(t, fixture, &workAPI{})
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			sandbox, err := provisionTestSandbox(t, ctx, launcher)
			require.NoError(t, err)
			assert.Equal(t, testSandboxID, sandbox.Id)
			require.Len(t, requests, 2)
			assert.True(t, proto.Equal(requests[0], requests[1]))
			assert.Equal(t, 2, resumes)
		})
	}
}

func TestProvisionStopsRetrying(t *testing.T) {
	terminalErr := serviceerrors.NewPermissionDenied("denied", nil)
	for _, tt := range []struct {
		name    string
		sandbox *apiv1.Sandbox
		err     error
		wantErr error
	}{
		{name: "permanent error", err: terminalErr, wantErr: terminalErr},
		{name: "transient error deadline", err: serviceerrors.NewUnavailable("retry", nil), wantErr: context.DeadlineExceeded},
		{name: "pending sandbox deadline", sandbox: &apiv1.Sandbox{Id: testSandboxID}, wantErr: context.DeadlineExceeded},
	} {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			fixture := &sandboxFixture{
				lookup: func(context.Context, string) (*apiv1.Sandbox, error) {
					if tt.sandbox != nil {
						instance := readySandbox()
						instance.State = tt.sandbox.State
						return instance, nil
					}
					return nil, errors.New("unexpected sandbox lookup")
				},
				create: func(context.Context, *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
					calls++
					return tt.sandbox, tt.err
				},
			}
			launcher := newTestLauncher(t, fixture, &workAPI{})
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			_, err := provisionTestSandbox(t, ctx, launcher)
			require.ErrorIs(t, err, tt.wantErr)
			assert.Equal(t, 1, calls)
		})
	}
}

func TestRetryable(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "service aborted", err: serviceerrors.NewAborted("retry", nil), want: true},
		{name: "service unavailable", err: serviceerrors.NewUnavailable("retry", nil), want: true},
		{name: "service exhausted", err: serviceerrors.NewResourceExhausted("retry", nil), want: true},
		{name: "service invalid", err: serviceerrors.NewInvalidArgument("terminal", nil)},
		{name: "service unauthenticated", err: serviceerrors.NewUnauthenticated("terminal", nil)},
		{name: "service denied", err: serviceerrors.NewPermissionDenied("terminal", nil)},
		{name: "service missing", err: serviceerrors.NewNotFound("terminal", nil)},
		{name: "service conflict", err: serviceerrors.NewAlreadyExists("terminal", nil)},
		{name: "service precondition", err: serviceerrors.NewFailedPrecondition("terminal", nil)},
		{name: "service internal", err: serviceerrors.NewInternal("terminal", nil)},
		{name: "outer service terminal overrides grpc", err: fmt.Errorf("provision: %w", serviceerrors.NewFailedPrecondition("terminal", status.Error(codes.Unavailable, "backend")))},
		{name: "outer service transient overrides grpc", err: serviceerrors.NewUnavailable("retry", status.Error(codes.PermissionDenied, "backend")), want: true},
		{name: "outer service terminal overrides deadline", err: serviceerrors.NewInternal("terminal", context.DeadlineExceeded)},
		{name: "outer service terminal overrides nested service", err: serviceerrors.NewPermissionDenied("terminal", serviceerrors.NewUnavailable("backend", nil))},
		{name: "grpc exhausted", err: status.Error(codes.ResourceExhausted, "retry"), want: true},
		{name: "grpc deadline", err: status.Error(codes.DeadlineExceeded, "retry"), want: true},
		{name: "wrapped deadline", err: fmt.Errorf("timeout: %w", context.DeadlineExceeded), want: true},
		{name: "canceled", err: context.Canceled},
		{name: "unknown", err: errors.New("unexpected")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, retryable(tt.err))
		})
	}
}

func TestLaunchServiceErrorDoesNotLeakSecret(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	fixture := &sandboxFixture{
		create: func(context.Context, *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
			return nil, serviceerrors.NewPermissionDenied("opaque-scoped-work-secret", status.Error(codes.Unavailable, "standing-environment-secret"))
		},
	}
	stopped := make(chan struct{})
	api := &workAPI{
		queue:  []*anthropic.BetaSelfHostedWork{testWork("work_test")},
		onStop: func() { close(stopped) },
		onEmpty: func() {
			select {
			case <-stopped:
				cancel()
			case <-ctx.Done():
			}
		},
	}
	launcher := newTestLauncher(t, fixture, api)
	var logs bytes.Buffer
	launcher.log = slog.New(slog.NewTextHandler(&logs, nil))
	require.NoError(t, launcher.Start(ctx))
	assert.Contains(t, logs.String(), "service_code=permission_denied")
	assert.Contains(t, logs.String(), "grpc_code=Unavailable")
	assert.Contains(t, logs.String(), "http_status=0")
	assert.NotContains(t, logs.String(), testConfig().EnvironmentKey)
	assert.NotContains(t, logs.String(), testWork("work_test").Secret)
	assert.Equal(t, 1, api.stops)
}

func TestPermanentPollingErrorDoesNotLeakSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, `{"type":"error","error":{"type":"authentication_error","message":"opaque-scoped-work-secret"}}`)
	}))
	defer server.Close()
	cfg := testConfig()
	cfg.BaseURL = server.URL
	var logs bytes.Buffer
	launcher, err := New(cfg, &sandboxFixture{}, slog.New(slog.NewTextHandler(&logs, nil)))
	require.NoError(t, err)
	err = launcher.Start(t.Context())
	require.ErrorContains(t, err, "HTTP status 401")
	assert.NotContains(t, err.Error(), "opaque-scoped-work-secret")
	// SDK log behavior is checked too: it must not expose upstream bodies.
	assert.NotContains(t, logs.String(), cfg.EnvironmentKey)
	assert.NotContains(t, logs.String(), "opaque-scoped-work-secret")
}
