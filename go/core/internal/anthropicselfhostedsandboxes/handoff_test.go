package anthropicselfhostedsandboxes

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
		scan: func(context.Context, string) (string, error) {
			markInspected()
			select {
			case <-exited:
				return "", nil
			default:
				return "ant\n", nil
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
	assert.Zero(t, api.stops)
	api.mu.Unlock()
	close(exited)
	require.NoError(t, <-done)
	assert.EqualValues(t, 2, starts.Load())
	assert.Equal(t, "new", launcher.processes["session_test"].workID)
	assert.Zero(t, api.stops)
}

func TestBlockedHandoffLeavesIncomingLeaseToExpire(t *testing.T) {
	for _, tt := range []struct {
		name   string
		output string
		err    error
	}{
		{name: "running", output: "ant\n"},
		{name: "transient", err: serviceerrors.NewUnavailable("secret-body", nil)},
		{name: "permanent", err: status.Error(codes.PermissionDenied, "secret-body")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			starts := 0
			api := &workAPI{}
			launcher := newTestLauncher(t, &sandboxFixture{
				scan: func(context.Context, string) (string, error) {
					return tt.output, tt.err
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
			assert.Zero(t, api.stops, "neither healthy old work nor timed-out incoming work may be stopped")
		})
	}
}

func TestHandoffCompletesPendingSuspensionBeforeResume(t *testing.T) {
	for _, initiator := range []string{"collector", "incoming assignment"} {
		t.Run(initiator, func(t *testing.T) {
			instance := readySandbox()
			fail, live := true, true
			scans := 0
			var transitions []string
			api := &workAPI{}
			launcher := newTestLauncher(t, &sandboxFixture{
				lookup: func(context.Context, string) (*apiv1.Sandbox, error) { return instance, nil },
				create: func(context.Context, *apiv1.CreateSandboxRequest) (*apiv1.Sandbox, error) {
					transitions = append(transitions, "create")
					return instance, nil
				},
				scan: func(context.Context, string) (string, error) {
					require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, instance.State)
					require.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE, instance.Operation)
					scans++
					if live {
						return "ant\n", nil
					}
					return "", nil
				},
				suspend: func(context.Context, string) (*apiv1.Sandbox, error) {
					require.False(t, live, "never suspend a running worker")
					instance.State = apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED
					if fail {
						instance.Operation = apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND
						return nil, serviceerrors.NewUnavailable("secret-body", nil)
					}
					transitions = append(transitions, "suspend")
					instance.Operation = apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE
					return instance, nil
				},
				resume: func(context.Context, string) (*apiv1.Sandbox, error) {
					require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED, instance.State)
					require.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE, instance.Operation)
					transitions = append(transitions, "resume")
					instance.State = apiv1.RuntimeState_RUNTIME_STATE_READY
					return instance, nil
				},
				start: func(_ context.Context, _ string, req *guestpb.StartProcessRequest) (*guestpb.Process, error) {
					require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, instance.State)
					require.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE, instance.Operation)
					transitions = append(transitions, "start "+req.Env["ANTHROPIC_WORK_ID"])
					live = true
					return &guestpb.Process{ProcessId: "worker", State: guestpb.ProcessState_PROCESS_STATE_RUNNING}, nil
				},
			}, api)
			require.NoError(t, launcher.launch(t.Context(), testWork("old")))
			live = false
			if initiator == "collector" {
				require.Error(t, launcher.collectSandbox(t.Context(), "session_test"))
			} else {
				ctx, cancel := context.WithTimeout(t.Context(), 150*time.Millisecond)
				defer cancel()
				require.Error(t, launcher.launch(ctx, testWork("blocked")))
			}
			assert.Equal(t, apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND, instance.Operation)
			assert.Equal(t, "old", launcher.processes["session_test"].workID)
			assert.Equal(t, []string{"create", "start old"}, transitions)
			assert.Zero(t, api.stops)

			fail = false
			require.NoError(t, launcher.launch(t.Context(), testWork("next")))
			assert.Equal(t, []string{"create", "start old", "suspend", "resume", "start next"}, transitions)
			assert.Equal(t, 1, scans, "pending cleanup must not inspect the old guest again")
			assert.Equal(t, "next", launcher.processes["session_test"].workID)
			assert.Equal(t, testSandboxID, launcher.processes["session_test"].sandboxID)
			require.NoError(t, launcher.collectSandbox(t.Context(), "session_test"))
			assert.Equal(t, 2, scans)
			assert.Equal(t, "next", launcher.processes["session_test"].workID)
			assert.Len(t, transitions, 5, "collection must leave the replacement worker running")
			assert.Zero(t, api.stops)
		})
	}
}
