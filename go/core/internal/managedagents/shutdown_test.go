package managedagents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/anthropics/anthropic-sdk-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

func TestShutdownDrainsThenEscalates(t *testing.T) {
	for _, tt := range []struct {
		name        string
		running     bool
		forceFails  bool
		wantForces  []bool
		wantSuspend int
	}{
		{name: "graceful exit", wantForces: []bool{false}, wantSuspend: 1},
		{name: "force then suspend running worker", running: true, wantForces: []bool{false, true}, wantSuspend: 1},
		{name: "failed force leaves running sandbox", running: true, forceFails: true, wantForces: []bool{false, true}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var requests atomic.Int32
			api := &workAPI{
				stop: func(w http.ResponseWriter, r *http.Request) {
					assert.NoError(t, r.Context().Err())
					n := requests.Add(1)
					if n == 2 && tt.forceFails {
						w.WriteHeader(http.StatusForbidden)
						_, _ = io.WriteString(w, `{"type":"error","error":{"type":"permission_error","message":"secret-body"}}`)
						return
					}
					_, _ = io.WriteString(w, `{}`)
				},
			}
			suspends := 0
			launcher := newTestLauncher(t, &sandboxFixture{
				get: func(ctx context.Context, _ string, _ *guestpb.GetProcessRequest) (*guestpb.Process, error) {
					assert.NoError(t, ctx.Err())
					assert.Positive(t, requests.Load(), "stop must be requested before observing worker exit")
					state := guestpb.ProcessState_PROCESS_STATE_EXITED
					if tt.running {
						state = guestpb.ProcessState_PROCESS_STATE_RUNNING
					}
					return &guestpb.Process{State: state}, nil
				},
				suspend: func(ctx context.Context, _ string) (*apiv1.Sandbox, error) {
					assert.NoError(t, ctx.Err())
					if tt.running {
						assert.Equal(t, int32(2), requests.Load(), "force stop must precede suspension of running processes")
					}
					suspends++
					return suspendedSandbox(), nil
				},
			}, api)
			trackTestProcesses(t, launcher, "worker")
			launcher.shutdown(t.Context(), 30*time.Millisecond, 100*time.Millisecond)
			assert.Equal(t, tt.wantForces, api.stopForces)
			assert.Equal(t, tt.wantSuspend, suspends)
			assert.Equal(t, tt.forceFails, len(launcher.trackedSessions()) != 0)
		})
	}
}

func TestShutdownRetriesPendingSuspension(t *testing.T) {
	suspends := 0
	launcher := newTestLauncher(t, &sandboxFixture{
		suspend: func(context.Context, string) (*apiv1.Sandbox, error) {
			suspends++
			if suspends == 1 {
				return &apiv1.Sandbox{Id: testSandboxID, State: apiv1.RuntimeState_RUNTIME_STATE_READY, Operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND}, nil
			}
			return suspendedSandbox(), nil
		},
	}, &workAPI{})
	trackTestProcesses(t, launcher, "worker")
	launcher.shutdown(t.Context(), 2*time.Second, time.Second)
	assert.Equal(t, 2, suspends)
	assert.Empty(t, launcher.trackedSessions())
}

