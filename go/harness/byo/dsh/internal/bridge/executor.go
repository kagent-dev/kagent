package bridge

import (
	"context"
	"iter"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	runtimea2a "github.com/kagent-dev/kagent/go/harness/runtime/a2a"
	"github.com/kagent-dev/kagent/go/pkg/tracing"
)

// Executor adds DSH-specific failure handling to the shared executor. It cannot
// infer process cleanup from missing in-memory state or reconstruct ACP promises.
type Executor struct {
	a2asrv.PassthroughCallInterceptor
	*runtimea2a.Executor
}

var _ a2asrv.AgentExecutor = (*Executor)(nil)
var _ a2asrv.CallInterceptor = (*Executor)(nil)

// NewExecutor constructs the DSH executor. Register its call interceptor too,
// so lost continuations are rejected before the SDK persists their messages.
func NewExecutor(runner *Runner, store runtimea2a.ContinuationStore, telemetry tracing.RuntimeTelemetry) (*Executor, error) {
	executor, err := runtimea2a.New(runner, store, telemetry)
	if err != nil {
		return nil, err
	}
	return &Executor{Executor: executor}, nil
}

// Before admits task continuations only for the live parked turn. In this
// single-turn adapter, active work cannot accept another send and completed work
// continues as a new A2A task. Reject at the call boundary: even the SDK's history
// append can settle a stored input-required task before Executor.Execute runs.
// TODO: replace this rejection with exact pending-call recovery once
// https://github.com/kagent-dev/kagent/pull/3165 and DSH support that contract.
func (e *Executor) Before(ctx context.Context, _ *a2asrv.CallContext, request *a2asrv.Request) (context.Context, any, error) {
	if send, ok := request.Payload.(*a2atype.SendMessageRequest); ok && send.Message != nil {
		message := send.Message
		payload, _ := message.Metadata[apia2a.HITLExtensionURI].(map[string]any)
		approval := payload["type"] == apia2a.HITLTypeToolApprovalResponse
		if (message.TaskID != "" || approval) && (message.TaskID == "" || e.ReservedTaskID() != message.TaskID) {
			return ctx, nil, a2atype.NewError(a2atype.ErrUnsupportedOperation, "DSH task has no live continuation owner; the request was not applied and no tool was replayed")
		}
	}
	return ctx, nil, nil
}

// Cancel must return a protocol error when the shared executor has no owner.
// An empty sequence leaves the A2A caller waiting indefinitely. Do not fabricate
// CANCELED: an absent owner is not proof that its tools have stopped.
// TODO: adopt fenced task recovery from
// https://github.com/kagent-dev/kagent/issues/3010 when it becomes available.
func (e *Executor) Cancel(ctx context.Context, input *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		emitted := false
		for event, err := range e.Executor.Cancel(ctx, input) {
			emitted = true
			if !yield(event, err) {
				return
			}
		}
		if !emitted {
			if err := ctx.Err(); err != nil {
				yield(nil, err)
				return
			}
			yield(nil, a2atype.NewError(a2atype.ErrTaskNotCancelable, "DSH task has no live execution owner; cleanup must be confirmed before task recovery"))
		}
	}
}
