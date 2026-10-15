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
	runner *Runner
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
	return &Executor{Executor: executor, runner: runner}, nil
}

// The shared executor generates/validates these identities before Runner.Run.
// Keep them out of model-controlled tool arguments and persisted DSH profiles.
func (e *Executor) Execute(ctx context.Context, req *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	if req != nil {
		ctx = context.WithValue(ctx, approvalOwnerKey{}, approvalOwner{TaskID: string(req.TaskID), ContextID: req.ContextID})
	}
	return e.Executor.Execute(ctx, req)
}

// #3165 makes admission restore durable ownership and return read errors.
// Keep this BYO adapter buildable against main while consuming that contract
// when available; do not copy the shared executor's recovery implementation.
func (e *Executor) reservedTask() (a2atype.TaskID, error) {
	switch executor := any(e.Executor).(type) {
	case interface {
		ReservedTaskID() (a2atype.TaskID, error)
	}:
		return executor.ReservedTaskID()
	case interface{ ReservedTaskID() a2atype.TaskID }:
		return executor.ReservedTaskID(), nil
	default:
		return "", a2atype.ErrUnsupportedOperation
	}
}

// Before admits task continuations only for an owned parked turn. In this
// single-turn adapter, active work cannot accept another send and completed work
// continues as a new A2A task. Reject at the call boundary: even the SDK's history
// append can settle a stored input-required task before Executor.Execute runs.
// Protected MCP calls use Runner.RestorePending once the shared contract in
// https://github.com/kagent-dev/kagent/pull/3165 lands. Keep this guard for native
// ACP permissions: DSH 0.2.0-rc.2 cannot reconstruct their execution point from
// a session ID. Never replay a native call with an uncommitted result.
func (e *Executor) Before(ctx context.Context, _ *a2asrv.CallContext, request *a2asrv.Request) (context.Context, any, error) {
	if send, ok := request.Payload.(*a2atype.SendMessageRequest); ok && send.Message != nil {
		if err := e.runner.checkApprovalContext(send.Message.ContextID); err != nil {
			return ctx, nil, a2atype.NewError(a2atype.ErrUnsupportedOperation, err.Error())
		}
		reserved, err := e.reservedTask()
		if err != nil {
			return ctx, nil, err
		}
		message := send.Message
		payload, _ := message.Metadata[apia2a.HITLExtensionURI].(map[string]any)
		approval := payload["type"] == apia2a.HITLTypeToolApprovalResponse
		if (message.TaskID != "" || approval) && (message.TaskID == "" || reserved != message.TaskID) {
			return ctx, nil, a2atype.NewError(a2atype.ErrUnsupportedOperation, "DSH task has no live continuation owner; the request was not applied and no tool was replayed")
		}
		if approval {
			saved, err := e.runner.loadApproval()
			if err != nil {
				return ctx, nil, err
			}
			if saved != nil && string(message.TaskID) == saved.Owner.TaskID && (saved.Phase == "waiting" || saved.Phase == "result") {
				response, err := apia2a.ParseToolApprovalResponse(message)
				if err != nil || response == nil || len(response.Approvals) != 1 || response.Approvals[0].ID != saved.Request.ID {
					return ctx, nil, a2atype.NewError(a2atype.ErrInvalidParams, "approval decision does not match saved DSH call")
				}
				decision := response.Approvals[0]
				if saved.Decision != nil && (saved.Decision.Approved != decision.Approved || saved.Decision.RejectionReason != decision.RejectionReason) {
					return ctx, nil, a2atype.NewError(a2atype.ErrInvalidParams, "approval decision changed after execution")
				}
			}
		}
	}
	return ctx, nil, nil
}

// Cancel must return a protocol error when the shared executor has no owner.
// An empty sequence leaves the A2A caller waiting indefinitely. Do not fabricate
// CANCELED: an absent owner is not proof that its tools have stopped.
// TODO: adopt fenced task recovery from
// https://github.com/kagent-dev/kagent/issues/3010 when it becomes available.
// https://github.com/kagent-dev/kagent/pull/3011 addresses the empty cancel
// iterator, but its missing-owner CANCELED result does not establish cleanup of
// DSH descendants. Remove this guard only when the shared path confirms that
// cleanup, including an unreachable/replaced Actor and unpublished boundaries.
// Go ADK also reproduces #3010 case B through the shared TaskStore; this is not
// a DSH-only failure. Allowing Session deletion only provides an escape hatch:
// it does not recover a pending ACP call or make missing-owner cancellation safe.
func (e *Executor) Cancel(ctx context.Context, input *a2asrv.ExecutorContext) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		emitted := false
		for event, err := range e.Executor.Cancel(ctx, input) {
			emitted = true
			if update, ok := event.(*a2atype.TaskStatusUpdateEvent); ok && update.Status.State == a2atype.TaskStateCanceled {
				saved, readErr := e.runner.loadApproval()
				if readErr != nil || saved != nil && (saved.Phase == "executing" || saved.Phase == "continuing") {
					// A remote MCP effect can outlive HTTP cancellation. Until the
					// shared cleanup/fencing contract in #3010 can certify it, do not
					// publish a successful cancel for an uncertain external operation.
					yield(nil, a2atype.NewError(a2atype.ErrTaskNotCancelable, "DSH approval outcome is uncertain; cancellation cannot certify remote cleanup"))
					return
				}
			}
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
