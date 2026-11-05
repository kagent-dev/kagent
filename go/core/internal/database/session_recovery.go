package database

import (
	"context"
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// SessionRuntimeCrashed identifies a recovery admitted after Substrate confirmed
// that the recorded Actor crashed. Transport failures cannot admit this work.
const SessionRuntimeCrashed = "RuntimeCrashed"

// SessionRuntimeUnrecoverable identifies a Session whose admitted crash
// recovery was abandoned because its recorded Actor is gone, was replaced, or no
// longer holds the admitted snapshot. No retry restores it; Delete still can.
const SessionRuntimeUnrecoverable = "RuntimeUnrecoverable"

// CrashRecoveryAdmitted reports a Session whose crash recovery was admitted and
// has not finished. A retry joins that operation instead of observing Substrate
// again. Only BeginCrashedSessionRecovery starts a REVERT, so the operation alone
// identifies it.
func CrashRecoveryAdmitted(session *apiv1.Session) bool {
	return session.Operation == apiv1.RuntimeOperation_RUNTIME_OPERATION_REVERT
}

// SessionGeneration is a live Session's lifecycle generation and the runtime it
// recorded, read together before crash recovery observes Substrate. OperationID
// is the Session's latest lifecycle operation, or uuid.Nil when none is recorded:
// every operation replaces it, and completion keeps it. ActorUID is the recorded
// runtime Actor, or "" when none is recorded. The template identifies where that
// Actor lives and what it must run.
type SessionGeneration struct {
	SessionID        string
	OperationID      uuid.UUID
	ActorUID         string
	TemplateAtespace string
	TemplateName     string
}

// GetSessionGeneration reads the lifecycle generation, recorded Actor UID, and
// Actor template of a live Session in one statement. Deleted or missing Sessions
// return ErrNotFound. Callers authorize access. The read takes no lock: admission
// re-checks the generation and Actor UID under the Session lock.
func (c *Client) GetSessionGeneration(ctx context.Context, id string) (*SessionGeneration, error) {
	type generationRow struct {
		OperationID           *uuid.UUID
		ActorUID              *string
		ActorTemplateAtespace string
		ActorTemplateName     string
	}
	row, err := queryOne(ctx, c.db, `
		SELECT i.operation_id, i.actor_uid, r.actor_template_atespace, r.actor_template_name
		FROM session_record i JOIN runtime_revision r ON r.revision = i.prepared_revision
		WHERE i.id = $1 AND i.state <> 'RUNTIME_STATE_DELETED'
	`, pgx.RowToStructByName[generationRow], id)
	if err != nil {
		return nil, fmt.Errorf("read Session %s generation: %w", id, notFoundOr(err))
	}
	generation := &SessionGeneration{SessionID: id, TemplateAtespace: row.ActorTemplateAtespace, TemplateName: row.ActorTemplateName}
	if row.OperationID != nil {
		generation.OperationID = *row.OperationID
	}
	if row.ActorUID != nil {
		generation.ActorUID = *row.ActorUID
	}
	return generation, nil
}

// BeginCrashedSessionRecovery admits confirmed-crash recovery under the Session
// lock. It preserves durable results; obsolete Actors and conflicting operations
// fail. generation is the caller's GetSessionGeneration read, taken before it
// observed the crash. The Session must still record its Actor UID, and a
// lifecycle operation begun since then makes the observation stale, because a
// concurrent revert keeps the Actor UID and returns to READY.
func (c *Client) BeginCrashedSessionRecovery(ctx context.Context, generation *SessionGeneration, snapshot SessionTaskSnapshot) (*SessionOperation, error) {
	var operation *SessionOperation
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockSession(ctx, tx, generation.SessionID)
		if err != nil {
			return notFoundOr(err)
		}
		// Both guards are read under the lock in one statement, and are evaluated in
		// their original order: Actor identity first, checkpoint creation last.
		type guards struct {
			Matches bool
			Busy    bool
		}
		guard, err := queryOne(ctx, tx, `
			SELECT actor_uid = $2 AS matches,
			    EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = $1 AND state = 'CREATING') AS busy
			FROM session WHERE id = $1
		`, pgx.RowToStructByName[guards], row.ID, generation.ActorUID)
		if err != nil {
			return err
		}
		if !guard.Matches || generation.ActorUID == "" {
			return ErrConflict
		}
		session, err := toSession(row)
		if err != nil {
			return err
		}
		if CrashRecoveryAdmitted(session) {
			operation, err = toSessionOperation(row)
			return err
		}
		if session.State != apiv1.RuntimeState_RUNTIME_STATE_READY || session.Operation != apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE {
			return ErrConflict
		}
		current := uuid.Nil
		if row.OperationID != nil {
			current = *row.OperationID
		}
		if current != generation.OperationID {
			return ErrConflict
		}
		// An issued idle suspension does not exclude recovery here. It cannot finish
		// against the crashed Actor, and its worker keeps the claim when suspension
		// fails, so waiting for it would block recovery permanently. Settlement below
		// retires the claim in this transaction; a late finish for it changes nothing.
		// The generation check keeps the crash observation current: without another
		// lifecycle operation, nothing returns the crashed Actor to service.
		if guard.Busy {
			return ErrConflict
		}
		row.State = apiv1.RuntimeState_RUNTIME_STATE_FAILED.String()
		row.runtimeInstanceRow, err = beginRuntimeOperation(ctx, tx, row.runtimeInstanceRow, runtimeKindAgent, apiv1.RuntimeOperation_RUNTIME_OPERATION_REVERT)
		if err != nil {
			return err
		}
		session.State, session.Operation = apiv1.RuntimeState_RUNTIME_STATE_FAILED, apiv1.RuntimeOperation_RUNTIME_OPERATION_REVERT
		session.Failure = &apiv1.Failure{Reason: SessionRuntimeCrashed, Message: "Runtime crashed; recovery to its last snapshot is pending"}
		session.UpdatedAt = timestamppb.Now()
		row.Data, err = marshalSession(session)
		if err != nil {
			return err
		}
		// Execution must restore the snapshot settlement below judged waits against,
		// so the operation pins it rather than letting Substrate choose at revert time.
		if err := execSQL(ctx, tx, `
			UPDATE session SET data = $2, dispatch_id = NULL, dispatch_expires_at = NULL,
			    recovery_operation_id = $3, recovery_snapshot_uri = $4
			WHERE id = $1
		`, row.ID, row.Data, row.OperationID, snapshot.URI); err != nil {
			return err
		}
		if err := settleCrashedSessionTask(ctx, tx, row, snapshot); err != nil {
			return err
		}
		operation, err = toSessionOperation(row)
		return err
	})
	return operation, err
}

