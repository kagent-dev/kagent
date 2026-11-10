package database

import (
	"context"
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

type sessionDeletionReason string

const (
	sessionDeletionUserRequested sessionDeletionReason = "user_requested"
	sessionDeletionIdleTimeout   sessionDeletionReason = "idle_timeout"
)

// beginSessionDeletion starts deletion or returns its existing operation.
// The caller holds the session lock and has checked any reason-specific policy,
// such as the idle-expiration deadline.
func beginSessionDeletion(ctx context.Context, tx pgx.Tx, row sessionRow, reason sessionDeletionReason, force bool) (*SessionOperation, error) {
	kind := apiv1.RuntimeOperation_RUNTIME_OPERATION_DELETE
	needed, err := runtimeOperationNeeded(row.runtimeInstanceRow, kind)
	if err != nil {
		return nil, err
	}
	if !needed {
		// Join an accepted deletion, or return its completed tombstone.
		return toSessionOperation(row)
	}
	// Preparation failure can clear the operation ID while leaving DELETING.
	// Continue that accepted deletion without requiring force again.
	if row.State != apiv1.RuntimeState_RUNTIME_STATE_DELETING.String() {
		if force {
			if err := admitForcedSessionDeletion(ctx, tx, row); err != nil {
				return nil, err
			}
		} else if err := admitAgentLifecycle(ctx, tx, row, kind); err != nil {
			return nil, err
		}
	}
	// Record deletion and revoke dispatch together so late input cannot start a turn.
	operation, err := startSessionOperation(ctx, tx, row, kind)
	if err != nil {
		return nil, err
	}
	if err := retireSessionDispatchCleanup(ctx, tx, row.ID); err != nil {
		return nil, err
	}
	if err := execSQL(ctx, tx, `UPDATE session SET deletion_reason = COALESCE(deletion_reason, $2) WHERE id = $1`, row.ID, string(reason)); err != nil {
		return nil, err
	}
	return operation, nil
}

// admitForcedSessionDeletion checks work that force must not interrupt:
// checkpoint snapshot copying and idle suspension already claimed by a worker.
// It skips turn, dispatch, and runtime-cleanup checks. These are kagent work
// records, not the Substrate actor state; RESUMING alone does not block deletion.
// The caller holds the session lock. runtimeOperationNeeded checks other
// lifecycle conflicts before this helper runs.
func admitForcedSessionDeletion(ctx context.Context, tx pgx.Tx, row sessionRow) error {
	busy, err := queryOne(ctx, tx, `
		SELECT EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = $1 AND state = 'CREATING')
		    OR EXISTS (SELECT 1 FROM session_task_event WHERE history_id = $2
		        AND quiescence_pending AND quiescence_executor_id IS NOT NULL)
		    OR EXISTS (SELECT 1 FROM session WHERE id = $1 AND dispatch_cleanup_executor_id IS NOT NULL)
	`, pgx.RowTo[bool], row.ID, row.HistoryID)
	if err != nil {
		return err
	}
	if busy {
		return fmt.Errorf("session has an active checkpoint or idle lifecycle operation: %w", ErrConflict)
	}
	return nil
}

// finishDeletedSessionTasks finalizes task records after Substrate reports
// deletion or actor absence. The caller holds the session lock and commits
// these writes with the session's DELETED state.
func finishDeletedSessionTasks(ctx context.Context, tx pgx.Tx, row sessionRow) error {
	if err := publishSavedSessionResults(ctx, tx, row); err != nil {
		return err
	}
	// Cancel only tasks still recorded as active after publishing saved results.
	// Waiting and terminal tasks keep their historical state.
	tasks, err := queryMany(ctx, tx, `
		SELECT data FROM session_task WHERE history_id = $1
		    AND state NOT IN ('TASK_STATE_COMPLETED', 'TASK_STATE_CANCELED', 'TASK_STATE_FAILED',
		        'TASK_STATE_REJECTED', 'TASK_STATE_INPUT_REQUIRED', 'TASK_STATE_AUTH_REQUIRED')
	`, pgx.RowTo[[]byte], row.HistoryID)
	if err != nil {
		return err
	}
	for _, data := range tasks {
		task, err := unmarshalSessionTask(data)
		if err != nil {
			return err
		}
		now := time.Now().UTC()
		task.Status = a2a.TaskStatus{State: a2a.TaskStateCanceled, Timestamp: &now,
			Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Session deleted while the turn was active."))}
		event := &a2a.TaskStatusUpdateEvent{TaskID: task.ID, ContextID: task.ContextID, Status: task.Status}
		if err := storeSessionTaskEvent(ctx, tx, row, task, event, false); err != nil {
			return err
		}
	}
	return retireSessionQuiescence(ctx, tx, row)
}

// finishSessionDeletion removes idle-expired sessions and their creation receipts.
// Explicit deletion keeps a tombstone so retrying the original Create request
// cannot recreate the session.
func finishSessionDeletion(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return execSQL(ctx, tx, `
		DELETE FROM runtime_instance r USING session s
		WHERE r.id = $1 AND s.id = r.id AND s.deletion_reason = 'idle_timeout'
	`, id)
}
