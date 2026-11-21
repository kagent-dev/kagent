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

// SessionQuiescence grants one worker permission to suspend an idle session
// and record its snapshot. Task publication has already completed.
type SessionQuiescence struct {
	Session    *apiv1.Session
	TaskID     string
	State      a2a.TaskState
	Version    int64
	ExecutorID uuid.UUID
}

// HasPendingSessionQuiescence includes ready sessions temporarily blocked by
// dispatch, checkpoints, lifecycle operations, or row locks. Already claimed
// work is excluded: its runtime outcome may be uncertain and cannot be retried
// by another worker. Suspended/deleted sessions need no automatic runtime work.
func (c *Client) HasPendingSessionQuiescence(ctx context.Context) (bool, error) {
	return queryOne(ctx, c.db, `
		SELECT EXISTS (
			SELECT 1 FROM session_task_event e JOIN session_record i ON i.history_id = e.history_id
			WHERE e.published AND e.quiescence_pending = TRUE
			  AND e.quiescence_executor_id IS NULL
			  AND i.state = 'RUNTIME_STATE_READY'
		)
	`, pgx.RowTo[bool])
}

// ClaimSessionQuiescence claims an idle session at its latest settled task
// version. A new turn supersedes unclaimed idle work; a claim blocks task writes,
// checkpoints, and explicit lifecycle operations until it finishes. Missing work
// returns ErrNotFound. Uncertain issued work is never reassigned after a timeout.
func (c *Client) ClaimSessionQuiescence(ctx context.Context) (*SessionQuiescence, error) {
	var result *SessionQuiescence
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		type candidate struct {
			SessionID string
			TaskID    string
			Sequence  int64
		}
		row, err := queryOne(ctx, tx, `
			SELECT i.id::text AS session_id, e.task_id, e.sequence
			FROM session_task_event e JOIN session_record i ON i.history_id = e.history_id
			WHERE e.published AND e.quiescence_pending = TRUE
			  AND e.quiescence_executor_id IS NULL
			  AND i.state = 'RUNTIME_STATE_READY'
			  AND i.operation = 'RUNTIME_OPERATION_NONE'
			  AND (i.dispatch_expires_at IS NULL OR i.dispatch_expires_at <= clock_timestamp())
			  AND NOT EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = i.id AND state = 'CREATING')
			ORDER BY e.sequence LIMIT 1 FOR UPDATE OF i SKIP LOCKED
		`, pgx.RowToStructByName[candidate])
		if err != nil {
			return notFoundOr(err)
		}
		session, err := readSession(ctx, tx, row.SessionID)
		if err != nil {
			return err
		}
		if session.State != "RUNTIME_STATE_READY" || session.Operation != "RUNTIME_OPERATION_NONE" {
			return ErrNotFound
		}
		stored, err := readSessionTask(ctx, tx, session.HistoryID, row.TaskID)
		if err != nil {
			return err
		}
		task, err := unmarshalSessionTask(stored.Data)
		if err != nil {
			return err
		}
		value, err := toSession(session)
		if err != nil {
			return err
		}
		result = &SessionQuiescence{Session: value, TaskID: row.TaskID, State: task.Status.State, Version: row.Sequence, ExecutorID: uuid.New()}
		// Recheck after locking: the candidate query's snapshot may predate a
		// new turn that committed just before we acquired the session lock.
		tag, err := tx.Exec(ctx, `
			UPDATE session_task_event SET quiescence_executor_id = $2, quiescence_claimed_at = clock_timestamp()
			WHERE sequence = $1 AND published AND quiescence_pending
			  AND quiescence_executor_id IS NULL
			  AND NOT EXISTS (SELECT 1 FROM session WHERE id = $3 AND dispatch_expires_at > clock_timestamp())
			  AND NOT EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = $3 AND state = 'CREATING')
		`, result.Version, result.ExecutorID, session.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrNotFound
		}
		return nil
	})
	return result, err
}

// FinishSessionQuiescence records a claimed suspension outcome and releases
// the session for new execution. Every settled task requires the matching snapshot.
// Retries are harmless, and a stale claim cannot release another operation.
// Task state and history remain readable throughout lifecycle work.
func (c *Client) FinishSessionQuiescence(ctx context.Context, work *SessionQuiescence, snapshot *SessionTaskSnapshot) error {
	if work == nil || work.ExecutorID == uuid.Nil {
		return fmt.Errorf("claimed task boundary is required")
	}
	if snapshot == nil || snapshot.URI == "" || snapshot.Atespace == "" || snapshot.Fidelity == "" {
		return fmt.Errorf("task quiescence requires a runtime snapshot")
	}
	return c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, work.Session.Id)
		if err != nil {
			return notFoundOr(err)
		}
		if session.State == "RUNTIME_STATE_DELETED" {
			return ErrNotFound
		}
		pending, err := queryOne(ctx, tx, `
			SELECT quiescence_pending FROM session_task_event
			WHERE sequence = $1 AND history_id = $2 AND task_id = $3 AND quiescence_executor_id = $4
		`, pgx.RowTo[bool], work.Version, session.HistoryID, work.TaskID, work.ExecutorID)
		if err != nil {
			return notFoundOr(err)
		}
		if !pending {
			return nil
		}
		stored, err := readSessionTask(ctx, tx, session.HistoryID, work.TaskID)
		if err != nil {
			return err
		}
		if stored.State != string(work.State) {
			return ErrConflict
		}
		if snapshot != nil {
			if err := execSQL(ctx, tx, `
				UPDATE session_task SET snapshot_atespace = $3, snapshot_uri = $4,
				    snapshot_fidelity = $5, history_sequence = $6
				WHERE history_id = $1 AND id = $2
			`, session.HistoryID, work.TaskID, snapshot.Atespace, snapshot.URI, snapshot.Fidelity, work.Version); err != nil {
				return err
			}
			if err := execSQL(ctx, tx, `
				UPDATE session_task_event SET snapshot_atespace = $2, snapshot_uri = $3, snapshot_fidelity = $4
				WHERE sequence = $1
			`, work.Version, snapshot.Atespace, snapshot.URI, snapshot.Fidelity); err != nil {
				return err
			}
		}
		return execSQL(ctx, tx, `
			UPDATE session_task_event SET quiescence_pending = FALSE, quiescence_claimed_at = NULL
			WHERE sequence = $1
		`, work.Version)
	})
}

