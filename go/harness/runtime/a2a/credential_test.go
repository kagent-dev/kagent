package a2a

import (
	"context"
	"sync"
	"testing"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/stretchr/testify/require"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	"github.com/kagent-dev/kagent/go/harness/runtime"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

// bindingRunner records what the forwarder would hold: the bound credential,
// or the empty string once it is released.
type bindingRunner struct {
	fakeRunner
	mu    sync.Mutex
	bound string
}

func (r *bindingRunner) BindCallerCredential(authorization string) func() {
	r.set(authorization)
	return func() { r.set("") }
}

func (r *bindingRunner) set(credential string) {
	r.mu.Lock()
	r.bound = credential
	r.mu.Unlock()
}

func (r *bindingRunner) current() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.bound
}

func callContext(t *testing.T, authorization string) context.Context {
	t.Helper()
	ctx, _ := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(map[string][]string{"authorization": {authorization}}))
	return ctx
}

func TestExecuteBindsTheCallerCredentialForRunAndResume(t *testing.T) {
	runner := &bindingRunner{}
	var duringRun, duringResume string
	runner.run = func(context.Context, runtime.Turn, runtime.EventSink) (runtime.Outcome, error) {
		duringRun = runner.current()
		return runtime.Outcome{Pending: &fakePendingTurn{
			request: &runtime.ApprovalRequest{ID: "7", CallID: "call-1", Name: "tools.write", Hint: "Allow tools.write?"},
			resume: func(context.Context, runtime.InputResponse, runtime.EventSink) (runtime.Outcome, error) {
				duringResume = runner.current()
				return runtime.Outcome{}, nil
			},
		}}, nil
	}
	executor, err := New(runner, &fakeContinuation{}, tracing.RuntimeTelemetry{})
	require.NoError(t, err)

	first := requestContext("task-1", "write")
	var boundAtPark string
	for event, err := range executor.Execute(callContext(t, "Bearer first-sender"), first) {
		require.NoError(t, err)
		if update, ok := event.(*a2atype.TaskStatusUpdateEvent); ok && update.Status.State == a2atype.TaskStateInputRequired {
			boundAtPark = runner.current()
			first.StoredTask = &a2atype.Task{ID: first.TaskID, ContextID: first.ContextID, Status: update.Status}
		}
	}
	require.Equal(t, "Bearer first-sender", duringRun)
	require.NotNil(t, first.StoredTask, "the turn did not park")
	require.Empty(t, boundAtPark, "a parked turn must hold no credential")

	decision := a2atype.NewMessage(a2atype.MessageRoleUser)
	decision.TaskID, decision.ContextID = first.TaskID, first.ContextID
	require.NoError(t, apia2a.AttachHITL(decision, apia2a.ToolApprovalResponse{
		Type: apia2a.HITLTypeToolApprovalResponse, Approvals: []apia2a.ToolApproval{{ID: "7", Approved: true}},
	}))
	second := &a2asrv.ExecutorContext{TaskID: first.TaskID, ContextID: first.ContextID, Message: decision, StoredTask: first.StoredTask}
	_, errs := collect(executor.Execute(callContext(t, "Bearer second-sender"), second))
	require.Empty(t, errs)
	require.Equal(t, "Bearer second-sender", duringResume, "a resumed turn runs as the request that resumed it")
	require.Empty(t, runner.current(), "a finished turn must hold no credential")
}

func TestCallerAuthorizationReadsTheA2ACall(t *testing.T) {
	require.Empty(t, callerAuthorization(context.Background()))
	ctx, _ := a2asrv.NewCallContext(t.Context(), a2asrv.NewServiceParams(map[string][]string{"x-user-id": {"someone"}}))
	require.Empty(t, callerAuthorization(ctx))
	require.Equal(t, "Bearer person", callerAuthorization(callContext(t, " Bearer person ")))
}
