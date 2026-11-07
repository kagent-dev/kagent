package database

import (
	"context"
	"errors"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
)

// SettledTask is a boundary SettleSessionTask published. Previous is the task
// it replaced, and Sender the dispatch sender of the execution that reached it.
type SettledTask struct {
	Previous *a2a.Task
	Task     *a2a.Task
	Sender   *TurnSender
}

// SettleSessionTask publishes a saved terminal/waiting task and its history
// atomically after native cleanup. It does not wait for runtime pause or snapshots.
// Retries cannot republish an old state over a later turn. Callers authenticate
// runtime authority; a missing or deleted session returns ErrNotFound. It
// returns the published boundary, or nil when the boundary was already published.
func (c *Client) SettleSessionTask(ctx context.Context, sessionID, taskID string, version int64) (*SettledTask, error) {
	var settled *SettledTask
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, sessionID)
		if err != nil {
			return notFoundOr(err)
		}
		if session.State == "RUNTIME_STATE_DELETED" {
			return ErrNotFound
		}
		settled, err = settleSessionTask(ctx, tx, session, taskID, version)
		return err
	})
	return settled, err
}

// publishSavedSessionResults publishes every result the runtime saved whose
// cleanup acknowledgement was lost, so ending the runtime preserves them.
func publishSavedSessionResults(ctx context.Context, tx pgx.Tx, session sessionRow) error {
	type boundary struct {
		TaskID   string
		Sequence int64
	}
	boundaries, err := queryMany(ctx, tx, `
		SELECT task_id, sequence FROM session_task_event
		WHERE history_id = $1 AND NOT published
		    AND expected_version IS NOT NULL AND quiescence_pending IS NOT NULL
		ORDER BY sequence
	`, pgx.RowToStructByName[boundary], session.HistoryID)
	if err != nil {
		return err
	}
	for _, saved := range boundaries {
		if _, err := settleSessionTask(ctx, tx, session, saved.TaskID, saved.Sequence); err != nil {
			return err
		}
	}
	return nil
}

// retireSessionQuiescence drops idle-work markers for a runtime that no longer
// needs a later suspension.
func retireSessionQuiescence(ctx context.Context, tx pgx.Tx, session sessionRow) error {
	return execSQL(ctx, tx, `UPDATE session_task_event SET quiescence_pending = FALSE WHERE history_id = $1 AND quiescence_pending`, session.HistoryID)
}

// settleSessionTask publishes one saved boundary under the session lock. Runtime
// acknowledgement, confirmed actor deletion, or a confirmed crash must establish
// cleanup first. It returns nil when the boundary was already published.
func settleSessionTask(ctx context.Context, tx pgx.Tx, session sessionRow, taskID string, version int64) (*SettledTask, error) {
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
		return nil, ErrConflict
	}
	if err != nil {
		return nil, err
	}
	if row.Published {
		return nil, nil
	}
	stored, err := readSessionTask(ctx, tx, session.HistoryID, taskID)
	if err != nil {
		return nil, err
	}
	wire, err := applyStoredBoundary(stored.Data, row.Data)
	if err != nil {
		return nil, err
	}
	task, err := pbconv.FromProtoTask(wire)
	if err != nil {
		return nil, err
	}
	previous, err := unmarshalSessionTask(stored.Data)
	if err != nil {
		return nil, err
	}
	sender, err := queryOne(ctx, tx, `
		SELECT turn_sender FROM session WHERE id = $1
	`, pgx.RowTo[*TurnSender], session.ID)
	if err != nil {
		return nil, err
	}
	data, err := proto.Marshal(wire)
	if err != nil {
		return nil, err
	}
	if _, err := saveTaskProjection(ctx, tx, session.HistoryID, taskID, string(task.Status.State), task.Status.Timestamp, data); err != nil {
		return nil, err
	}
	if err := execSQL(ctx, tx, `
		UPDATE session_task_event SET published = TRUE
		WHERE history_id = $1 AND task_id = $2 AND sequence > $3 AND sequence <= $4
	`, session.HistoryID, taskID, row.ExpectedVersion, version); err != nil {
		return nil, err
	}
	if err := publishPushBoundary(ctx, tx, session.HistoryID, taskID, version, task); err != nil {
		return nil, err
	}
	return &SettledTask{Previous: previous, Task: task, Sender: sender}, nil
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
