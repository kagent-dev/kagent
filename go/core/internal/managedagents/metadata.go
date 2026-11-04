package managedagents

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/cenkalti/backoff/v5"
	"github.com/google/uuid"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

const (
	managedByKey   = "kagent.dev/managed-by"
	managedByValue = "kagent"
	sandboxIDKey   = "kagent.dev/sandbox-id"
	processIDKey   = "kagent.dev/process-id"
)

// Recovery errors retain only a retry classification, never upstream bodies.
type recoveryError struct {
	message   string
	transient bool
}

func (e *recoveryError) Error() string { return e.message }

func recoveryRetryable(err error) bool {
	var failure *recoveryError
	return errors.As(err, &failure) && failure.transient
}

func safeRecoveryError(message string, err error) error {
	code := upstreamStatus(err)
	var networkError net.Error
	return &recoveryError{message: message, transient: recoveryRetryable(err) || retryable(err) ||
		(serviceerrors.CodeOf(err) == "" && (errors.As(err, &networkError) || code == 408 || code == 409 || code == 429 || code >= 500))}
}

func (l *Launcher) updateMetadata(ctx context.Context, workID string, values map[string]*string) error {
	_, err := l.anthropic.Beta.Environments.Work.Update(ctx, workID, anthropic.BetaEnvironmentWorkUpdateParams{
		EnvironmentID: l.config.EnvironmentID,
	}, option.WithJSONSet("metadata", values))
	if err != nil {
		return safeRecoveryError(fmt.Sprintf("managed agents metadata update failed (HTTP status %d)", upstreamStatus(err)), err)
	}
	return nil
}

func (l *Launcher) retryWorkerMetadata(ctx context.Context, entry *sandboxWorker) error {
	return l.retryMetadata(ctx, entry.workID, map[string]*string{
		managedByKey: new(managedByValue),
		sandboxIDKey: new(entry.sandboxID),
		processIDKey: new(entry.processID),
	})
}

func (l *Launcher) retryMetadata(ctx context.Context, workID string, values map[string]*string) error {
	ctx, cancel := context.WithTimeout(ctx, collectionTimeout)
	defer cancel()
	_, err := backoff.Retry(ctx, func() (struct{}, error) {
		err := l.updateMetadata(ctx, workID, values)
		if err != nil && !recoveryRetryable(err) {
			return struct{}{}, backoff.Permanent(err)
		}
		return struct{}{}, err
	}, backoff.WithBackOff(backoff.NewConstantBackOff(100*time.Millisecond)), backoff.WithMaxElapsedTime(0))
	if err != nil {
		return safeRecoveryError("managed agents worker metadata was not persisted", err)
	}
	return nil
}

func (l *Launcher) recoverWorkers(ctx context.Context) error {
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		attemptCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), provisionTimeout+2*collectionTimeout)
		err := l.adopt(attemptCtx)
		cancel()
		if err == nil || !recoveryRetryable(err) {
			return err
		}
		timer := time.NewTimer(250 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}