func TestShutdownStopsOnlyTrackedLocalHandoffs(t *testing.T) {
	starts, gets, suspends := 0, 0, 0
	gced := testWork("work_gced")
	gced.Data.ID = "session_gced"
	api := &workAPI{
		heartbeat: func(w http.ResponseWriter, r *http.Request, _ int) {
			if strings.Contains(r.URL.Path, "/work_unowned/") {
				w.WriteHeader(http.StatusPreconditionFailed)
				_, _ = io.WriteString(w, `{"type":"error","error":{"type":"api_error","message":"lease lost"}}`)
				return
			}
			_, _ = io.WriteString(w, `{"last_heartbeat":"lease","ttl_seconds":60,"lease_extended":true,"state":"active"}`)
		},
	}
	launcher := newTestLauncher(t, &sandboxFixture{
		create: func(_ context.Context, req *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
			sb := readySandbox()
			if req.RequestId == createRequest(testConfig(), gced.Data.ID).RequestId {
				sb.Id = "gced-sandbox"
			}
			return sb, nil
		},
		start: func(_ context.Context, _ string, req *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			if req.Env["ANTHROPIC_WORK_ID"] == "work_failed" {
				return nil, errors.New("definitive launch failure")
			}
			starts++
			return &guestpb.Process{ProcessId: fmt.Sprintf("process-%d", starts), State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
		},
		get: func(_ context.Context, id string, _ *guestpb.GetProcessRequest) (*guestpb.Process, error) {
			gets++
			assert.Equal(t, "gced-sandbox", id)
			return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_EXITED}, nil
		},
		suspend: func(_ context.Context, id string) (*apiv1.Sandbox, error) {
			suspends++
			sb := suspendedSandbox()
			sb.Id = id
			return sb, nil
		},
	}, api)
	for _, work := range []*anthropic.BetaSelfHostedWork{
		testWork("work_shared"), testWork("work_shared"), testWork("work_other"), gced,
	} {
		require.NoError(t, launcher.launch(t.Context(), work))
	}
	require.Error(t, launcher.launch(t.Context(), testWork("work_failed")))
	require.Error(t, launcher.launch(t.Context(), testWork("work_unowned")))
	launcher.collectSandbox(t.Context(), gced.Data.ID)
	require.NotContains(t, launcher.processes, gced.Data.ID)
	require.Equal(t, []string{"work_failed"}, api.stoppedWork, "successful handoffs are untouched before shutdown")
	require.Equal(t, []bool{true}, api.stopForces)

	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	launcher.stopWorkers(ctx, false)
	assert.ElementsMatch(t, []string{"work_failed", "work_shared", "work_other"}, api.stoppedWork)
	assert.Equal(t, []bool{true, false, false}, api.stopForces)
	assert.Equal(t, 1, gets, "shutdown must not inspect or wait for guest processes")
	assert.Equal(t, 1, suspends, "only the explicit pre-shutdown collection may suspend")
}

func TestShutdownRegistersSuccessfulHandoffAfterCancellation(t *testing.T) {
	for _, tt := range []struct {
		name       string
		pollStatus int
	}{
		{name: "controller canceled"},
		{name: "terminal poll error", pollStatus: http.StatusUnauthorized},
	} {
		t.Run(tt.name, func(t *testing.T) {
			testCtx, timeout := context.WithTimeout(t.Context(), 5*time.Second)
			defer timeout()
			ctx, cancel := context.WithCancel(testCtx)
			defer cancel()
			starting := make(chan struct{})
			api := &workAPI{
				queue:      []*anthropic.BetaSelfHostedWork{testWork("work_late_success")},
				pollStatus: tt.pollStatus,
				onEmpty: func() {
					select {
					case <-starting:
						if tt.pollStatus == 0 {
							cancel()
						}
					case <-testCtx.Done():
					}
				},
			}
			if tt.pollStatus != 0 {
				api.stop = func(w http.ResponseWriter, _ *http.Request) {
					w.WriteHeader(http.StatusForbidden)
					_, _ = io.WriteString(w, `{"type":"error","error":{"type":"permission_error","message":"opaque-scoped-work-secret standing-environment-secret"}}`)
				}
			}
			launcher := newTestLauncher(t, &sandboxFixture{
				start: func(ctx context.Context, _ string, _ *guestpb.StartProcessRequest) (*guestpb.Process, error) {
					close(starting)
					<-ctx.Done()
					// StartProcess's definitive successful response must be tracked
					// even though the launch context has already been canceled.
					return &guestpb.Process{ProcessId: "late-worker", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
				},
				get: func(context.Context, string, *guestpb.GetProcessRequest) (*guestpb.Process, error) {
					return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_EXITED}, nil
				},
			}, api)
			var logs bytes.Buffer
			launcher.log = slog.New(slog.NewTextHandler(&logs, nil))
			err := launcher.Start(ctx)
			if tt.pollStatus == 0 {
				require.NoError(t, err)
				require.ErrorIs(t, ctx.Err(), context.Canceled)
			} else {
				require.EqualError(t, err, "managed agents poller stopped (HTTP status 401)")
				require.NoError(t, ctx.Err())
				assert.Contains(t, logs.String(), "failed to request managed agent stop")
				assert.Contains(t, logs.String(), "http_status=403")
			}
			assert.Equal(t, []string{"work_late_success"}, api.stoppedWork, "stop requests must use a fresh uncanceled context")
			assert.Equal(t, []bool{false}, api.stopForces)
			assert.NotContains(t, logs.String(), testConfig().EnvironmentKey)
			assert.NotContains(t, logs.String(), testWork("unused").Secret)
		})
	}
}

