package database

import (
	"context"
	"errors"
	"fmt"

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

// GetSessionDispatchCleanup reads a revoked dispatch or its worker's suspension claim.
// Newer sends, active turns, and pending or claimed post-turn suspension
// supersede it with ErrNotFound; a checkpoint returns ErrDispatchBusy for a retry.
// The session row lock makes these reads consistent and ends before runtime I/O.
func (c *Client) GetSessionDispatchCleanup(ctx context.Context, sessionID string, revokedID, claimID uuid.UUID) (*SessionDispatchCleanup, error) {
	return c.sessionDispatchCleanup(ctx, sessionID, revokedID, claimID, false)
}

// ClaimSessionDispatchSuspension closes admission immediately before SuspendActor.
// The worker keeps its private claimID across retries, including a lost commit reply.
// A different worker or a newer send cannot finish or acquire the same claim.
func (c *Client) ClaimSessionDispatchSuspension(ctx context.Context, sessionID string, revokedID, claimID uuid.UUID) (*SessionDispatchCleanup, error) {
	if claimID == uuid.Nil || claimID == revokedID {
		return nil, fmt.Errorf("suspension requires a distinct claim ID")
	}
	return c.sessionDispatchCleanup(ctx, sessionID, revokedID, claimID, true)
}

// sessionDispatchCleanup checks eligibility and captures the latest task boundary
// under the same lock as send admission. Only a granted claim survives the transaction.
func (c *Client) sessionDispatchCleanup(ctx context.Context, sessionID string, revokedID, claimID uuid.UUID, claim bool) (*SessionDispatchCleanup, error) {
	var result *SessionDispatchCleanup
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, sessionID)
		if err != nil {
			return notFoundOr(err)
		}
		if session.State != apiv1.RuntimeState_RUNTIME_STATE_READY.String() || session.Operation != apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE.String() {
			return ErrNotFound
		}
		id, err := queryOne(ctx, tx, `
			SELECT dispatch_id FROM session WHERE id = $1 AND (
			    (dispatch_id = $2 AND dispatch_expires_at <= clock_timestamp()) OR
			    (dispatch_id = $3 AND dispatch_expires_at = 'infinity'))
		`, pgx.RowTo[uuid.UUID], session.ID, revokedID, claimID)
		if err != nil {
			return notFoundOr(err)
		}
		if id == revokedID {
			if err := dispatchCleanupEligible(ctx, tx, session); err != nil {
				return err
			}
			if claim {
				// The token is private to the worker. A suspend can outlive its RPC,
				// so admission must stay closed until its outcome is known.
				if err := execSQL(ctx, tx, `
					UPDATE session SET dispatch_id = $2, dispatch_expires_at = 'infinity' WHERE id = $1
				`, session.ID, claimID); err != nil {
					return err
				}
				id = claimID
			}
		}
		work := &SessionDispatchCleanup{CleanupID: id}
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

// dispatchCleanupEligible yields to admitted execution and post-turn work.
// A checkpoint can finish without replacing the dispatch, so retry it later.
func dispatchCleanupEligible(ctx context.Context, tx pgx.Tx, session sessionRow) error {
	type blockers struct {
		NewWork    bool `db:"new_work"`
		Checkpoint bool
	}
	state, err := queryOne(ctx, tx, `
		SELECT EXISTS (SELECT 1 FROM session_task WHERE history_id = $1
		        AND state IN ('TASK_STATE_SUBMITTED', 'TASK_STATE_WORKING'))
		    OR EXISTS (SELECT 1 FROM session_task_event WHERE history_id = $1
		        AND (NOT published OR quiescence_pending)) AS new_work,
		    EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = $2 AND state = 'CREATING') AS checkpoint
	`, pgx.RowToStructByName[blockers], session.HistoryID, session.ID)
	if err != nil {
		return err
	}
	if state.NewWork {
		return ErrNotFound
	}
	if state.Checkpoint {
		return ErrDispatchBusy
	}
	return nil
}

// FinishSessionDispatchCleanup records the captured boundary's new snapshot and
// releases only the matching dispatch or suspension claim in the same transaction.
// A failed snapshot write rolls back the release so checkpoints cannot use the old snapshot.
// A superseded or already-finished request is a no-op.
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
