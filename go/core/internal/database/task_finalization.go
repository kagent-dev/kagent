package database

import (
	"context"
	"errors"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
)

// SettleSessionTask publishes a saved terminal/waiting task and its history
// atomically after native cleanup. It does not wait for runtime pause or snapshots.
// Retries cannot republish an old state over a later turn. Callers authenticate
// runtime authority; a missing or deleted session returns ErrNotFound.
func (c *Client) SettleSessionTask(ctx context.Context, sessionID, taskID string, version int64) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, sessionID)
		if err != nil {
			return notFoundOr(err)
		}
		if session.State == "RUNTIME_STATE_DELETED" {
			return ErrNotFound
		}
		type boundary struct {
			Data            []byte
			ExpectedVersion int64
			Published       bool
		}
		row, err := queryOne(ctx, tx, `
			SELECT data, expected_version, published FROM session_task_event
			WHERE history_id = $1 AND task_id = $2 AND sequence = $3
			  AND expected_version IS NOT NULL AND quiescence_pending IS NOT NULL
		`, pgx.RowToStructByName[boundary], session.HistoryID, taskID, version)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrConflict
		}
		if err != nil {
			return err
		}
		if row.Published {
			return nil
		}
		stored, err := readSessionTask(ctx, tx, session.HistoryID, taskID)
		if err != nil {
			return err
		}
		wire, err := applyStoredBoundary(stored.Data, row.Data)
		if err != nil {
			return err
		}
		task, err := pbconv.FromProtoTask(wire)
		if err != nil {
			return err
		}
		data, err := proto.Marshal(wire)
		if err != nil {
			return err
		}
		if _, err := saveTaskProjection(ctx, tx, session.HistoryID, taskID, string(task.Status.State), task.Status.Timestamp, data); err != nil {
			return err
		}
		return execSQL(ctx, tx, `
			UPDATE session_task_event SET published = TRUE
			WHERE history_id = $1 AND task_id = $2 AND sequence > $3 AND sequence <= $4
		`, session.HistoryID, taskID, row.ExpectedVersion, version)
	})
}

func applyStoredBoundary(data, eventData []byte) (*a2apb.Task, error) {
	task, event := &a2apb.Task{}, &a2apb.StreamResponse{}
	if err := proto.Unmarshal(data, task); err != nil {
		return nil, err
	}
	if err := proto.Unmarshal(eventData, event); err != nil {
		return nil, err
	}
	return applyTaskEvent(task, event)
}
