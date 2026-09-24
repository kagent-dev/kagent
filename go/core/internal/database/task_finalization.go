package database

import (
	"context"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"google.golang.org/protobuf/proto"
)

// TaskFinalization is a claimed runtime boundary. The executor may issue one
// pause/suspend operation; the claim never expires while its outcome is unknown.
type TaskFinalization struct {
	Instance   *apiv1alpha1.AgentInstance
	TaskID     string
	State      a2a.TaskState
	Version    int64
	ExecutorID uuid.UUID
}

// ClaimTaskFinalization claims the oldest unissued runtime boundary. A worker
// must perform runtime I/O after this transaction commits. Missing work returns
// ErrNotFound; issued work is never reassigned after a timeout or worker restart.
func (c *Client) ClaimTaskFinalization(ctx context.Context) (*TaskFinalization, error) {
	var result *TaskFinalization
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		type candidate struct {
			InstanceID string
			TaskID     string
			Sequence   int64
			Data       []byte
		}
		row, err := queryOne(ctx, tx, `
			SELECT i.id::text AS instance_id, e.task_id, e.sequence, e.data
			FROM agent_instance_task_event e JOIN agent_instance i ON i.history_id = e.history_id
			WHERE NOT e.published AND e.expected_version IS NOT NULL AND e.runtime_settled
			  AND e.finalization_executor_id IS NULL
			  AND i.state = 'AGENT_INSTANCE_STATE_READY'
			  AND i.operation = 'AGENT_INSTANCE_OPERATION_UNSPECIFIED'
			ORDER BY e.sequence LIMIT 1 FOR UPDATE OF i SKIP LOCKED
		`, pgx.RowToStructByName[candidate])
		if err != nil {
			return notFoundOr(err)
		}
		instance, err := readAgentInstance(ctx, tx, row.InstanceID)
		if err != nil {
			return err
		}
		stored, err := readAgentInstanceTask(ctx, tx, instance.HistoryID, row.TaskID)
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
		value, err := toAgentInstance(instance)
		if err != nil {
			return err
		}
		result = &TaskFinalization{Instance: value, TaskID: row.TaskID, State: task.Status.State, Version: row.Sequence, ExecutorID: uuid.New()}
		return execSQL(ctx, tx, `
			UPDATE agent_instance_task_event SET finalization_executor_id = $2 WHERE sequence = $1
		`, result.Version, result.ExecutorID)
	})
	return result, err
}

// PublishTaskBoundary makes a claimed event and its archived history visible
// together after runtime quiescence. Terminal tasks require the exact snapshot
// produced for this boundary. A delayed or duplicate publication cannot affect
// another task or release a different pending boundary.
func (c *Client) PublishTaskBoundary(ctx context.Context, work *TaskFinalization, snapshot *AgentInstanceTaskSnapshot) error {
	if work == nil || work.ExecutorID == uuid.Nil {
		return fmt.Errorf("claimed task boundary is required")
	}
	if work.State.Terminal() && (snapshot == nil || snapshot.URI == "" || snapshot.Atespace == "" || snapshot.ContentScope == "") {
		return fmt.Errorf("terminal task publication requires a runtime snapshot")
	}
	return c.withTx(ctx, func(tx pgx.Tx) error {
		instance, err := lockAgentInstance(ctx, tx, work.Instance.Id)
		if err != nil {
			return notFoundOr(err)
		}
		if instance.State == "AGENT_INSTANCE_STATE_DELETED" {
			return ErrNotFound
		}
		type boundary struct {
			Data            []byte
			ExpectedVersion int64
			Published       bool
		}
		row, err := queryOne(ctx, tx, `
			SELECT data, expected_version, published FROM agent_instance_task_event
			WHERE sequence = $1 AND history_id = $2 AND task_id = $3 AND finalization_executor_id = $4
		`, pgx.RowToStructByName[boundary], work.Version, instance.HistoryID, work.TaskID, work.ExecutorID)
		if err != nil {
			return notFoundOr(err)
		}
		if row.Published {
			return nil
		}
		stored, err := readAgentInstanceTask(ctx, tx, instance.HistoryID, work.TaskID)
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
		if task.Status.State != work.State {
			return ErrConflict
		}
		// Preserve unknown protobuf fields when publishing a newer runtime's task.
		data, err := proto.Marshal(wire)
		if err != nil {
			return err
		}
		if _, err := saveTaskProjection(ctx, tx, instance.HistoryID, work.TaskID, string(task.Status.State), task.Status.Timestamp, data); err != nil {
			return err
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
			UPDATE agent_instance_task_event SET published = TRUE
			WHERE history_id = $1 AND task_id = $2 AND sequence > $3 AND sequence <= $4
		`, instance.HistoryID, work.TaskID, row.ExpectedVersion, work.Version)
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

// SettleAgentInstanceTask confirms native cleanup for one immutable saved
// boundary. Retries are harmless even after publication or a later turn; they
// can never settle another version or authorize another execution.
func (c *Client) SettleAgentInstanceTask(ctx context.Context, instanceID, taskID string, version int64) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		instance, err := lockAgentInstance(ctx, tx, instanceID)
		if err != nil {
			return notFoundOr(err)
		}
		if instance.State == "AGENT_INSTANCE_STATE_DELETED" {
			return ErrNotFound
		}
		tag, err := tx.Exec(ctx, `
			UPDATE agent_instance_task_event SET runtime_settled = TRUE
			WHERE history_id = $1 AND task_id = $2 AND sequence = $3
			  AND expected_version IS NOT NULL AND (NOT published OR runtime_settled)
		`, instance.HistoryID, taskID, version)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
		return nil
	})
}