// Restore every page before polling or collection can touch any runtime.
func (l *Launcher) adopt(ctx context.Context) error {
	restored := make(map[string]*sandboxWorker)
	sandboxSessions := make(map[string]string)
	instances := make(map[string]*apiv1.Sandbox)
	selected := make(map[string]*apiv1.Sandbox)
	records := make(map[string]anthropic.BetaSelfHostedWork)
	pager := l.anthropic.Beta.Environments.Work.ListAutoPaging(ctx, l.config.EnvironmentID, anthropic.BetaEnvironmentWorkListParams{})
	for pager.Next() {
		work := pager.Current()
		record, exists := records[work.ID]
		if exists && (record.Data.ID != work.Data.ID || record.Data.Type != work.Data.Type ||
			record.EnvironmentID != work.EnvironmentID ||
			record.Metadata[managedByKey] != work.Metadata[managedByKey] ||
			record.Metadata[sandboxIDKey] != work.Metadata[sandboxIDKey] ||
			record.Metadata[processIDKey] != work.Metadata[processIDKey]) {
			l.log.WarnContext(ctx, "conflicting managed agents adoption work identities; using last observed record", "work_id", work.ID)
		}
		records[work.ID] = work
	}
	if err := pager.Err(); err != nil {
		return safeRecoveryError(fmt.Sprintf("managed agents adoption list failed (HTTP status %d)", upstreamStatus(err)), err)
	}
	for workID, work := range records {
		if work.Metadata[managedByKey] != managedByValue {
			delete(records, workID)
			continue
		}
		processID, hasProcess := work.Metadata[processIDKey]
		sandboxID, hasSandbox := work.Metadata[sandboxIDKey]
		if !hasSandbox && !hasProcess {
			delete(records, workID)
			continue
		}
		if uuid.Validate(sandboxID) != nil || (hasProcess && strings.TrimSpace(processID) == "") || work.EnvironmentID != l.config.EnvironmentID || work.ID == "" ||
			work.Data.ID == "" || work.Data.Type != "session" {
			l.log.WarnContext(ctx, "ignoring invalid managed agents adoption record", "work_id", work.ID)
			delete(records, workID)
			continue
		}
		if sessionID, exists := sandboxSessions[sandboxID]; exists && sessionID != work.Data.ID {
			l.log.WarnContext(ctx, "ignoring conflicting managed agents adoption sessions", "work_id", work.ID, "sandbox_id", sandboxID)
			delete(records, workID)
			continue
		}
		callCtx, cancel := context.WithTimeout(ctx, collectionTimeout)
		instance, err := l.sandboxes.Get(callCtx, sandboxID)
		cancel()
		if err != nil && !processNotFound(err) {
			l.log.WarnContext(ctx, "ignoring managed agents adoption record after sandbox lookup failure",
				"work_id", work.ID, "sandbox_id", sandboxID, "service_code", serviceerrors.CodeOf(err))
			delete(records, workID)
			continue
		}
		if err == nil {
			if err := l.validateSavedSandbox(instance, sandboxID); err != nil {
				l.log.WarnContext(ctx, "ignoring invalid managed agents adoption sandbox", "work_id", work.ID,
					"sandbox_id", sandboxID, "error", err)
				delete(records, workID)
				continue
			}
			latest, err := newerSandbox(selected[work.Data.ID], instance)
			if err != nil {
				l.log.WarnContext(ctx, "ignoring conflicting managed agents adoption sandbox generations",
					"work_id", work.ID, "sandbox_id", sandboxID, "error", err)
				delete(records, workID)
				continue
			}
			selected[work.Data.ID] = latest
		} else {
			instance = nil
		}
		instances[sandboxID] = instance
		sandboxSessions[sandboxID] = work.Data.ID
	}
	for sessionID, instance := range selected {
		restored[sessionID] = &sandboxWorker{sandboxID: instance.GetId()}
	}
	for _, work := range records {
		processID, hasProcess := work.Metadata[processIDKey]
		if !hasProcess {
			continue
		}
		sandboxID := work.Metadata[sandboxIDKey]
		instance := instances[sandboxID]
		entry := restored[work.Data.ID]
		if instance == nil || sandboxExpired(instance) ||
			instance.GetState() == apiv1.RuntimeState_RUNTIME_STATE_DELETED ||
			instance.GetState() == apiv1.RuntimeState_RUNTIME_STATE_DELETING ||
			instance.GetOperation() == apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE ||
			(instance.GetState() == apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED && instance.GetOperation() == apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE) {
			if err := l.updateMetadata(ctx, work.ID, map[string]*string{processIDKey: nil}); err != nil {
				l.log.WarnContext(ctx, "failed to clear stale managed agents adoption metadata", "work_id", work.ID, "error", err)
				if entry != nil && entry.sandboxID == sandboxID {
					entry.staleWorkIDs = append(entry.staleWorkIDs, work.ID)
				}
			}
			continue
		}
		suspending := instance.GetOperation() == apiv1.RuntimeOperation_RUNTIME_OPERATION_SUSPEND
		if !suspending && (instance.GetState() != apiv1.RuntimeState_RUNTIME_STATE_READY ||
			instance.GetOperation() != apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE) {
			l.log.WarnContext(ctx, "ignoring managed agents adoption sandbox that is not ready", "work_id", work.ID, "sandbox_id", sandboxID)
			continue
		}
		if entry.sandboxID != sandboxID {
			l.log.WarnContext(ctx, "ignoring conflicting managed agents adoption sandbox generations", "work_id", work.ID, "sandbox_id", sandboxID)
			continue
		}
		if entry.workID != "" && (entry.workID != work.ID || entry.processID != processID) {
			l.log.WarnContext(ctx, "ignoring conflicting managed agents adoption sandbox workers", "work_id", work.ID, "sandbox_id", sandboxID)
			continue
		}
		entry.workID, entry.processID = work.ID, processID
	}
	l.processMu.Lock()
	l.processes = restored
	l.processMu.Unlock()
	for _, sessionID := range l.trackedSessions() {
		if err := l.collectSandbox(ctx, sessionID); err != nil {
			l.log.WarnContext(ctx, "failed to collect managed agents adoption sandbox", "session_id", sessionID, "error", err)
		}
	}
	return nil
}
