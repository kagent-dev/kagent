package managedagents

import (
	"context"
	"errors"

	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

type workerState int

const (
	workerLive workerState = iota
	workerExited
	workerInactive
)

func (l *Launcher) inspectWorker(ctx context.Context, entry *sandboxWorker) (workerState, error) {
	callCtx, cancel := context.WithTimeout(ctx, collectionTimeout)
	instance, err := l.sandboxes.Get(callCtx, entry.sandboxID)
	cancel()
	if processNotFound(err) {
		return workerInactive, nil
	}
	if err != nil {
		return workerLive, safeRecoveryError("managed agents sandbox inspection failed", err)
	}
	if err := l.validateSavedSandbox(instance, entry.sandboxID); err != nil {
		return workerLive, err
	}
	if sandboxExpired(instance) || instance.GetState() == apiv1.RuntimeState_RUNTIME_STATE_DELETED ||
		instance.GetState() == apiv1.RuntimeState_RUNTIME_STATE_DELETING ||
		instance.GetOperation() == apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE ||
		(instance.GetState() == apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED && instance.GetOperation() == apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE) {
		return workerInactive, nil
	}
	if instance.GetOperation() == apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND {
		return workerExited, nil
	}
	callCtx, cancel = context.WithTimeout(ctx, collectionTimeout)
	process, err := l.sandboxes.GetProcess(callCtx, entry.sandboxID, &guestpb.GetProcessRequest{ProcessId: entry.processID})
	cancel()
	if processNotFound(err) {
		return workerExited, nil
	}
	if err != nil {
		return workerLive, safeRecoveryError("managed agents process inspection failed", err)
	}
	if process.GetState() == guestpb.ProcessState_PROCESS_STATE_EXITED {
		return workerExited, nil
	}
	return workerLive, nil
}

func (l *Launcher) finishWorker(ctx context.Context, entry *sandboxWorker) error {
	if entry.workID == "" {
		return nil
	}
	state, err := l.inspectWorker(ctx, entry)
	if err != nil {
		l.log.WarnContext(ctx, "failed to inspect managed agent worker", "sandbox_id", entry.sandboxID)
		return err
	}
	if state == workerLive {
		return nil
	}
	if state == workerExited {
		callCtx, cancel := context.WithTimeout(ctx, collectionTimeout)
		suspended, err := l.sandboxes.Suspend(callCtx, entry.sandboxID)
		cancel()
		if err != nil {
			l.log.WarnContext(ctx, "failed to suspend idle managed agent sandbox", "sandbox_id", entry.sandboxID,
				"grpc_code", status.Code(err).String(), "service_code", serviceerrors.CodeOf(err))
			return safeRecoveryError("managed agents suspension failed", err)
		}
		if suspended.GetId() != entry.sandboxID {
			return errors.New("managed agents suspension sandbox identity mismatch")
		}
		if suspended.GetState() != apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED ||
			suspended.GetOperation() != apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE {
			return &recoveryError{message: "managed agents suspension is incomplete", transient: true}
		}
	}
	workID := entry.workID
	entry.workID, entry.processID = "", ""
	if err := l.retryMetadata(ctx, workID, map[string]*string{processIDKey: nil}); err != nil {
		entry.staleWorkIDs = append(entry.staleWorkIDs, workID)
		l.log.WarnContext(ctx, "inactive managed agent worker has stale metadata", "work_id", workID, "sandbox_id", entry.sandboxID)
		return err
	}
	return nil
}