func TestShutdownParallelStopsShareDeadlineAndLogSafeFailures(t *testing.T) {
	const requests = 3
	allStarted := make(chan struct{})
	started := make(chan struct{}, requests)
	var release sync.Once
	// A blocked handler must not keep the test server alive after a failure.
	unblock := make(chan struct{})
	defer release.Do(func() { close(unblock) })
	api := &workAPI{
		stop: func(w http.ResponseWriter, r *http.Request) {
			started <- struct{}{}
			select {
			case <-allStarted:
			case <-r.Context().Done():
				return
			case <-unblock:
				return
			}
			switch {
			case strings.Contains(r.URL.Path, "/work_blocked/"):
				select {
				case <-r.Context().Done():
				case <-unblock:
				}
			case strings.Contains(r.URL.Path, "/work_failed/"):
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, `{"type":"error","error":{"type":"permission_error","message":"opaque-scoped-work-secret standing-environment-secret"}}`)
			default:
				_, _ = io.WriteString(w, `{}`)
			}
		},
	}
	launcher := newTestLauncher(t, &sandboxFixture{}, api)
	// Register actual successful handoffs; the stop failure is not a launch failure.
	starts := 0
	launcher.sandboxes = &sandboxFixture{
		start: func(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			starts++
			return &guestpb.Process{ProcessId: fmt.Sprintf("worker-%d", starts)}, nil
		},
	}
	for _, id := range []string{"work_blocked", "work_failed", "work_ok"} {
		require.NoError(t, launcher.launch(t.Context(), testWork(id)))
	}
	var logs bytes.Buffer
	launcher.log = slog.New(slog.NewTextHandler(&logs, nil))
	ctx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
	defer cancel()
	done := make(chan struct{})
	go func() {
		launcher.stopWorkers(ctx, false)
		close(done)
	}()
	// All three handlers must be entered before any handler may finish.
	for range requests {
		select {
		case <-started:
		case <-ctx.Done():
			release.Do(func() { close(unblock) })
			<-done
			t.Fatal("stop requests did not run independently in parallel")
		}
	}
	close(allStarted)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		release.Do(func() { close(unblock) })
		<-done
		t.Fatal("stop requests ignored their shared deadline")
	}
	release.Do(func() { close(unblock) })
	assert.ErrorIs(t, ctx.Err(), context.DeadlineExceeded)
	assert.ElementsMatch(t, []string{"work_blocked", "work_failed", "work_ok"}, api.stoppedWork)
	assert.Equal(t, []bool{false, false, false}, api.stopForces)
	assert.Equal(t, 2, strings.Count(logs.String(), "failed to request managed agent stop"))
	assert.Contains(t, logs.String(), "http_status=403")
	assert.Contains(t, logs.String(), "error_category=deadline_exceeded")
	assert.NotContains(t, logs.String(), testConfig().EnvironmentKey)
	assert.NotContains(t, logs.String(), testWork("unused").Secret)
}
