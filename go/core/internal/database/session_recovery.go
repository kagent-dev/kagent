package database

import (
	"context"
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/types/known/timestamppb"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// SessionRuntimeCrashed identifies a recovery admitted after Substrate confirmed
// that the recorded Actor crashed. Transport failures cannot admit this work.
const SessionRuntimeCrashed = "RuntimeCrashed"

// ListSessionRecoveryCandidates returns an ID-ordered page of accepted active
// turns and admitted recoveries. It never includes idle or publicly waiting
// sessions. Callers verify the recorded Actor before admitting recovery; this
// read grants no execution authority. The active-task predicate matches the
// existing partial index, and Actor reads are bounded by the page size.
func (c *Client) ListSessionRecoveryCandidates(ctx context.Context, afterID string, limit int) ([]*apiv1.Session, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT id, user_id, prepared_revision, state, data, operation, context_id,
		    source_checkpoint_id, history_id, operation_id, executor_id
		FROM session_record i
		WHERE ($1::text = '' OR i.id > $1::uuid)
		  AND ((i.state = 'RUNTIME_STATE_READY' AND i.operation = 'RUNTIME_OPERATION_NONE'
		    AND EXISTS (SELECT 1 FROM session_task t WHERE t.history_id = i.history_id
		      AND t.state NOT IN ('TASK_STATE_COMPLETED', 'TASK_STATE_CANCELED', 'TASK_STATE_FAILED',
		        'TASK_STATE_REJECTED', 'TASK_STATE_INPUT_REQUIRED', 'TASK_STATE_AUTH_REQUIRED')))
		    OR (i.state = 'RUNTIME_STATE_FAILED' AND i.operation = 'RUNTIME_OPERATION_RESUME'))
		ORDER BY i.id LIMIT $2
	`, pgx.RowToStructByName[sessionRow], afterID, limit)
	if err != nil {
		return nil, err
	}
	result := make([]*apiv1.Session, 0, len(rows))
	for _, row := range rows {
		session, err := toSession(row)
		if err != nil {
			return nil, err
		}
		result = append(result, session)
	}
	return result, nil
}

// BeginCrashedSessionRecovery closes admission for a confirmed crashed Actor
// under the same lock as runtime writes and lifecycle changes. A concurrent
// terminal write wins before this lock or is retained for recovery to publish;
// it is never replaced using the candidate scan's older projection. Repeated
// admission joins the same recovery. Obsolete Actors and superseding lifecycle
// work return a conflict; missing Sessions return ErrNotFound. No runtime I/O
// runs inside this transaction.
func (c *Client) BeginCrashedSessionRecovery(ctx context.Context, id, actorUID string) (*SessionOperation, error) {
	var operation *SessionOperation
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockSession(ctx, tx, id)
		if err != nil {
			return notFoundOr(err)
		}
		matches, err := queryOne(ctx, tx, `SELECT actor_uid = $2 FROM session WHERE id = $1`, pgx.RowTo[bool], row.ID, actorUID)
		if err != nil {
			return err
		}
		if !matches || actorUID == "" {
			return ErrConflict
		}
		session, err := toSession(row)
		if err != nil {
			return err
		}
		if session.State == apiv1.RuntimeState_RUNTIME_STATE_FAILED && session.Operation == apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME && session.Failure.GetReason() == SessionRuntimeCrashed {
			operation, err = toSessionOperation(row)
			return err
		}
		if session.State != apiv1.RuntimeState_RUNTIME_STATE_READY || session.Operation != apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE {
			return ErrConflict
		}
		active, err := queryOne(ctx, tx, `
			SELECT EXISTS (SELECT 1 FROM session_task WHERE history_id = $1
			    AND state IN ('TASK_STATE_SUBMITTED', 'TASK_STATE_WORKING'))
			    AND NOT EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = $2 AND state = 'CREATING')
		`, pgx.RowTo[bool], row.HistoryID, row.ID)
		if err != nil {
			return err
		}
		if !active {
			return ErrConflict
		}
		row.State = apiv1.RuntimeState_RUNTIME_STATE_FAILED.String()
		row.runtimeInstanceRow, err = beginRuntimeOperation(ctx, tx, row.runtimeInstanceRow, runtimeKindAgent, apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME)
		if err != nil {
			return err
		}
		session.State, session.Operation = apiv1.RuntimeState_RUNTIME_STATE_FAILED, apiv1.RuntimeOperation_RUNTIME_OPERATION_RESUME
		session.Failure = &apiv1.Failure{Reason: SessionRuntimeCrashed, Message: "Runtime crashed; recovery to its last snapshot is pending"}
		session.UpdatedAt = timestamppb.Now()
		row.Data, err = marshalSession(session)
		if err != nil {
			return err
		}
		if err := execSQL(ctx, tx, `UPDATE session SET data = $2, dispatch_id = NULL, dispatch_expires_at = NULL WHERE id = $1`, row.ID, row.Data); err != nil {
			return err
		}
		if err := settleCrashedSessionTask(ctx, tx, row); err != nil {
			return err
		}
		operation, err = toSessionOperation(row)
		return err
	})
	return operation, err
}

// settleCrashedSessionTask publishes a staged result or fails the interrupted
// turn atomically with confirmed-crash admission. A CRASHED Actor can no longer
// execute the turn, even if reverting its discarded state remains unavailable.
// Recovery does not capture this turn's state: never attach the older Actor
// snapshot to it. Callers hold the Session row lock and fence new execution.
func settleCrashedSessionTask(ctx context.Context, tx pgx.Tx, session sessionRow) error {
	taskID, err := queryOne(ctx, tx, `
		SELECT id FROM session_task WHERE history_id = $1
		    AND state IN ('TASK_STATE_SUBMITTED', 'TASK_STATE_WORKING')
	`, pgx.RowTo[string], session.HistoryID)
	if err != nil {
		return fmt.Errorf("read interrupted Session task: %w", err)
	}
	version, err := taskVersion(ctx, tx, session.HistoryID, taskID)
	if err != nil {
		return err
	}
	pending, err := queryOne(ctx, tx, `
		SELECT EXISTS (SELECT 1 FROM session_task_event
		    WHERE sequence = $1 AND NOT published AND quiescence_pending IS NOT NULL)
	`, pgx.RowTo[bool], version)
	if err != nil {
		return err
	}
	if pending {
		if err := settleSessionTask(ctx, tx, session, taskID, version); err != nil {
			return err
		}
	}
	stored, err := readSessionTask(ctx, tx, session.HistoryID, taskID)
	if err != nil {
		return err
	}
	task, err := unmarshalSessionTask(stored.Data)
	if err != nil {
		return err
	}
	if !task.Status.State.Terminal() {
		task.Status = a2a.TaskStatus{State: a2a.TaskStateFailed, Timestamp: new(time.Now().UTC()),
			Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("The runtime crashed before this turn completed. Send a new message to continue."))}
		event := &a2a.TaskStatusUpdateEvent{TaskID: task.ID, ContextID: task.ContextID, Status: task.Status}
		if err := storeSessionTaskEvent(ctx, tx, session, task, event, false); err != nil {
			return err
		}
		version, err = taskVersion(ctx, tx, session.HistoryID, taskID)
		if err != nil {
			return err
		}
		if err := publishPushBoundary(ctx, tx, session.HistoryID, taskID, version, task); err != nil {
			return err
		}
	}
	return execSQL(ctx, tx, `UPDATE session_task_event SET quiescence_pending = FALSE WHERE history_id = $1 AND quiescence_pending`, session.HistoryID)
}
