package managedagents

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

func suspendedSandbox() *apiv1.Sandbox {
	return &apiv1.Sandbox{
		Id: testSandboxID, State: apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED,
		Operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE,
	}
}

func trackTestProcesses(t *testing.T, launcher *Launcher, processID string) {
	t.Helper()
	entry, err := launcher.lockSandbox(t.Context(), "session_test")
	require.NoError(t, err)
	defer launcher.unlockSandbox("session_test", entry)
	entry.sandboxID = testSandboxID
	entry.processID = processID
	entry.workID = "work_" + processID
}

func TestCollectorProcessStates(t *testing.T) {
	for _, tt := range []struct {
		name    string
		process *guestpb.Process
		err     error
		suspend bool
	}{
		{name: "running", process: &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_RUNNING}},
		{name: "unknown", process: &guestpb.Process{}},
		{name: "empty response"},
		{name: "exited zero", process: &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_EXITED}, suspend: true},
		{name: "exited nonzero", process: &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_EXITED, ExitCode: 1}, suspend: true},
		{name: "guest not found", err: fmt.Errorf("guest: %w", status.Error(codes.NotFound, "secret-body")), suspend: true},
		{name: "service not found", err: serviceerrors.NewNotFound("secret-body", nil), suspend: true},
		{name: "service not found overrides guest", err: serviceerrors.NewNotFound("secret-body", status.Error(codes.Unavailable, "secret-body")), suspend: true},
		{name: "unavailable", err: status.Error(codes.Unavailable, "secret-body")},
		{name: "deadline", err: context.DeadlineExceeded},
		{name: "canceled", err: context.Canceled},
		{name: "permission denied", err: status.Error(codes.PermissionDenied, "secret-body")},
		{name: "unauthenticated", err: serviceerrors.NewUnauthenticated("secret-body", nil)},
		{name: "expired or suspended", err: serviceerrors.NewFailedPrecondition("secret-body", nil)},
		{name: "permission overrides guest not found", err: serviceerrors.NewPermissionDenied("secret-body", status.Error(codes.NotFound, "secret-body"))},
		{name: "unavailable overrides guest not found", err: serviceerrors.NewUnavailable("secret-body", status.Error(codes.NotFound, "secret-body"))},
		{name: "outer service overrides inner service", err: serviceerrors.NewUnavailable("secret-body", serviceerrors.NewNotFound("secret-body", nil))},
	} {
		t.Run(tt.name, func(t *testing.T) {
			suspends := 0
			fixture := &sandboxFixture{
				get: func(ctx context.Context, id string, req *guestpb.GetProcessRequest) (*guestpb.Process, error) {
					assert.Equal(t, testSandboxID, id)
					assert.Equal(t, "worker", req.GetProcessId())
					deadline, ok := ctx.Deadline()
					assert.True(t, ok)
					assert.LessOrEqual(t, time.Until(deadline), collectionTimeout)
					return tt.process, tt.err
				},
				suspend: func(ctx context.Context, id string) (*apiv1.Sandbox, error) {
					assert.Equal(t, testSandboxID, id)
					_, ok := ctx.Deadline()
					assert.True(t, ok)
					suspends++
					return suspendedSandbox(), nil
				},
			}
			launcher := newTestLauncher(t, fixture, &workAPI{})
			var logs bytes.Buffer
			launcher.log = slog.New(slog.NewTextHandler(&logs, nil))
			trackTestProcesses(t, launcher, "worker")
			launcher.collect(t.Context())
			assert.NotContains(t, logs.String(), "secret-body")
			if tt.suspend {
				assert.Equal(t, 1, suspends)
				requireIdleSandbox(t, launcher, "session_test")
			} else {
				assert.Zero(t, suspends)
				assert.Contains(t, launcher.processes, "session_test")
			}
		})
	}
}

