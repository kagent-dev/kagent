package database

import (
	"context"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

// InstanceQuiescence grants one worker permission to pause an idle instance or
// suspend it and record its snapshot. Task publication has already completed.
type InstanceQuiescence struct {
	Instance   *apiv1alpha1.AgentInstance
	TaskID     string
	State      a2a.TaskState
	Version    int64
	ExecutorID uuid.UUID
}

// ClaimInstanceQuiescence claims an idle instance at its latest settled task
// version. A new turn supersedes unclaimed idle work; a claim blocks task writes,
// checkpoints, and explicit lifecycle operations until it finishes. Missing work
// returns ErrNotFound. Uncertain issued work is never reassigned after a timeout.
func (c *Client) ClaimInstanceQuiescence(ctx context.Context) (*InstanceQuiescence, error) {
	var result *InstanceQuiescence
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		type candidate struct {
			InstanceID string
			TaskID     string
			Sequence   int64
		}
		row, err := queryOne(ctx, tx, `
			SELECT i.id::text AS instance_id, e.task_id, e.sequence
			FROM agent_instance_task_event e JOIN agent_instance i ON i.history_id = e.history_id
			WHERE e.published AND e.quiescence_pending = TRUE
			  AND e.quiescence_executor_id IS NULL
			  AND i.state = 'AGENT_INSTANCE_STATE_READY'
			  AND i.operation = 'AGENT_INSTANCE_OPERATION_UNSPECIFIED'
			  AND (i.dispatch_expires_at IS NULL OR i.dispatch_expires_at <= clock_timestamp())
			  AND NOT EXISTS (SELECT 1 FROM agent_instance_checkpoint WHERE source_instance_id = i.id AND state = 'CREATING')
			ORDER BY e.sequence LIMIT 1 FOR UPDATE OF i SKIP LOCKED
		`, pgx.RowToStructByName[candidate])
		if err != nil {
			return notFoundOr(err)
		}
		instance, err := readAgentInstance(ctx, tx, row.InstanceID)
		if err != nil {
			return err
		}
		if instance.State != "AGENT_INSTANCE_STATE_READY" || instance.Operation != "AGENT_INSTANCE_OPERATION_UNSPECIFIED" {
			return ErrNotFound
		}
		stored, err := readAgentInstanceTask(ctx, tx, instance.HistoryID, row.TaskID)
		if err != nil {
			return err
		}
		task, err := unmarshalAgentInstanceTask(stored.Data)
		if err != nil {
			return err
		}
		value, err := toAgentInstance(instance)
		if err != nil {
			return err
		}
		result = &InstanceQuiescence{Instance: value, TaskID: row.TaskID, State: task.Status.State, Version: row.Sequence, ExecutorID: uuid.New()}
		// Recheck after locking: the candidate query's snapshot may predate a
		// new turn that committed just before we acquired the instance lock.
		tag, err := tx.Exec(ctx, `
			UPDATE agent_instance_task_event SET quiescence_executor_id = $2
			WHERE sequence = $1 AND published AND quiescence_pending
			  AND quiescence_executor_id IS NULL
			  AND NOT EXISTS (SELECT 1 FROM agent_instance WHERE id = $3 AND dispatch_expires_at > clock_timestamp())
			  AND NOT EXISTS (SELECT 1 FROM agent_instance_checkpoint WHERE source_instance_id = $3 AND state = 'CREATING')
		`, result.Version, result.ExecutorID, instance.ID)
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

// FinishInstanceQuiescence records a claimed pause/suspend outcome and releases
// the instance for new execution. Terminal tasks require the matching snapshot.
// Retries are harmless, and a stale claim cannot release another operation.
// Task state and history remain readable throughout lifecycle work.
func (c *Client) FinishInstanceQuiescence(ctx context.Context, work *InstanceQuiescence, snapshot *AgentInstanceTaskSnapshot) error {
	if work == nil || work.ExecutorID == uuid.Nil {
		return fmt.Errorf("claimed task boundary is required")
	}
	if work.State.Terminal() && (snapshot == nil || snapshot.URI == "" || snapshot.Atespace == "" || snapshot.ContentScope == "") {
		return fmt.Errorf("terminal task quiescence requires a runtime snapshot")
	}
	return c.withTx(ctx, func(tx pgx.Tx) error {
		instance, err := lockAgentInstance(ctx, tx, work.Instance.Id)
		if err != nil {
			return notFoundOr(err)
		}
		if instance.State == "AGENT_INSTANCE_STATE_DELETED" {
			return ErrNotFound
		}
		pending, err := queryOne(ctx, tx, `
			SELECT quiescence_pending FROM agent_instance_task_event
			WHERE sequence = $1 AND history_id = $2 AND task_id = $3 AND quiescence_executor_id = $4
		`, pgx.RowTo[bool], work.Version, instance.HistoryID, work.TaskID, work.ExecutorID)
		if err != nil {
			return notFoundOr(err)
		}
		if !pending {
			return nil
		}
		stored, err := readAgentInstanceTask(ctx, tx, instance.HistoryID, work.TaskID)
		if err != nil {
			return err
		}
		if stored.State != string(work.State) {
			return ErrConflict
		}
		if snapshot != nil {
			if err := execSQL(ctx, tx, `
				UPDATE agent_instance_task SET snapshot_atespace = $3, snapshot_uri = $4,
				    snapshot_content_scope = $5, history_sequence = $6
				WHERE history_id = $1 AND id = $2
			`, instance.HistoryID, work.TaskID, snapshot.Atespace, snapshot.URI, snapshot.ContentScope, work.Version); err != nil {
				return err
			}
			if err := execSQL(ctx, tx, `
				UPDATE agent_instance_task_event SET snapshot_atespace = $2, snapshot_uri = $3, snapshot_content_scope = $4
				WHERE sequence = $1
			`, work.Version, snapshot.Atespace, snapshot.URI, snapshot.ContentScope); err != nil {
				return err
			}
		}
		return execSQL(ctx, tx, `
			UPDATE agent_instance_task_event SET quiescence_pending = FALSE WHERE sequence = $1
		`, work.Version)
	})
}