// GetSessionRecoverySnapshot returns the durable snapshot URI pinned when the
// Session's REVERT operation was admitted. The URI is empty when the crashed
// Actor had no durable snapshot. Any other operation returns ErrNotFound.
func (c *Client) GetSessionRecoverySnapshot(ctx context.Context, sessionID string, operationID uuid.UUID) (string, error) {
	uri, err := queryOne(ctx, c.db, `
		SELECT s.recovery_snapshot_uri FROM session s JOIN runtime_instance r USING (id)
		WHERE s.id = $1 AND s.recovery_operation_id = $2
		  AND r.operation_id = $2 AND r.operation = 'RUNTIME_OPERATION_REVERT'
	`, pgx.RowTo[string], sessionID, operationID)
	if err != nil {
		return "", fmt.Errorf("read Session %s recovery snapshot: %w", sessionID, notFoundOr(err))
	}
	return uri, nil
}

// AbandonSessionRecovery ends an admitted REVERT whose Actor can no longer be
// restored: it is missing, was replaced, or no longer holds the pinned snapshot.
// Unlike a preparation failure it may end claimed work, because those
// observations show that no revert of the recorded Actor can still succeed. A
// live claim still wins, so an attempt that is issuing the revert finishes first.
// The Session stays FAILED with no operation, so Delete can proceed.
func (c *Client) AbandonSessionRecovery(ctx context.Context, sessionID string, operationID uuid.UUID, cause string) (*apiv1.Session, error) {
	var result *apiv1.Session
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockSession(ctx, tx, sessionID)
		if err != nil {
			return notFoundOr(err)
		}
		tag, err := tx.Exec(ctx, `
			UPDATE runtime_instance SET operation = 'RUNTIME_OPERATION_NONE', executor_id = NULL, executor_expires_at = NULL
			WHERE id = $1 AND kind = 'agent' AND operation_id = $2 AND operation = 'RUNTIME_OPERATION_REVERT'
			  AND (executor_id IS NULL OR executor_expires_at <= clock_timestamp())
		`, row.ID, operationID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
		row.Operation, row.ExecutorID = apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE.String(), nil
		result, err = toSession(row)
		if err != nil {
			return err
		}
		result.Operation = apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE
		result.Failure = &apiv1.Failure{Reason: SessionRuntimeUnrecoverable, Message: "Runtime cannot be recovered: " + cause}
		result.UpdatedAt = timestamppb.Now()
		row.Data, err = marshalSession(result)
		if err != nil {
			return err
		}
		return execSQL(ctx, tx, `UPDATE session SET data = $2 WHERE id = $1`, row.ID, row.Data)
	})
	return result, err
}

