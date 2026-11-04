package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

// CheckpointOperation contains the durable inputs for creation or deletion.
// Failure on a CREATING checkpoint means tag cleanup must finish before the
// reservation can become FAILED and release source-session admission.
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
	var operation *CheckpointOperation
	// Do not let a canceled autocommit UPDATE acquire a claim after its caller
	// has already tried to release it. A lost COMMIT reply still holds the row
	// lock until the transaction ends, so release serializes behind that outcome.
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := queryOne(ctx, tx, `
		UPDATE session_checkpoint SET executor_id = $3,
		    executor_expires_at = clock_timestamp() + $4 * interval '1 millisecond'
		WHERE id = $1 AND user_id = $2 AND state IN ('CREATING', 'DELETING')
		    AND (executor_id IS NULL OR executor_expires_at <= clock_timestamp())
		RETURNING id, source_session_id, user_id, request_id, head_task_id, history_sequence,
		    snapshot_atespace, snapshot_uri, snapshot_fidelity, tag_uid, state, data,
		    source_history_id, prepared_revision
		`, pgx.RowToStructByName[sessionCheckpointRow], id, owner, executor, time.Until(deadline).Milliseconds()+1)
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
			UPDATE session_checkpoint SET executor_id = NULL, executor_expires_at = NULL
			WHERE id = $1 AND executor_id = $2
		`, id, executor)
		return err
	})
}

// checkCheckpointExecutor requires the caller to hold the checkpoint row lock.
// Direct finalization is permitted only when no executor owns the row;
// claimed transitions reject expired and superseded attempts.
func checkCheckpointExecutor(ctx context.Context, tx pgx.Tx, id string, executor uuid.UUID) error {
	var expected *uuid.UUID
	if executor != uuid.Nil {
		expected = &executor
	}
	matches, err := queryOne(ctx, tx, `
		SELECT executor_id IS NOT DISTINCT FROM $2::uuid
		    AND ($2::uuid IS NULL OR executor_expires_at > clock_timestamp())
		FROM session_checkpoint WHERE id = $1
	`, pgx.RowTo[bool], id, expected)
	if err != nil {
		return err
	}
	if !matches {
		return ErrConflict
	}
	return nil
}

// GetCheckpointOperation returns an owner's checkpoint in any lifecycle phase.
// Missing and other-owner records return ErrNotFound. It does not authorize
// callers; services compose permissions before using this internal lifecycle read.
func (c *Client) GetCheckpointOperation(ctx context.Context, id, owner string) (*CheckpointOperation, error) {
	row, err := readCheckpoint(ctx, c.db, id, owner, nil)
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
		  AND ($1::uuid IS NULL OR id > $1::uuid)
		ORDER BY id LIMIT $2
	`, pgx.RowToStructByName[sessionCheckpointRow], after, limit)
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

// BeginCheckpointCleanup durably records creation compensation and its tag UID.
// The checkpoint remains CREATING, retaining history/revision references and
// blocking source mutations until external cleanup finishes. Identical retries
// succeed; a conflicting outcome or a terminal checkpoint returns ErrConflict.
// A non-nil executor must own the attempt; nil requires unclaimed work.
func (c *Client) BeginCheckpointCleanup(ctx context.Context, id string, executor uuid.UUID, tagUID, failure string) error {
	if failure == "" {
		return fmt.Errorf("checkpoint cleanup requires a failure")
	}
	return c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockCheckpoint(ctx, tx, id, true, "", nil)
		if err != nil {
			return notFoundOr(err)
		}
		if err := checkCheckpointExecutor(ctx, tx, id, executor); err != nil {
			return err
		}
		checkpoint, err := toSessionCheckpoint(row)
		if err != nil {
			return err
		}
		if row.State != "CREATING" {
			return ErrConflict
		}
		if checkpoint.Failure != nil {
			if row.TagUID == tagUID && checkpoint.Failure.Message == failure {
				return nil
			}
			return ErrConflict
		}
		checkpoint.Failure = &apiv1.Failure{Reason: "SnapshotTagFailed", Message: failure}
		data, err := proto.Marshal(checkpoint)
		if err != nil {
			return fmt.Errorf("encode checkpoint cleanup: %w", err)
		}
		_, err = tx.Exec(ctx, `UPDATE session_checkpoint SET tag_uid = $2, data = $3
		    WHERE id = $1 AND state = 'CREATING'`, row.ID, tagUID, data)
		return err
	})
}
