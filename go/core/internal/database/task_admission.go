package database

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/jackc/pgx/v5"
)

// ContinueAgentInstanceTask atomically admits a reply to a waiting task, archives
// the question and answer, and changes the task to SUBMITTED. The instance must be
// READY with no lifecycle operation, creating checkpoint, or other active task.
// Identical message/hash retries return the current task without dispatch, even
// after its state advances; reused IDs with different content return
// ErrIdempotencyConflict. The second result is the prior waiting task, present
// only for a new admission so the caller can restore runtime continuation state.
// Missing instances/tasks return ErrNotFound; callers authorize access.
func (c *Client) ContinueAgentInstanceTask(ctx context.Context, instanceID string, requestHash []byte, message *a2a.Message) (*a2a.Task, *a2a.Task, error) {
	if message == nil || message.ID == "" || message.TaskID == "" || len(requestHash) == 0 {
		return nil, nil, fmt.Errorf("task reply requires message ID, task ID, and request hash")
	}
	var result, waiting *a2a.Task
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		instance, err := lockAgentInstance(ctx, tx, instanceID)
		if err != nil {
			return notFoundOr(err)
		}
		if message.ContextID != instance.ContextID.String() {
			return fmt.Errorf("task reply context does not match AgentInstance")
		}
		row, err := readAgentInstanceTask(ctx, tx, instance.HistoryID, string(message.TaskID))
		if err != nil {
			return notFoundOr(err)
		}
		result, err = unmarshalAgentInstanceTask(row.Data)
		if err != nil {
			return err
		}
		hash, err := queryOne(ctx, tx, `
			SELECT request_hash FROM agent_instance_task_event
			WHERE history_id = $1 AND task_id = $2 AND message_id = $3
		`, pgx.RowTo[[]byte], instance.HistoryID, string(message.TaskID), message.ID)
		if err == nil {
			if !bytes.Equal(hash, requestHash) {
				return ErrIdempotencyConflict
			}
			return loadAgentInstanceTaskHistories(ctx, tx, instance.HistoryID, []*a2a.Task{result}, nil)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if instance.State != "AGENT_INSTANCE_STATE_READY" || instance.Operation != "AGENT_INSTANCE_OPERATION_UNSPECIFIED" {
			return fmt.Errorf("AgentInstance cannot accept a reply in state %s with operation %s: %w", instance.State, instance.Operation, ErrConflict)
		}
		if result.Status.State != a2a.TaskStateInputRequired && result.Status.State != a2a.TaskStateAuthRequired {
			return fmt.Errorf("task is not waiting for input: %w", ErrConflict)
		}
		if err := loadAgentInstanceTaskHistories(ctx, tx, instance.HistoryID, []*a2a.Task{result}, nil); err != nil {
			return err
		}
		waiting = result
		submitted := *waiting
		submitted.History = append([]*a2a.Message{}, waiting.History...)
		if question := waiting.Status.Message; question != nil {
			if question.ID == message.ID {
				return ErrIdempotencyConflict
			}
			if question.ID == "" {
				return fmt.Errorf("stored task status message has no ID")
			}
			archived := *question
			archived.TaskID, archived.ContextID = waiting.ID, waiting.ContextID
			waiting.Status.Message = &archived
			submitted.History = append(submitted.History, &archived)
		}
		submitted.History = append(submitted.History, message)
		now := time.Now().UTC()
		submitted.Status = a2a.TaskStatus{State: a2a.TaskStateSubmitted, Timestamp: &now}
		if err := storeAgentInstanceTaskEvent(ctx, tx, instance, &submitted, message, nil); err != nil {
			return err
		}
		if err := execSQL(ctx, tx, `
			UPDATE agent_instance_task_event SET request_hash = $4
			WHERE history_id = $1 AND task_id = $2 AND message_id = $3
		`, instance.HistoryID, string(message.TaskID), message.ID, requestHash); err != nil {
			return err
		}
		result = &submitted
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("continue AgentInstance task: %w", err)
	}
	return result, waiting, nil
}
