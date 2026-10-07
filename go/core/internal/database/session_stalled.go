package database

import (
	"context"
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/jackc/pgx/v5"
)

// StalledSessionTask is a submitted or working task whose last recorded event
// is older than a cutoff: the turn started and nothing has been saved for it since.
type StalledSessionTask struct {
	SessionID   string
	TaskID      string
	State       a2a.TaskState
	LastEventAt time.Time
}

// ListStalledSessionTasks returns active tasks whose last event was recorded
// before cutoff, oldest first, up to limit. Tasks of a session in a lifecycle
// operation and tasks whose dispatch reservation has not expired are left out.
// Admission is rechecked under the session lock by InterruptSessionTask.
func (c *Client) ListStalledSessionTasks(ctx context.Context, cutoff time.Time, limit int) ([]StalledSessionTask, error) {
	tasks, err := queryMany(ctx, c.db, `
		SELECT i.id::text AS session_id, t.id AS task_id, t.state, latest.created_at AS last_event_at
		FROM session_task t JOIN session_record i ON i.history_id = t.history_id
		JOIN LATERAL (
		    SELECT created_at FROM session_task_event
		    WHERE history_id = t.history_id AND task_id = t.id ORDER BY sequence DESC LIMIT 1
		) latest ON TRUE
		WHERE t.state IN ('TASK_STATE_SUBMITTED', 'TASK_STATE_WORKING')
		  AND latest.created_at < $1
		  AND i.state = 'RUNTIME_STATE_READY'
		  AND i.operation = 'RUNTIME_OPERATION_NONE'
		  AND (i.dispatch_expires_at IS NULL OR i.dispatch_expires_at <= clock_timestamp())
		ORDER BY latest.created_at, i.id LIMIT $2
	`, pgx.RowToStructByName[StalledSessionTask], cutoff, int32(limit))
	if err != nil {
		return nil, fmt.Errorf("list stalled Session tasks: %w", err)
	}
	return tasks, nil
}

// SessionTaskInterruption is how a stalled task ended: Settled when the
// runtime's own saved boundary was published, otherwise a failure was appended.
type SessionTaskInterruption struct {
	State   a2a.TaskState
	Settled bool
}

// InterruptSessionTask ends a submitted or working task that recorded no event
// since cutoff; a zero cutoff ends the task whatever it recorded last, for a
// caller that knows its runtime is gone. A boundary the runtime saved but never
// settled is published as it is: the runtime's outcome wins over an
// interruption. Otherwise a published
// FAILED status update carrying message ends the task, so a late runtime save
// fails on the terminal task and the session takes its next message. The
// boundary carries no idle work: a suspend of a runtime that may be gone would
// leave a claim that never expires. A task that moved on, a session in a
// lifecycle operation, or an unexpired dispatch returns ErrConflict; a claimed
// idle boundary returns ErrFailedPrecondition.
func (c *Client) InterruptSessionTask(ctx context.Context, sessionID, taskID string, cutoff time.Time, message string) (*SessionTaskInterruption, error) {
	if message == "" {
		return nil, fmt.Errorf("task interruption requires a message")
	}
	var result *SessionTaskInterruption
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, sessionID)
		if err != nil {
			return notFoundOr(err)
		}
		if session.State == "RUNTIME_STATE_DELETED" {
			return ErrNotFound
		}
		if session.State != "RUNTIME_STATE_READY" || session.Operation != "RUNTIME_OPERATION_NONE" {
			return fmt.Errorf("session %s is %s with operation %s: %w", sessionID, session.State, session.Operation, ErrConflict)
		}
		row, err := readSessionTask(ctx, tx, session.HistoryID, taskID)
		if err != nil {
			return notFoundOr(err)
		}
		if row.State != string(a2a.TaskStateSubmitted) && row.State != string(a2a.TaskStateWorking) {
			return fmt.Errorf("task %s is %s: %w", taskID, row.State, ErrConflict)
		}
		type latestEvent struct {
			Sequence  int64
			CreatedAt time.Time
			Published bool
			Boundary  bool
		}
		latest, err := queryOne(ctx, tx, `
			SELECT sequence, created_at, published,
			    (expected_version IS NOT NULL AND quiescence_pending IS NOT NULL) AS boundary
			FROM session_task_event WHERE history_id = $1 AND task_id = $2
			ORDER BY sequence DESC LIMIT 1
		`, pgx.RowToStructByName[latestEvent], session.HistoryID, taskID)
		if err != nil {
			return notFoundOr(err)
		}
		if !cutoff.IsZero() && !latest.CreatedAt.Before(cutoff) {
			return fmt.Errorf("task %s recorded an event at %s: %w", taskID, latest.CreatedAt.Format(time.RFC3339), ErrConflict)
		}
		if !latest.Published && latest.Boundary {
			if err := settleSessionTask(ctx, tx, session, taskID, latest.Sequence); err != nil {
				return err
			}
			settled, err := readSessionTask(ctx, tx, session.HistoryID, taskID)
			if err != nil {
				return err
			}
			result = &SessionTaskInterruption{State: a2a.TaskState(settled.State), Settled: true}
			return nil
		}
		if err := requireSettledRuntime(ctx, tx, session.HistoryID, ""); err != nil {
			return err
		}
		if err := execSQL(ctx, tx, `
			UPDATE session_task_event SET quiescence_pending = FALSE
			WHERE history_id = $1 AND quiescence_pending
		`, session.HistoryID); err != nil {
			return err
		}
		task, err := unmarshalSessionTask(row.Data)
		if err != nil {
			return err
		}
		event := a2a.NewStatusUpdateEvent(task, a2a.TaskStateFailed, a2a.NewMessageForTask(a2a.MessageRoleAgent, task, a2a.NewTextPart(message)))
		task.Status = event.Status
		if err := storeSessionTaskEvent(ctx, tx, session, task, event, false); err != nil {
			return err
		}
		result = &SessionTaskInterruption{State: a2a.TaskStateFailed}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("interrupt Session task %s: %w", taskID, err)
	}
	return result, nil
}
