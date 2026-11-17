package database

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// SessionDispatchCleanup identifies an unused, revoked dispatch for local cleanup.
// TaskID and Version pin the history boundary whose snapshot may be replaced.
type SessionDispatchCleanup struct {
	Session   *apiv1.Session
	CleanupID uuid.UUID
	TaskID    string
	Version   int64
}

// GetSessionDispatchCleanup reads an unused, revoked dispatch without blocking
// new sends. Newer sends, active turns, and pending or claimed post-turn suspension
// supersede it with ErrNotFound; a checkpoint returns ErrDispatchBusy for a retry.
// The session row lock makes these reads consistent and ends before runtime I/O.
func (c *Client) GetSessionDispatchCleanup(ctx context.Context, sessionID string, cleanupID uuid.UUID) (*SessionDispatchCleanup, error) {
	var result *SessionDispatchCleanup
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, sessionID)
		if err != nil {
			return notFoundOr(err)
		}
		if session.State != apiv1.RuntimeState_RUNTIME_STATE_READY.String() || session.Operation != apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE.String() {
			return ErrNotFound
		}
		unused, err := queryOne(ctx, tx, `
			SELECT EXISTS (SELECT 1 FROM session
			    WHERE id = $1 AND dispatch_id = $2 AND dispatch_expires_at <= clock_timestamp())
		`, pgx.RowTo[bool], session.ID, cleanupID)
		if err != nil {
			return err
		}
		if !unused {
			return ErrNotFound
		}
		if err := requireSettledRuntime(ctx, tx, session.HistoryID, ""); err != nil {
			if errors.Is(err, ErrFailedPrecondition) {
				return ErrNotFound // Native or post-turn cleanup already owns this session.
			}
			return err
		}
		// Leave queued suspension to its normal worker even before it is claimed.
		busy, err := queryOne(ctx, tx, `
			SELECT EXISTS (SELECT 1 FROM session_task WHERE history_id = $1
			    AND state IN ('TASK_STATE_SUBMITTED', 'TASK_STATE_WORKING'))
			    OR EXISTS (SELECT 1 FROM session_task_event WHERE history_id = $1 AND quiescence_pending)
		`, pgx.RowTo[bool], session.HistoryID)
		if err != nil {
			return err
		}
		if busy {
			return ErrNotFound
		}
		checkpointing, err := queryOne(ctx, tx, `
			SELECT EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = $1 AND state = 'CREATING')
		`, pgx.RowTo[bool], session.ID)
		if err != nil {
			return err
		}
		if checkpointing {
			return ErrDispatchBusy
		}
		work := &SessionDispatchCleanup{CleanupID: cleanupID}
		type boundary struct {
			TaskID   string
			Sequence int64
		}
		latest, err := queryOne(ctx, tx, `
			SELECT task_id, sequence FROM session_task_event
			WHERE history_id = $1 ORDER BY sequence DESC LIMIT 1
		`, pgx.RowToStructByName[boundary], session.HistoryID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		work.TaskID, work.Version = latest.TaskID, latest.Sequence
		work.Session, err = toSession(session)
		result = work
		return err
	})
	return result, err
}

// FinishSessionDispatchCleanup records the captured boundary's new snapshot and
// clears only its revoked dispatch. A newer send makes completion a harmless
// no-op, including after a lost response. The check and writes are atomic.
// A nil snapshot retires cleanup of a crashed, missing, or replaced actor.
// Post-turn suspension markers belong to their worker and are never cleared here.
func (c *Client) FinishSessionDispatchCleanup(ctx context.Context, work *SessionDispatchCleanup, snapshot *SessionTaskSnapshot) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, work.Session.Id)
		if err != nil {
			return notFoundOr(err)
		}
		tag, err := tx.Exec(ctx, `
			UPDATE session SET dispatch_id = NULL, dispatch_expires_at = NULL
			WHERE id = $1 AND dispatch_id = $2
		`, session.ID, work.CleanupID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return nil
		}
		if snapshot == nil || work.TaskID == "" {
			return nil
		}
		unchanged, err := queryOne(ctx, tx, `
			SELECT COALESCE(MAX(sequence), 0) = $2 FROM session_task_event WHERE history_id = $1
		`, pgx.RowTo[bool], session.HistoryID, work.Version)
		if err != nil {
			return err
		}
		if !unchanged || session.State != apiv1.RuntimeState_RUNTIME_STATE_READY.String() || session.Operation != apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE.String() {
			return ErrConflict
		}
		return recordSessionTaskSnapshot(ctx, tx, session.HistoryID, work.TaskID, work.Version, snapshot)
	})
}
