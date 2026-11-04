package managedagents

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
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

func TestDifferentWorkWaitsForCLIExitAndMaintainsIncomingLease(t *testing.T) {
	instance := readySandbox()
	exited, inspected, renewed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	markInspected := sync.OnceFunc(func() { close(inspected) })
	var starts atomic.Int32
	var incomingBeats int
	api := &workAPI{heartbeat: func(w http.ResponseWriter, r *http.Request, _ int) {
		if !strings.Contains(r.URL.Path, "/new/") {
			_, _ = io.WriteString(w, `{"last_heartbeat":"old-token","ttl_seconds":60,"lease_extended":true,"state":"active"}`)
			return
		}
		incomingBeats++
		if incomingBeats == 1 {
			assert.Equal(t, noHeartbeat, r.URL.Query().Get("expected_last_heartbeat"))
			_, _ = io.WriteString(w, `{"last_heartbeat":"incoming-first","ttl_seconds":2,"lease_extended":true,"state":"active"}`)
		} else {
			assert.Equal(t, "incoming-first", r.URL.Query().Get("expected_last_heartbeat"))
			_, _ = io.WriteString(w, `{"last_heartbeat":"incoming-final","ttl_seconds":60,"lease_extended":true,"state":"active"}`)
			close(renewed)
		}
	}}
	launcher := newTestLauncher(t, &sandboxFixture{
		lookup: func(context.Context, string) (*apiv1.Sandbox, error) { return instance, nil },
		get: func(_ context.Context, _ string, req *guestpb.GetProcessRequest) (*guestpb.Process, error) {
			assert.Equal(t, "proc-old", req.ProcessId)
			markInspected()
			select {
			case <-exited:
				return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_EXITED}, nil
			default:
				return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
			}
		},
		suspend: func(context.Context, string) (*apiv1.Sandbox, error) {
			select {
			case <-exited:
			default:
				t.Error("must not suspend a live CLI")
			}
			instance.State = apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED
			return instance, nil
		},
		resume: func(context.Context, string) (*apiv1.Sandbox, error) {
			instance.State = apiv1.RuntimeState_RUNTIME_STATE_READY
			return instance, nil
		},
		start: func(_ context.Context, _ string, req *guestpb.StartProcessRequest) (*guestpb.Process, error) {
			starts.Add(1)
			if req.Env["ANTHROPIC_WORK_ID"] == "new" {
				select {
				case <-exited:
				default:
					t.Error("new CLI overlapped its predecessor")
				}
				assert.Equal(t, "incoming-final", req.Env["ANTHROPIC_WORK_LAST_HEARTBEAT"])
				assert.NotContains(t, api.metadata["old"], processIDKey, "old metadata cleanup must precede handoff")
			}
			return &guestpb.Process{ProcessId: "proc-" + req.Env["ANTHROPIC_WORK_ID"], State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
		},
	}, api)
	require.NoError(t, launcher.launch(t.Context(), testWork("old")))
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- launcher.launch(ctx, testWork("new")) }()
	for _, signal := range []<-chan struct{}{inspected, renewed} {
		select {
		case <-signal:
		case <-ctx.Done():
			t.Fatal("incoming work did not wait with a maintained lease")
		}
	}
	assert.EqualValues(t, 1, starts.Load())
	api.mu.Lock()
	assert.Equal(t, "proc-old", api.metadata["old"][processIDKey])
	assert.Zero(t, api.stops)
	api.mu.Unlock()
	close(exited)
	require.NoError(t, <-done)
	assert.EqualValues(t, 2, starts.Load())
	assert.Equal(t, "new", launcher.processes["session_test"].workID)
	assert.Equal(t, "proc-new", launcher.processes["session_test"].processID)
	assert.Equal(t, testSandboxID, api.metadata["old"][sandboxIDKey])
	assert.NotContains(t, api.metadata["old"], processIDKey)
	assert.Zero(t, api.stops)
}