// ClaimStaleSessionQuiescence takes over a claim whose runtime outcome was
// never recorded, so it can be reconciled against Substrate rather than left
// holding the session. Only claims older than olderThan are eligible, which
// leaves a suspension that is merely slow with the worker that issued it. A
// claim with no recorded time belongs to a replica that has not been upgraded
// and may still be inside its call, so it is left alone; the upgrade dates the
// claims that existed before it. Taking over invalidates the previous claim:
// its executor ID no longer matches, so a late finish from it is ignored.
func (c *Client) ClaimStaleSessionQuiescence(ctx context.Context, olderThan time.Duration) (*SessionQuiescence, error) {
	var result *SessionQuiescence
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		type candidate struct {
			SessionID string
			TaskID    string
			Sequence  int64
		}
		row, err := queryOne(ctx, tx, `
			SELECT i.id::text AS session_id, e.task_id, e.sequence
			FROM session_task_event e JOIN session_record i ON i.history_id = e.history_id
			WHERE e.published AND e.quiescence_pending = TRUE
			  AND e.quiescence_executor_id IS NOT NULL
			  AND e.quiescence_claimed_at <= clock_timestamp() - $1::interval
			ORDER BY e.sequence LIMIT 1 FOR UPDATE OF i SKIP LOCKED
		`, pgx.RowToStructByName[candidate], olderThan)
		if err != nil {
			return notFoundOr(err)
		}
		session, err := readSession(ctx, tx, row.SessionID)
		if err != nil {
			return err
		}
		stored, err := readSessionTask(ctx, tx, session.HistoryID, row.TaskID)
		if err != nil {
			return err
		}
		task, err := unmarshalSessionTask(stored.Data)
		if err != nil {
			return err
		}
		value, err := toSession(session)
		if err != nil {
			return err
		}
		result = &SessionQuiescence{Session: value, TaskID: row.TaskID, State: task.Status.State, Version: row.Sequence, ExecutorID: uuid.New()}
		tag, err := tx.Exec(ctx, `
			UPDATE session_task_event SET quiescence_executor_id = $2, quiescence_claimed_at = clock_timestamp()
			WHERE sequence = $1 AND published AND quiescence_pending
			  AND quiescence_executor_id IS NOT NULL
			  AND quiescence_claimed_at <= clock_timestamp() - $3::interval
		`, result.Version, result.ExecutorID, olderThan)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrNotFound
		}
		return nil
	})
	return result, err
}

// RetireSessionQuiescence gives up a claim and the suspension it owed, for an
// Actor that no longer exists. Releasing alone would leave the work pending,
// and the next worker would claim it and fail against the same missing Actor,
// closing the session again. There is no snapshot to record and no runtime
// left to take one, so the boundary ends here and the session can be deleted.
func (c *Client) RetireSessionQuiescence(ctx context.Context, work *SessionQuiescence) error {
	return c.giveUpQuiescence(ctx, work, true)
}

// ReleaseSessionQuiescence gives up a claim without recording a snapshot, for
// a suspension Substrate says did not happen. The session is admitted again
// immediately, so answers, cancellation and deletion stop being refused, and
// the idle work stays pending for an ordinary claim to retry.
func (c *Client) ReleaseSessionQuiescence(ctx context.Context, work *SessionQuiescence) error {
	return c.giveUpQuiescence(ctx, work, false)
}

// giveUpQuiescence ends a claim the caller holds. retire also clears the
// pending suspension, so nothing claims it again.
func (c *Client) giveUpQuiescence(ctx context.Context, work *SessionQuiescence, retire bool) error {
	if work == nil || work.ExecutorID == uuid.Nil {
		return fmt.Errorf("claimed task boundary is required")
	}
	return c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, work.Session.Id)
		if err != nil {
			return notFoundOr(err)
		}
		tag, err := tx.Exec(ctx, `
			UPDATE session_task_event
			SET quiescence_executor_id = NULL, quiescence_claimed_at = NULL,
			    quiescence_pending = CASE WHEN $5 THEN FALSE ELSE quiescence_pending END
			WHERE sequence = $1 AND history_id = $2 AND task_id = $3 AND quiescence_executor_id = $4
		`, work.Version, session.HistoryID, work.TaskID, work.ExecutorID, retire)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			// The claim was taken over or finished by someone else. Saying so
			// keeps a caller from reporting a release that did not happen.
			return ErrNotFound
		}
		return nil
	})
}