func TestCollectorRetriesUnfinishedSuspensionWithoutGuestAccess(t *testing.T) {
	for _, tt := range []struct {
		name     string
		response *apiv1.Sandbox
		err      error
		pending  bool
	}{
		{name: "transient failure after durable intent", err: serviceerrors.NewUnavailable("secret-body", nil), pending: true},
		{name: "transient failure before durable intent", err: serviceerrors.NewUnavailable("secret-body", nil)},
		{name: "ambiguous timeout", err: context.DeadlineExceeded, pending: true},
		{name: "vanished sandbox", err: serviceerrors.NewNotFound("secret-body", nil)},
		{name: "ready", response: readySandbox()},
		{name: "pending", response: &apiv1.Sandbox{Id: testSandboxID, State: apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED, Operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND}, pending: true},
		{name: "deleted", response: &apiv1.Sandbox{Id: testSandboxID, State: apiv1.RuntimeState_RUNTIME_STATE_DELETED}},
		{name: "empty response"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			gets, suspends := 0, 0
			launcher := newTestLauncher(t, &sandboxFixture{
				lookup: func(context.Context, string) (*apiv1.Sandbox, error) {
					instance := readySandbox()
					if suspends > 0 && tt.pending {
						instance.State = apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED
						instance.Operation = apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND
					}
					return instance, nil
				},
				get: func(context.Context, string, *guestpb.GetProcessRequest) (*guestpb.Process, error) {
					gets++
					return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_EXITED}, nil
				},
				suspend: func(context.Context, string) (*apiv1.Sandbox, error) {
					suspends++
					if suspends == 1 {
						return tt.response, tt.err
					}
					return suspendedSandbox(), nil
				},
			}, &workAPI{})
			var logs bytes.Buffer
			launcher.log = slog.New(slog.NewTextHandler(&logs, nil))
			trackTestProcesses(t, launcher, "worker")
			launcher.collect(t.Context())
			require.Contains(t, launcher.processes, "session_test")
			launcher.collect(t.Context())
			wantGets := 2
			if tt.pending {
				wantGets = 1
			}
			assert.Equal(t, wantGets, gets, "only persisted suspension intent may bypass guest inspection")
			assert.Equal(t, 2, suspends)
			requireIdleSandbox(t, launcher, "session_test")
			assert.NotContains(t, logs.String(), "secret-body")
		})
	}
}

func TestLaunchTracksOnlySuccessfulCLIStarts(t *testing.T) {
	for _, tt := range []struct {
		name    string
		process *guestpb.Process
		err     error
		tracked bool
	}{
		{name: "running", process: &guestpb.Process{ProcessId: "worker", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, tracked: true},
		{name: "immediate success", process: &guestpb.Process{ProcessId: "worker", State: guestpb.ProcessState_PROCESS_STATE_EXITED}, tracked: true},
		{name: "immediate failure", process: &guestpb.Process{ProcessId: "worker", State: guestpb.ProcessState_PROCESS_STATE_EXITED, ExitCode: 1}},
		{name: "no ID", process: &guestpb.Process{}},
		{name: "start failed", err: errors.New("failed")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			launcher := newTestLauncher(t, &sandboxFixture{
				start: func(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error) {
					return tt.process, tt.err
				},
			}, &workAPI{})
			err := launcher.launch(t.Context(), testWork("work_test"))
			if tt.tracked {
				require.NoError(t, err)
				require.Contains(t, launcher.processes, "session_test")
				assert.Equal(t, "work_test", launcher.processes["session_test"].workID)
				assert.Equal(t, "worker", launcher.processes["session_test"].processID)
				assert.Nil(t, launcher.processes["session_test"].busy)
			} else {
				require.Error(t, err)
				requireIdleSandbox(t, launcher, "session_test")
			}
		})
	}
}

