package database

import (
	"context"
	"fmt"

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
func (c *Client) BeginCheckpointCleanup(ctx context.Context, id, tagUID, failure string) error {
	if failure == "" {
		return fmt.Errorf("checkpoint cleanup requires a failure")
	}
	return c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := lockCheckpoint(ctx, tx, id, true, "", nil)
		if err != nil {
			return notFoundOr(err)
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
