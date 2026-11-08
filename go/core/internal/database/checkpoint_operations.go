package database

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// CheckpointOperation contains the durable inputs for creation or deletion.
type CheckpointOperation struct {
	Checkpoint *apiv1.Checkpoint
	Creator    string
	Snapshot   *SessionTaskSnapshot
	TagUID     string
}

// ClaimCheckpointOperation grants one bounded attempt exclusive execution of an
// owner's pending checkpoint and returns its current phase, not a caller's earlier
// observation. The caller must establish its attempt deadline before claiming and
// use that context for every backend call. Missing, terminal, or actively claimed
// work returns ErrConflict. No database lock survives this call.
func (c *Client) ClaimCheckpointOperation(ctx context.Context, id, owner string, executor uuid.UUID) (*CheckpointOperation, error) {
	deadline, ok := ctx.Deadline()
	if !ok || executor == uuid.Nil || time.Until(deadline) > RuntimeOperationTimeout {
		return nil, ErrFailedPrecondition
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	legacyEligible := false
	if identifier, parseErr := uuid.Parse(id); parseErr == nil && identifier.Version() == 7 {
		seconds, nanos := identifier.Time().UnixTime()
		legacyEligible = time.Since(time.Unix(seconds, nanos)) > RuntimeOperationTimeout
	}
	var operation *CheckpointOperation
	// Do not let a canceled autocommit UPDATE acquire a claim after its caller
	// has already tried to release it. A lost COMMIT reply still holds the row
	// lock until the transaction ends, so release serializes behind that outcome.
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := queryOne(ctx, tx, `
		UPDATE session_checkpoint SET executor_id = $3,
		    executor_expires_at = clock_timestamp() + $4 * interval '1 millisecond'
		WHERE id = $1 AND user_id = $2 AND state IN ('CREATING', 'DELETING')
		    AND ((executor_id IS NULL AND $5::boolean) OR executor_expires_at <= clock_timestamp())
		RETURNING id, source_session_id, user_id, request_id, head_task_id, history_sequence,
		    snapshot_atespace, snapshot_uri, snapshot_fidelity, tag_uid, state, data,
		    source_history_id, prepared_revision
		`, pgx.RowToStructByName[sessionCheckpointRow], id, owner, executor, time.Until(deadline).Milliseconds()+1, legacyEligible)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		operation, err = checkpointOperation(row)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("claim checkpoint operation: %w", err)
	}
	return operation, nil
}

// ReleaseCheckpointOperation releases only this executor's attempt. It does not
// abandon durable work, and a delayed release cannot revoke a replacement claim.
// Missing rows and other executors are successful no-ops.
func (c *Client) ReleaseCheckpointOperation(ctx context.Context, id string, executor uuid.UUID) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		// Lock by ID before testing the executor: an acquisition's COMMIT can
		// still be in flight, with the old committed executor invisible to a
		// conditional UPDATE. Wait for that transaction before deciding to no-op.
		_, err := lockCheckpoint(ctx, tx, id, true, "", nil)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `
			UPDATE session_checkpoint SET executor_expires_at = clock_timestamp()
			WHERE id = $1 AND executor_id = $2
		`, id, executor)
		return err
	})
}

// GetCheckpointOperation returns an owner's checkpoint in any lifecycle phase.
// Missing and other-owner records return ErrNotFound. It does not authorize
// callers; services compose permissions before using this internal lifecycle read.
func (c *Client) GetCheckpointOperation(ctx context.Context, id, owner string) (*CheckpointOperation, error) {
	row, err := queryOne(ctx, c.db, `
        SELECT id, source_session_id, user_id, request_id, head_task_id, history_sequence,
            snapshot_atespace, snapshot_uri, snapshot_fidelity, tag_uid, state, data,
            source_history_id, prepared_revision
        FROM session_checkpoint WHERE id = $1 AND user_id = $2
    `, pgx.RowToStructByName[sessionCheckpointRow], id, owner)
	if err != nil {
		return nil, fmt.Errorf("get checkpoint operation: %w", notFoundOr(err))
	}
	return checkpointOperation(row)
}

// ListPendingCheckpoints returns a bounded ID-ordered page of durable creation
// and deletion work across owners. Only the controller's checkpoint worker uses
// this unscoped query; age never establishes whether a tag copy is still live.
func (c *Client) ListPendingCheckpoints(ctx context.Context, afterID string, limit int) ([]*CheckpointOperation, error) {
	var after *string
	if afterID != "" {
		after = &afterID
	}
	rows, err := queryMany(ctx, c.db, `
		SELECT id, source_session_id, user_id, request_id, head_task_id, history_sequence,
		    snapshot_atespace, snapshot_uri, snapshot_fidelity, tag_uid, state, data,
		    source_history_id, prepared_revision
		FROM session_checkpoint
		WHERE state IN ('CREATING', 'DELETING')
          AND (executor_expires_at <= statement_timestamp() OR (executor_id IS NULL AND id < $3::uuid))
		  AND ($1::uuid IS NULL OR id > $1::uuid)
		ORDER BY id LIMIT $2
	`, pgx.RowToStructByName[sessionCheckpointRow], after, limit, legacyCheckpointCutoff(time.Now()))
	if err != nil {
		return nil, fmt.Errorf("list pending checkpoints: %w", err)
	}
	result := make([]*CheckpointOperation, 0, len(rows))
	for _, row := range rows {
		op, err := checkpointOperation(row)
		if err != nil {
			return nil, err
		}
		result = append(result, op)
	}
	return result, nil
}

func checkpointOperation(row sessionCheckpointRow) (*CheckpointOperation, error) {
	checkpoint, err := toSessionCheckpoint(row)
	if err != nil {
		return nil, err
	}
	return &CheckpointOperation{Checkpoint: checkpoint, Creator: row.UserID,
		Snapshot: checkpointSnapshot(row), TagUID: row.TagUID}, nil
}

// Legacy rows have no claim. The UUID grace avoids taking freshly reserved work
// during rollout, but is not a fence: the first upgrade must drain old writers.
func legacyCheckpointCutoff(now time.Time) uuid.UUID {
	var id uuid.UUID
	binary.BigEndian.PutUint64(id[:8], uint64(now.Add(-RuntimeOperationTimeout).UnixMilli())<<16)
	return id
}