func TestLaunchFinishesPendingSuspensionBeforeHandoff(t *testing.T) {
	starts, gets, suspends := 0, 0, 0
	instance := readySandbox()
	launcher := newTestLauncher(t, &sandboxFixture{
		lookup: func(context.Context, string) (*apiv1.Sandbox, error) { return instance, nil },
		start: func(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			starts++
			return &guestpb.Process{ProcessId: fmt.Sprintf("worker-%d", starts), State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
		},
		get: func(context.Context, string, *guestpb.GetProcessRequest) (*guestpb.Process, error) {
			gets++
			if starts == 1 {
				return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_EXITED}, nil
			}
			return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
		},
		suspend: func(context.Context, string) (*apiv1.Sandbox, error) {
			suspends++
			if suspends == 1 {
				instance.Operation = apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND
				return nil, serviceerrors.NewUnavailable("pending", nil)
			}
			instance.Operation = apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE
			return suspendedSandbox(), nil
		},
	}, &workAPI{})
	require.NoError(t, launcher.launch(t.Context(), testWork("work_first")))
	launcher.collect(t.Context())
	require.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND, instance.Operation)
	require.NoError(t, launcher.launch(t.Context(), testWork("work_second")))
	assert.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE, instance.Operation)
	assert.Equal(t, "worker-2", launcher.processes["session_test"].processID)
	assert.Equal(t, "work_second", launcher.processes["session_test"].workID)
	launcher.collect(t.Context())
	assert.Equal(t, 2, gets)
	assert.Equal(t, 2, suspends, "handoff must finish the pending suspension rather than replace its state")
}

