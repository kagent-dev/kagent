package managedagents

import (
	"context"
	"maps"
	"slices"
	"time"

	guestpb "github.com/agent-substrate/env/proto/ateenv/v1alpha"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

const (
	collectionInterval = 30 * time.Second
	collectionTimeout  = 10 * time.Second
)

func (l *Launcher) collectPeriodically(ctx context.Context) {
	ticker := time.NewTicker(collectionInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			l.collect(ctx)
		}
	}
}

func (l *Launcher) trackedSessions() []string {
	l.processMu.Lock()
	defer l.processMu.Unlock()
	return slices.Collect(maps.Keys(l.processes))
}

func (l *Launcher) collect(ctx context.Context) {
	for _, sessionID := range l.trackedSessions() {
		if ctx.Err() != nil {
			return
		}
		l.collectSandbox(ctx, sessionID)
	}
}

func (l *Launcher) collectSandbox(ctx context.Context, sessionID string) {
	entry, err := l.lockSandbox(ctx, sessionID)
	if err != nil {
		return
	}
	defer l.unlockSandbox(sessionID, entry)
	if len(entry.processIDs) == 0 {
		return
	}
	sandboxID := entry.sandboxID
	if !entry.suspending {
		allExited := true
		for processID := range entry.processIDs {
			if ctx.Err() != nil {
				return
			}
			callCtx, cancel := context.WithTimeout(ctx, collectionTimeout)
			process, err := l.sandboxes.GetProcess(callCtx, sandboxID, &guestpb.GetProcessRequest{ProcessId: processID})
			cancel()
			if err != nil {
				if !processNotFound(err) {
					allExited = false
					l.log.WarnContext(ctx, "failed to inspect managed agent process", "sandbox_id", sandboxID, "process_id", processID,
						"grpc_code", status.Code(err).String(), "service_code", serviceerrors.CodeOf(err))
				}
			} else if process.GetState() != guestpb.ProcessState_PROCESS_STATE_EXITED {
				allExited = false
			}
		}
		if !allExited {
			return
		}
		entry.suspending = true
	}
	if ctx.Err() != nil {
		return
	}
	callCtx, cancel := context.WithTimeout(ctx, collectionTimeout)
	suspended, err := l.sandboxes.Suspend(callCtx, sandboxID)
	cancel()
	if err != nil {
		l.log.WarnContext(ctx, "failed to suspend idle managed agent sandbox", "sandbox_id", sandboxID,
			"grpc_code", status.Code(err).String(), "service_code", serviceerrors.CodeOf(err))
		return
	}
	if suspended.GetId() != sandboxID || suspended.GetState() != apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED ||
		suspended.GetOperation() != apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		return
	}
	entry.processIDs = nil
}

func processNotFound(err error) bool {
	if code := serviceerrors.CodeOf(err); code != "" {
		return code == serviceerrors.CodeNotFound
	}
	return status.Code(err) == codes.NotFound
}
