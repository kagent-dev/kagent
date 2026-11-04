package managedagents

import (
	"context"
	"errors"
	"strings"

	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"

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
	if instance.GetState() != apiv1.RuntimeState_RUNTIME_STATE_READY ||
		instance.GetOperation() != apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		return workerLive, &recoveryError{message: "managed agents sandbox is not ready for inspection", transient: true}
	}
	if entry.processID == "" {
		return l.discoverWorker(ctx, entry.sandboxID)
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

func (l *Launcher) finishWorker(ctx context.Context, entry *sandboxWorker) (workerState, error) {
	if entry.sandboxID == "" {
		return workerInactive, nil
	}
	state, err := l.inspectWorker(ctx, entry)
	if err != nil {
		l.log.WarnContext(ctx, "failed to inspect managed agent worker", "sandbox_id", entry.sandboxID)
		return state, err
	}
	if state == workerLive {
		return state, nil
	}
	if state == workerExited {
		callCtx, cancel := context.WithTimeout(ctx, collectionTimeout)
		suspended, err := l.sandboxes.Suspend(callCtx, entry.sandboxID)
		cancel()
		if err != nil {
			l.log.WarnContext(ctx, "failed to suspend idle managed agent sandbox", "sandbox_id", entry.sandboxID,
				"grpc_code", status.Code(err).String(), "service_code", serviceerrors.CodeOf(err))
			return state, safeRecoveryError("managed agents suspension failed", err)
		}
		if suspended.GetId() != entry.sandboxID {
			return state, errors.New("managed agents suspension sandbox identity mismatch")
		}
		if suspended.GetState() != apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED ||
			suspended.GetOperation() != apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE {
			return state, &recoveryError{message: "managed agents suspension is incomplete", transient: true}
		}
	}
	entry.workID, entry.processID = "", ""
	return state, nil
}

// Only inspect command names: ps arguments or environment can contain credentials.
func (l *Launcher) discoverWorker(ctx context.Context, sandboxID string) (workerState, error) {
	ctx, cancel := context.WithTimeout(ctx, collectionTimeout)
	defer cancel()
	process, err := l.sandboxes.StartProcess(ctx, sandboxID, &guestpb.StartProcessRequest{
		Command: []string{"ps", "-eo", "comm="},
		Timeout: durationpb.New(collectionTimeout),
	})
	if err != nil {
		return workerLive, safeRecoveryError("managed agents process discovery failed", err)
	}
	if process.GetProcessId() == "" {
		return workerLive, errors.New("managed agents process discovery returned no process ID")
	}
	var output strings.Builder
	var exit *guestpb.Process
	err = l.sandboxes.StreamProcessOutput(ctx, sandboxID, &guestpb.StreamProcessOutputRequest{
		ProcessId: process.GetProcessId(), Follow: true,
	}, func(chunk *guestpb.ProcessOutput) error {
		if output.Len()+len(chunk.GetStdout()) > 1<<20 {
			return errors.New("managed agents process listing is too large")
		}
		output.Write(chunk.GetStdout())
		if chunk.GetExit() != nil {
			exit = chunk.GetExit()
		}
		return nil
	})
	if err != nil {
		return workerLive, safeRecoveryError("managed agents process listing failed", err)
	}
	if exit == nil || exit.GetState() != guestpb.ProcessState_PROCESS_STATE_EXITED || exit.GetExitCode() != 0 {
		return workerLive, errors.New("managed agents process listing did not complete successfully")
	}
	for name := range strings.FieldsSeq(output.String()) {
		if name == "ant" {
			return workerLive, nil
		}
	}
	return workerExited, nil
}
