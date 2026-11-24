package database

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// IdlePurgeCandidate distinguishes expired history within a bounded scan page.
type IdlePurgeCandidate struct {
	HistoryID uuid.UUID
	Orphaned  bool
}

// ListIdlePurgeCandidates inspects one page of contexts. Session creation
// inserts a new context and its Session atomically. Idle expiry removes the
// Session, while explicit deletion retains it as a tombstone.
func (c *Client) ListIdlePurgeCandidates(ctx context.Context, afterID uuid.UUID, limit int) ([]IdlePurgeCandidate, error) {
	return queryMany(ctx, c.db, `
		SELECT page.id AS history_id, NOT EXISTS (
		    SELECT 1 FROM session s WHERE s.history_id = page.id
		) AS orphaned
		FROM (SELECT id FROM a2a_context WHERE id > $1 ORDER BY id LIMIT $2) page
		ORDER BY page.id
	`, pgx.RowToStructByName[IdlePurgeCandidate], afterID, limit)
}

// IdlePurgeCheckpoint holds the retained identity needed to remove a Tag.
type IdlePurgeCheckpoint struct {
	ID       string
	UserID   string
	Snapshot SessionTaskSnapshot
	TagUID   string
}

// BeginIdlePurgeCheckpointDeletion closes fork admission for one checkpoint
// from an expired history. A checkpoint marked for deletion but still pinned
// reports progress without returning a Tag to delete.
func (c *Client) BeginIdlePurgeCheckpointDeletion(ctx context.Context, historyID uuid.UUID) (work *IdlePurgeCheckpoint, retired bool, err error) {
	err = c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := queryOne(ctx, tx, `
			SELECT cp.id, cp.source_session_id, cp.user_id, cp.request_id, cp.head_task_id, cp.history_sequence,
			    cp.snapshot_atespace, cp.snapshot_uri, cp.snapshot_fidelity, cp.tag_uid, cp.state, cp.data,
			    cp.source_history_id, cp.prepared_revision
			FROM session_checkpoint cp
			WHERE cp.source_history_id = $1
			  AND NOT EXISTS (SELECT 1 FROM session s WHERE s.history_id = $1)
			  AND (cp.state IN ('READY', 'FAILED') OR (cp.state = 'DELETING' AND NOT EXISTS (
			      SELECT 1 FROM session s WHERE s.pinned_checkpoint_id = cp.id
			  )))
			ORDER BY cp.id LIMIT 1 FOR UPDATE
		`, pgx.RowToStructByName[sessionCheckpointRow], historyID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if err := markCheckpointDeleting(ctx, tx, row); err != nil {
			return err
		}
		// Read dependencies after acquiring the lock so a fork that won it
		// first is visible even if it committed while we were waiting.
		pinned, err := checkpointPinned(ctx, tx, row.ID)
		if err != nil {
			return err
		}
		if pinned {
			retired = row.State != "DELETING"
		} else {
			work = &IdlePurgeCheckpoint{ID: row.ID.String(), UserID: row.UserID, Snapshot: *checkpointSnapshot(row), TagUID: row.TagUID}
		}
		return nil
	})
	return work, retired, err
}

// IdlePurgeFork identifies a fork whose runtime may have stopped borrowing a Tag.
type IdlePurgeFork struct {
	ID           string
	CheckpointID uuid.UUID
	ActorUID     string
	Atespace     string
}

// ListIdlePurgeForks returns live Actor identities holding checkpoints from the
// expired history, ordered by Session ID for a bounded scan.
func (c *Client) ListIdlePurgeForks(ctx context.Context, historyID uuid.UUID, afterID string, limit int) ([]IdlePurgeFork, error) {
	return queryMany(ctx, c.db, `
		SELECT s.id::text, cp.id AS checkpoint_id, s.actor_uid, rev.actor_template_atespace AS atespace
		FROM session_checkpoint cp
		JOIN session s ON s.pinned_checkpoint_id = cp.id
		JOIN runtime_instance r ON r.id = s.id
		JOIN runtime_revision rev ON rev.revision = r.prepared_revision
		WHERE cp.source_history_id = $1 AND s.actor_uid IS NOT NULL
		  AND EXISTS (
		      SELECT 1 FROM session_task t WHERE t.history_id = s.history_id AND t.snapshot_uri <> cp.snapshot_uri
		  )
		  AND s.id > COALESCE(NULLIF($2::text, '')::uuid, '00000000-0000-0000-0000-000000000000'::uuid)
		ORDER BY s.id LIMIT $3
	`, pgx.RowToStructByName[IdlePurgeFork], historyID, afterID, limit)
}

