package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

// SessionQuiescence grants one worker permission to pause an idle session or
// suspend it and record its snapshot. Task publication has already completed.
type SessionQuiescence struct {
	Session    *apiv1alpha1.Session
	TaskID     string
	State      a2a.TaskState
	Version    int64
	ExecutorID uuid.UUID
	// Suspend asks for a durable suspend of a waiting task's runtime whose
	// pause is older than the claim's TTL, in place of a second pause.
	Suspend bool
}

type quiescenceCandidate struct {
	SessionID string
	TaskID    string
	Sequence  int64
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
//
// With a positive pauseTTL, a waiting task whose pause finished longer ago than
// the TTL and whose runtime has no recorded snapshot is claimed again, for a
// suspend: the pause checkpoint lives on one node, and a reply that takes longer
// than the TTL may outlive that node. A reply re-arms nothing: it clears the
// boundary's idle marker and the task leaves the waiting state. Sessions in
// skip are left out of that second claim, for a caller that found their suspend
// impossible for now.
func (c *Client) ClaimSessionQuiescence(ctx context.Context, pauseTTL time.Duration, skip []string) (*SessionQuiescence, error) {
	var result *SessionQuiescence
	err := c.withTx(ctx, func(tx pgx.Tx) error {
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
		`, pgx.RowToStructByName[quiescenceCandidate])
		suspend := false
		if errors.Is(err, pgx.ErrNoRows) && pauseTTL > 0 {
			row, err = expiredPauseCandidate(ctx, tx, pauseTTL, skip)
			suspend = true
		}
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
		result = &SessionQuiescence{Session: value, TaskID: row.TaskID, State: task.Status.State, Version: row.Sequence, ExecutorID: uuid.New(), Suspend: suspend}
		// Recheck after locking: the candidate query's snapshot may predate a
		// new turn that committed just before we acquired the session lock.
		var tag pgconn.CommandTag
		if suspend {
			tag, err = tx.Exec(ctx, `
				UPDATE session_task_event SET quiescence_pending = TRUE, quiescence_executor_id = $2
				WHERE sequence = $1 AND published AND quiescence_pending = FALSE
				  AND NOT EXISTS (SELECT 1 FROM session WHERE id = $3 AND dispatch_expires_at > clock_timestamp())
				  AND NOT EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = $3 AND state = 'CREATING')
				  AND NOT EXISTS (SELECT 1 FROM session_task_event WHERE history_id = $4 AND (quiescence_pending OR NOT published))
				  AND NOT EXISTS (SELECT 1 FROM session_task_event WHERE history_id = $4 AND task_id = $5 AND sequence > $1)
				  AND EXISTS (SELECT 1 FROM session_task WHERE history_id = $4 AND id = $5 AND snapshot_uri IS NULL
				      AND state IN ('TASK_STATE_INPUT_REQUIRED', 'TASK_STATE_AUTH_REQUIRED'))
			`, result.Version, result.ExecutorID, session.ID, session.HistoryID, row.TaskID)
		} else {
			tag, err = tx.Exec(ctx, `
				UPDATE session_task_event SET quiescence_executor_id = $2
				WHERE sequence = $1 AND published AND quiescence_pending
				  AND quiescence_executor_id IS NULL
				  AND NOT EXISTS (SELECT 1 FROM session WHERE id = $3 AND dispatch_expires_at > clock_timestamp())
				  AND NOT EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = $3 AND state = 'CREATING')
			`, result.Version, result.ExecutorID, session.ID)
		}
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

// expiredPauseCandidate finds the oldest waiting task whose pause finished
// longer ago than ttl and whose runtime has no recorded snapshot. The boundary
// is the task's latest event: a reply appends to the task and leaves the
// waiting state, and a superseded boundary is never a waiting task's latest.
func expiredPauseCandidate(ctx context.Context, tx pgx.Tx, ttl time.Duration, skip []string) (quiescenceCandidate, error) {
	if skip == nil {
		skip = []string{}
	}
	return queryOne(ctx, tx, `
		SELECT i.id::text AS session_id, e.task_id, e.sequence
		FROM session_task_event e
		JOIN session_task t ON t.history_id = e.history_id AND t.id = e.task_id
		JOIN session_record i ON i.history_id = e.history_id
		WHERE e.published AND e.quiescence_pending = FALSE
		  AND e.created_at <= clock_timestamp() - $1::interval
		  AND t.state IN ('TASK_STATE_INPUT_REQUIRED', 'TASK_STATE_AUTH_REQUIRED')
		  AND t.snapshot_uri IS NULL
		  AND NOT EXISTS (SELECT 1 FROM session_task_event WHERE history_id = e.history_id AND task_id = e.task_id AND sequence > e.sequence)
		  AND i.state = 'RUNTIME_STATE_READY'
		  AND i.operation = 'RUNTIME_OPERATION_NONE'
		  AND (i.dispatch_expires_at IS NULL OR i.dispatch_expires_at <= clock_timestamp())
		  AND NOT EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = i.id AND state = 'CREATING')
		  AND NOT (i.id::text = ANY($2::text[]))
		ORDER BY e.created_at, e.sequence LIMIT 1 FOR UPDATE OF i SKIP LOCKED
	`, pgx.RowToStructByName[quiescenceCandidate], ttl, skip)
}

// FinishSessionQuiescence records a claimed pause/suspend outcome and releases
// the session for new execution. Terminal tasks require the matching snapshot.
// Retries are harmless, and a stale claim cannot release another operation.
// Task state and history remain readable throughout lifecycle work.
func (c *Client) FinishSessionQuiescence(ctx context.Context, work *SessionQuiescence, snapshot *SessionTaskSnapshot) error {
	if work.State.Terminal() && (snapshot == nil || snapshot.URI == "" || snapshot.Atespace == "" || snapshot.ContentScope == "") {
		return fmt.Errorf("terminal task quiescence requires a runtime snapshot")
	}
	return c.settleSessionQuiescence(ctx, work, snapshot)
}

// ReleaseSessionQuiescence settles a claimed boundary whose runtime was neither
// suspended nor paused, for a caller that found the Actor's state after a failed
// Quiesce or Pause: the task keeps no snapshot, admission reopens, and the next
// turn takes the runtime as it is. A terminal task released this way cannot be
// checkpointed until a later turn records a snapshot.
func (c *Client) ReleaseSessionQuiescence(ctx context.Context, work *SessionQuiescence) error {
	return c.settleSessionQuiescence(ctx, work, nil)
}

func (c *Client) settleSessionQuiescence(ctx context.Context, work *SessionQuiescence, snapshot *SessionTaskSnapshot) error {
	if work == nil || work.ExecutorID == uuid.Nil {
		return fmt.Errorf("claimed task boundary is required")
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
				    snapshot_content_scope = $5, history_sequence = $6
				WHERE history_id = $1 AND id = $2
			`, session.HistoryID, work.TaskID, snapshot.Atespace, snapshot.URI, snapshot.ContentScope, work.Version); err != nil {
				return err
			}
			if err := execSQL(ctx, tx, `
				UPDATE session_task_event SET snapshot_atespace = $2, snapshot_uri = $3, snapshot_content_scope = $4
				WHERE sequence = $1
			`, work.Version, snapshot.Atespace, snapshot.URI, snapshot.ContentScope); err != nil {
				return err
			}
		}
		return execSQL(ctx, tx, `
			UPDATE session_task_event SET quiescence_pending = FALSE WHERE sequence = $1
		`, work.Version)
	})
}
