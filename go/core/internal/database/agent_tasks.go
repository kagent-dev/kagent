package database

import (
	"context"
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/jackc/pgx/v5"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
)

// AgentInstanceForTask resolves a globally unique public task ID. The caller must
// authorize the resulting instance before returning data or invoking its runtime.
func (c *Client) AgentInstanceForTask(ctx context.Context, taskID string) (string, error) {
	id, err := queryOne(ctx, c.db, `
		SELECT i.id::text FROM agent_instance i
		JOIN agent_instance_task t ON t.history_id = i.history_id
		WHERE t.id = $1 AND i.state <> 'AGENT_INSTANCE_STATE_DELETED'
	`, pgx.RowTo[string], taskID)
	return id, notFoundOr(err)
}

// ListAgentTasks applies authorization before pagination: instanceIDs contains
// only the conversations the caller may read. Task IDs break ties between copies
// of the same historical position in different forks.
func (c *Client) ListAgentTasks(ctx context.Context, instanceIDs []string, afterID string, state a2a.TaskState, statusTimestampAfter *time.Time, limit int, historyLength *int) ([]*a2a.Task, int, error) {
	total, err := queryOne(ctx, c.db, `
		SELECT COUNT(*) FROM agent_instance_task t
		JOIN agent_instance i ON i.history_id = t.history_id
		WHERE i.id = ANY($1::uuid[]) AND i.state <> 'AGENT_INSTANCE_STATE_DELETED'
		  AND ($2::text = '' OR t.state = $2)
		  AND ($3::timestamptz IS NULL OR t.status_timestamp > $3)
	`, pgx.RowTo[int64], instanceIDs, string(state), statusTimestampAfter)
	if err != nil {
		return nil, 0, fmt.Errorf("count Agent tasks: %w", err)
	}
	rows, err := queryMany(ctx, c.db, `
		SELECT t.history_id, t.id, t.state, t.status_timestamp, t.data, t.created_at,
		    t.snapshot_atespace, t.snapshot_uri, t.snapshot_content_scope,
		    t.history_sequence, t.position FROM agent_instance_task t
		JOIN agent_instance i ON i.history_id = t.history_id
		WHERE i.id = ANY($1::uuid[]) AND i.state <> 'AGENT_INSTANCE_STATE_DELETED'
		  AND ($2::text = '' OR (t.position, t.id) > (
		      SELECT cursor.position, cursor.id FROM agent_instance_task cursor
		      JOIN agent_instance owner ON owner.history_id = cursor.history_id
		      WHERE cursor.id = $2 AND owner.id = ANY($1::uuid[])
		  ))
		  AND ($3::text = '' OR t.state = $3)
		  AND ($4::timestamptz IS NULL OR t.status_timestamp > $4)
		ORDER BY t.position, t.id LIMIT $5
	`, pgx.RowToStructByName[agentInstanceTaskRow], instanceIDs, afterID, string(state), statusTimestampAfter, int32(limit))
	if err != nil {
		return nil, 0, fmt.Errorf("list Agent tasks: %w", err)
	}
	tasks := make([]*a2a.Task, 0, len(rows))
	for _, row := range rows {
		task, err := unmarshalAgentInstanceTask(row.Data)
		if err != nil {
			return nil, 0, err
		}
		apia2a.SetTaskCreatedAt(task, row.CreatedAt)
		if err := loadAgentInstanceTaskHistories(ctx, c.db, row.HistoryID, []*a2a.Task{task}, historyLength, false); err != nil {
			return nil, 0, err
		}
		tasks = append(tasks, task)
	}
	return tasks, int(total), nil
}