// ReleaseIdlePurgeForkPin releases the pin if the same Actor has settled the
// independently owned snapshot observed by the worker. Runtime changes share
// the Session lock. Provenance and fork request identity remain intact.
func (c *Client) ReleaseIdlePurgeForkPin(ctx context.Context, fork IdlePurgeFork, snapshot SessionTaskSnapshot) (bool, error) {
	var released bool
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockSession(ctx, tx, fork.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if row.Operation != "RUNTIME_OPERATION_NONE" {
			return nil
		}
		tag, err := tx.Exec(ctx, `
			UPDATE session s SET pinned_checkpoint_id = NULL
			FROM session_checkpoint cp
			WHERE s.id = $1 AND s.actor_uid = $2 AND s.pinned_checkpoint_id = $3
			  AND cp.id = s.pinned_checkpoint_id AND cp.state = 'DELETING' AND cp.snapshot_uri <> $4
			  AND NOT EXISTS (SELECT 1 FROM session source WHERE source.history_id = cp.source_history_id)
			  AND EXISTS (
			      SELECT 1 FROM session_task t WHERE t.history_id = s.history_id
			      AND t.snapshot_uri = $4 AND t.snapshot_atespace = $5 AND t.snapshot_fidelity = $6
			  )
		`, row.ID, fork.ActorUID, fork.CheckpointID, snapshot.URI, snapshot.Atespace, snapshot.Fidelity)
		if err != nil {
			return err
		}
		released = tag.RowsAffected() == 1
		return nil
	})
	return released, err
}

// DeleteIdlePurgeHistory removes one bounded batch of an expired history and
// returns whether the history is gone. ErrConflict means checkpoints remain.
func (c *Client) DeleteIdlePurgeHistory(ctx context.Context, historyID uuid.UUID) (done bool, err error) {
	err = c.withTx(ctx, func(tx pgx.Tx) error {
		_, err := queryOne(ctx, tx, `
			SELECT h.id FROM a2a_context h WHERE h.id = $1
			  AND NOT EXISTS (SELECT 1 FROM session s WHERE s.history_id = h.id)
			FOR UPDATE
		`, pgx.RowTo[uuid.UUID], historyID)
		if err != nil {
			return notFoundOr(err)
		}
		hasCheckpoints, err := queryOne(ctx, tx, `
			SELECT EXISTS (SELECT 1 FROM session_checkpoint WHERE source_history_id = $1)
		`, pgx.RowTo[bool], historyID)
		if err != nil {
			return err
		}
		if hasCheckpoints {
			return ErrConflict
		}

		events, err := tx.Exec(ctx, `
			DELETE FROM session_task_event
			WHERE sequence IN (
			    SELECT sequence FROM session_task_event WHERE history_id = $1 ORDER BY sequence LIMIT 1000
			)
		`, historyID)
		if err != nil {
			return err
		}
		tasks, err := tx.Exec(ctx, `
			DELETE FROM session_task WHERE history_id = $1 AND id IN (
			    SELECT id FROM session_task WHERE history_id = $1 ORDER BY id LIMIT 1000
			)
		`, historyID)
		if err != nil {
			return err
		}
		if events.RowsAffected() == 1000 || tasks.RowsAffected() == 1000 {
			return nil
		}
		done = true
		return execSQL(ctx, tx, `DELETE FROM a2a_context WHERE id = $1`, historyID)
	})
	return done, err
}

// IdlePurgeLastEventAt returns the latest recorded activity for reporting
// blocked work. Older expirations have no surviving expiry timestamp.
func (c *Client) IdlePurgeLastEventAt(ctx context.Context, historyID uuid.UUID) (time.Time, error) {
	last, err := queryOne(ctx, c.db, `
		SELECT created_at FROM session_task_event WHERE history_id = $1 ORDER BY sequence DESC LIMIT 1
	`, pgx.RowTo[time.Time], historyID)
	return last, notFoundOr(err)
}