// settleCrashedSessionTask retains captured waits and fails discarded nonterminal work.
// Callers hold the Session lock; recovery cannot checkpoint discarded state.
func settleCrashedSessionTask(ctx context.Context, tx pgx.Tx, session sessionRow, snapshot SessionTaskSnapshot) error {
	type interruptedTask struct {
		ID      string
		Data    []byte
		Version *int64
		Pending bool
	}
	// The CTE clears the history's pending quiescence marker. The main query
	// reads the pre-update snapshot, but pending only tests that a boundary was
	// recorded, which clearing keeps true. A task without events is malformed.
	tasks, err := queryMany(ctx, tx, `
		WITH cleared AS (
		    UPDATE session_task_event SET quiescence_pending = FALSE
		    WHERE history_id = $1 AND quiescence_pending)
		SELECT t.id, t.data, latest.sequence AS version,
		    COALESCE(NOT latest.published AND latest.quiescence_pending IS NOT NULL, FALSE) AS pending
		FROM session_task t
		LEFT JOIN LATERAL (SELECT sequence, published, quiescence_pending FROM session_task_event e
		    WHERE e.history_id = t.history_id AND e.task_id = t.id
		    ORDER BY sequence DESC LIMIT 1) latest ON TRUE
		WHERE t.history_id = $1
		    AND (t.state IN ('TASK_STATE_SUBMITTED', 'TASK_STATE_WORKING')
		      OR (t.state IN ('TASK_STATE_INPUT_REQUIRED', 'TASK_STATE_AUTH_REQUIRED')
		        AND NOT EXISTS (SELECT 1 FROM session_task_event captured
		          WHERE captured.history_id = t.history_id AND $2::text <> ''
		            AND captured.snapshot_uri = $2 AND captured.snapshot_fidelity = $3
		            AND captured.snapshot_atespace = $4
		            AND captured.sequence >= latest.sequence)))
	`, pgx.RowToStructByName[interruptedTask], session.HistoryID, snapshot.URI, snapshot.Fidelity, snapshot.Atespace)
	if err != nil {
		return fmt.Errorf("read interrupted Session tasks: %w", err)
	}
	for _, interrupted := range tasks {
		taskID, data := interrupted.ID, interrupted.Data
		if interrupted.Version == nil {
			return fmt.Errorf("interrupted Session task %s has no events", taskID)
		}
		if interrupted.Pending {
			if err := settleSessionTask(ctx, tx, session, taskID, *interrupted.Version); err != nil {
				return err
			}
			// Settlement rewrote the projection, so the selected data is stale.
			stored, err := readSessionTask(ctx, tx, session.HistoryID, taskID)
			if err != nil {
				return err
			}
			data = stored.Data
		}
		task, err := unmarshalSessionTask(data)
		if err != nil {
			return err
		}
		if !task.Status.State.Terminal() {
			task.Status = a2a.TaskStatus{State: a2a.TaskStateFailed, Timestamp: new(time.Now().UTC()),
				Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("The runtime crashed before this turn completed. Send a new message to continue."))}
			apia2a.SetTimelinePosition(task.Status.Message, *task.Status.Timestamp)
			event := &a2a.TaskStatusUpdateEvent{TaskID: task.ID, ContextID: task.ContextID, Status: task.Status}
			if err := storeSessionTaskEvent(ctx, tx, session, task, event, false); err != nil {
				return err
			}
			version, err := taskVersion(ctx, tx, session.HistoryID, taskID)
			if err != nil {
				return err
			}
			if err := publishPushBoundary(ctx, tx, session.HistoryID, taskID, version, task); err != nil {
				return err
			}
		}
	}
	return nil
}