func TestCollectorExcludesConcurrentLaunchUntilSuspended(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	inspecting, finishInspect := make(chan struct{}), make(chan struct{})
	suspending, finishSuspend := make(chan struct{}), make(chan struct{})
	heartbeat := make(chan struct{})
	var creates, resumes, starts atomic.Int32
	var suspended atomic.Bool
	launcher := newTestLauncher(t, &sandboxFixture{
		lookup: func(context.Context, string) (*apiv1.Sandbox, error) {
			instance := readySandbox()
			if suspended.Load() {
				instance.State = apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED
			}
			return instance, nil
		},
		get: func(ctx context.Context, _ string, _ *guestpb.GetProcessRequest) (*guestpb.Process, error) {
			close(inspecting)
			select {
			case <-finishInspect:
				return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_EXITED}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		suspend: func(ctx context.Context, _ string) (*apiv1.Sandbox, error) {
			close(suspending)
			select {
			case <-finishSuspend:
				suspended.Store(true)
				return suspendedSandbox(), nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		create: func(context.Context, *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
			creates.Add(1)
			return suspendedSandbox(), nil
		},
		resume: func(context.Context, string) (*apiv1.Sandbox, error) {
			resumes.Add(1)
			suspended.Store(false)
			return readySandbox(), nil
		},
		start: func(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			starts.Add(1)
			return &guestpb.Process{ProcessId: "new-worker", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
		},
	}, &workAPI{heartbeat: func(w http.ResponseWriter, _ *http.Request, _ int) {
		_, _ = w.Write([]byte(`{"last_heartbeat":"token","ttl_seconds":60,"lease_extended":true,"state":"active"}`))
		close(heartbeat)
	}})
	trackTestProcesses(t, launcher, "old-worker")
	collected := make(chan struct{})
	go func() {
		launcher.collectSandbox(ctx, "session_test")
		close(collected)
	}()
	select {
	case <-inspecting:
	case <-ctx.Done():
		t.Fatal("collector did not inspect the process")
	}
	launched := make(chan error, 1)
	go func() { launched <- launcher.launch(ctx, testWork("work_new")) }()
	select {
	case <-heartbeat:
		t.Fatal("a launch must wait for the active session operation")
	default:
	}
	assert.Never(t, func() bool { return creates.Load() != 0 }, 25*time.Millisecond, time.Millisecond,
		"Create/Resume must wait while the collector checks processes")
	close(finishInspect)
	select {
	case <-suspending:
	case <-ctx.Done():
		t.Fatal("collector did not suspend")
	}
	assert.Never(t, func() bool { return creates.Load() != 0 }, 25*time.Millisecond, time.Millisecond,
		"launch must remain blocked until Suspend returns")
	assert.Zero(t, resumes.Load())
	assert.Zero(t, starts.Load())
	close(finishSuspend)
	select {
	case <-collected:
	case <-ctx.Done():
		t.Fatal("collector did not finish")
	}
	select {
	case err := <-launched:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("new launch did not finish")
	}
	assert.Zero(t, creates.Load(), "reuse must use the retained sandbox association")
	assert.EqualValues(t, 1, resumes.Load())
	assert.EqualValues(t, 1, starts.Load())
	require.Contains(t, launcher.processes, "session_test")
	assert.Equal(t, "new-worker", launcher.processes["session_test"].processID)
	assert.Equal(t, "work_new", launcher.processes["session_test"].workID)
	assert.Nil(t, launcher.processes["session_test"].busy)
}

func TestCollectorWaitsForInFlightStartToBeTracked(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	starting, finishStart := make(chan struct{}), make(chan struct{})
	var gets, suspends atomic.Int32
	launcher := newTestLauncher(t, &sandboxFixture{
		start: func(ctx context.Context, _ string, _ *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			close(starting)
			select {
			case <-finishStart:
				return &guestpb.Process{ProcessId: "new-worker", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		},
		get: func(_ context.Context, _ string, req *guestpb.GetProcessRequest) (*guestpb.Process, error) {
			gets.Add(1)
			if req.GetProcessId() == "new-worker" {
				return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
			}
			return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_EXITED}, nil
		},
		suspend: func(context.Context, string) (*apiv1.Sandbox, error) {
			suspends.Add(1)
			return suspendedSandbox(), nil
		},
	}, &workAPI{})
	trackTestProcesses(t, launcher, "old-worker")
	launched := make(chan error, 1)
	go func() { launched <- launcher.launch(ctx, testWork("work_new")) }()
	select {
	case <-starting:
	case <-ctx.Done():
		t.Fatal("launch did not start the process")
	}
	collected := make(chan struct{})
	go func() {
		launcher.collect(ctx)
		close(collected)
	}()
	before := gets.Load()
	assert.Never(t, func() bool { return gets.Load() != before }, 25*time.Millisecond, time.Millisecond,
		"collector must wait until the newly started process is registered")
	close(finishStart)
	select {
	case err := <-launched:
		require.NoError(t, err)
	case <-ctx.Done():
		t.Fatal("launch did not finish")
	}
	select {
	case <-collected:
	case <-ctx.Done():
		t.Fatal("collector did not finish")
	}
	assert.EqualValues(t, 2, gets.Load())
	assert.EqualValues(t, 1, suspends.Load(), "only the previous exited CLI may have been suspended")
	require.Contains(t, launcher.processes, "session_test")
	assert.Equal(t, "new-worker", launcher.processes["session_test"].processID)
	assert.Nil(t, launcher.processes["session_test"].busy)
}

func TestSandboxGateCancellationAndRecordRemoval(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	launcher := newTestLauncher(t, &sandboxFixture{}, &workAPI{})
	held, err := launcher.lockSandbox(ctx, "session_test")
	require.NoError(t, err)
	waitCtx, stopWaiting := context.WithTimeout(ctx, 20*time.Millisecond)
	defer stopWaiting()
	_, err = launcher.lockSandbox(waitCtx, "session_test")
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Same(t, held, launcher.processes["session_test"], "a canceled waiter must not remove the held gate")

	waiting := make(chan *sandboxWorker, 2)
	for range 2 {
		go func() {
			entry, err := launcher.lockSandbox(ctx, "session_test")
			assert.NoError(t, err)
			waiting <- entry
		}()
	}
	launcher.unlockSandbox("session_test", held)
	var first *sandboxWorker
	select {
	case first = <-waiting:
		require.NotNil(t, first)
		require.NotSame(t, held, first, "empty records should be removed")
	case <-ctx.Done():
		t.Fatal("waiting operation did not acquire the released gate")
	}
	select {
	case <-waiting:
		t.Fatal("waiters acquired separate gates for the same sandbox")
	case <-time.After(25 * time.Millisecond):
	}
	launcher.unlockSandbox("session_test", first)
	select {
	case second := <-waiting:
		require.NotNil(t, second)
		assert.NotSame(t, first, second)
		launcher.unlockSandbox("session_test", second)
	case <-ctx.Done():
		t.Fatal("second waiting operation did not acquire the released gate")
	}
	assert.Empty(t, launcher.processes)
}

func TestCollectorCancellation(t *testing.T) {
	for _, phase := range []string{"lookup", "suspend", "gate"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			calling := make(chan struct{})
			launcher := newTestLauncher(t, &sandboxFixture{
				get: func(ctx context.Context, _ string, _ *guestpb.GetProcessRequest) (*guestpb.Process, error) {
					if phase == "lookup" {
						close(calling)
						<-ctx.Done()
						return nil, ctx.Err()
					}
					return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_EXITED}, nil
				},
				suspend: func(ctx context.Context, _ string) (*apiv1.Sandbox, error) {
					assert.Equal(t, "suspend", phase)
					close(calling)
					<-ctx.Done()
					return nil, ctx.Err()
				},
			}, &workAPI{})
			trackTestProcesses(t, launcher, "worker")
			var held *sandboxWorker
			if phase == "gate" {
				var err error
				held, err = launcher.lockSandbox(ctx, "session_test")
				require.NoError(t, err)
			}
			done := make(chan struct{})
			go func() {
				launcher.collectSandbox(ctx, "session_test")
				close(done)
			}()
			if phase != "gate" {
				select {
				case <-calling:
				case <-time.After(time.Second):
					t.Fatal("collector did not reach network call")
				}
			}
			cancel()
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("collector did not honor cancellation")
			}
			if held != nil {
				launcher.unlockSandbox("session_test", held)
			}
			require.Contains(t, launcher.processes, "session_test")
			assert.Nil(t, launcher.processes["session_test"].busy)
		})
	}
}

func TestPeriodicCollectorStopsWithoutWaitingForTick(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	launcher := newTestLauncher(t, &sandboxFixture{}, &workAPI{})
	done := make(chan struct{})
	go func() {
		launcher.collectPeriodically(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("periodic collector did not stop")
	}
}

func TestCollectorDrainFinishesActiveSandboxOnly(t *testing.T) {
	for _, phase := range []string{"inspect", "suspend"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			entered, release := make(chan struct{}), make(chan struct{})
			var calls atomic.Int32
			wait := func(operationCtx context.Context) {
				close(entered)
				select {
				case <-release:
					assert.NoError(t, operationCtx.Err(), "normal drain must preserve active GC")
				case <-operationCtx.Done():
					t.Error("active GC canceled")
				}
			}
			launcher := newTestLauncher(t, &sandboxFixture{
				get: func(operationCtx context.Context, _ string, _ *guestpb.GetProcessRequest) (*guestpb.Process, error) {
					calls.Add(1)
					if phase == "inspect" {
						wait(operationCtx)
					}
					return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_EXITED}, nil
				},
				suspend: func(operationCtx context.Context, id string) (*apiv1.Sandbox, error) {
					if phase == "suspend" {
						wait(operationCtx)
					}
					sb := suspendedSandbox()
					sb.Id = id
					return sb, nil
				},
			}, &workAPI{})
			for i, sessionID := range []string{"first", "second"} {
				entry, err := launcher.lockSandbox(t.Context(), sessionID)
				require.NoError(t, err)
				entry.sandboxID = fmt.Sprintf("00000000-0000-4000-8000-%012d", i+1)
				entry.processID = "proc-" + sessionID
				entry.workID = "work-" + sessionID
				launcher.unlockSandbox(sessionID, entry)
			}
			done := make(chan struct{})
			go func() { launcher.collect(ctx); close(done) }()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("GC did not start")
			}
			cancel()
			select {
			case <-done:
				t.Fatal("GC returned before active sandbox finished")
			case <-time.After(20 * time.Millisecond):
			}
			close(release)
			select {
			case <-done:
			case <-time.After(time.Second):
				t.Fatal("GC did not drain")
			}
			assert.EqualValues(t, 1, calls.Load(), "drain must not schedule the remaining registry")
			assert.Len(t, launcher.processes, 2)
			active := 0
			for _, entry := range launcher.processes {
				if entry.workID != "" {
					active++
				}
			}
			assert.Equal(t, 1, active)
		})
	}
}