func TestBlockedHandoffLeavesIncomingLeaseToExpire(t *testing.T) {
	for _, tt := range []struct {
		name    string
		process *guestpb.Process
		err     error
	}{
		{name: "running", process: &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_RUNNING}},
		{name: "unknown", process: &guestpb.Process{}},
		{name: "empty response"},
		{name: "transient", err: serviceerrors.NewUnavailable("secret-body", nil)},
		{name: "permanent", err: status.Error(codes.PermissionDenied, "secret-body")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			starts := 0
			api := &workAPI{}
			launcher := newTestLauncher(t, &sandboxFixture{
				get: func(context.Context, string, *guestpb.GetProcessRequest) (*guestpb.Process, error) {
					return tt.process, tt.err
				},
				suspend: func(context.Context, string) (*apiv1.Sandbox, error) {
					t.Error("uncertain or live CLI must not be suspended")
					return nil, errors.New("unexpected suspension")
				},
				start: func(context.Context, string, *guestpb.StartProcessRequest) (*guestpb.Process, error) {
					starts++
					return &guestpb.Process{ProcessId: "old-process", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
				},
			}, api)
			require.NoError(t, launcher.launch(t.Context(), testWork("old")))
			ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
			defer cancel()
			err := launcher.launch(ctx, testWork("incoming"))
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "secret-body")
			assert.Equal(t, 1, starts)
			assert.Equal(t, "old", launcher.processes["session_test"].workID)
			assert.Equal(t, "old-process", launcher.processes["session_test"].processID)
			assert.Equal(t, "old-process", api.metadata["old"][processIDKey])
			assert.NotContains(t, api.metadata["incoming"], sandboxIDKey)
			assert.Zero(t, api.stops, "neither healthy old work nor timed-out incoming work may be stopped")
		})
	}
}

func TestFailedCleanupBlocksHandoffWithoutOverwritingOldMetadata(t *testing.T) {
	for _, phase := range []string{"suspend", "metadata"} {
		t.Run(phase, func(t *testing.T) {
			instance := readySandbox()
			fail := true
			starts, gets := 0, 0
			api := &workAPI{update: func(w http.ResponseWriter, _ *http.Request, values map[string]*string) {
				if _, cleanup := values[processIDKey]; cleanup && values[processIDKey] == nil && values[sandboxIDKey] == nil && phase == "metadata" && fail {
					w.WriteHeader(http.StatusServiceUnavailable)
					return
				}
				_, _ = io.WriteString(w, `{}`)
			}}
			launcher := newTestLauncher(t, &sandboxFixture{
				lookup: func(context.Context, string) (*apiv1.Sandbox, error) { return instance, nil },
				get: func(context.Context, string, *guestpb.GetProcessRequest) (*guestpb.Process, error) {
					gets++
					return &guestpb.Process{State: guestpb.ProcessState_PROCESS_STATE_EXITED}, nil
				},
				suspend: func(context.Context, string) (*apiv1.Sandbox, error) {
					if phase == "suspend" && fail {
						instance.Operation = apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND
						return nil, serviceerrors.NewUnavailable("secret-body", nil)
					}
					instance.State = apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED
					instance.Operation = apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE
					return instance, nil
				},
				resume: func(context.Context, string) (*apiv1.Sandbox, error) {
					instance.State = apiv1.RuntimeState_RUNTIME_STATE_READY
					return instance, nil
				},
				start: func(_ context.Context, _ string, req *guestpb.StartProcessRequest) (*guestpb.Process, error) {
					starts++
					if starts > 1 {
						assert.NotContains(t, api.metadata["old"], processIDKey)
					}
					return &guestpb.Process{ProcessId: req.Env["ANTHROPIC_WORK_ID"], State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
				},
			}, api)
			require.NoError(t, launcher.launch(t.Context(), testWork("old")))
			ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
			defer cancel()
			require.Error(t, launcher.launch(ctx, testWork("blocked")))
			entry := launcher.processes["session_test"]
			if phase == "suspend" {
				require.NotNil(t, entry)
				assert.Equal(t, "old", entry.workID)
				assert.Equal(t, "old", entry.processID)
			} else {
				requireIdleSandbox(t, launcher, "session_test")
				retryCtx, retryCancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
				defer retryCancel()
				require.Error(t, launcher.launch(retryCtx, testWork("still_blocked")))
				assert.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED, instance.State)
				assert.NotContains(t, api.metadata["still_blocked"], sandboxIDKey)
			}
			assert.Equal(t, "old", api.metadata["old"][processIDKey])
			assert.Equal(t, 1, starts)
			assert.Zero(t, api.stops)
			fail = false
			require.NoError(t, launcher.launch(t.Context(), testWork("next")))
			assert.Equal(t, 2, starts)
			assert.Equal(t, 1, gets, "pending cleanup must not inspect the old guest again")
			assert.NotContains(t, api.metadata["old"], processIDKey)
			assert.Equal(t, "next", launcher.processes["session_test"].workID)
		})
	}
}
