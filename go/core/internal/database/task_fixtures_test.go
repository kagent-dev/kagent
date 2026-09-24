package database

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// InterruptActiveAgentInstanceTask atomically marks taskID failed and records its
// interruption message and event, only if it is still the instance's active task. It
// returns false if no active task matches. Callers authorize access and stop runtime work
// separately.
func (c *Client) InterruptActiveAgentInstanceTask(ctx context.Context, instanceID, taskID string) (bool, error) {
	interruptedTask := false
	instance, err := readAgentInstance(ctx, c.db, instanceID)
	if err != nil {
		return false, fmt.Errorf("get AgentInstance history: %w", notFoundOr(err))
	}
	err = c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := queryOne(ctx, tx, `
			SELECT history_id, id, state, status_timestamp, data, created_at, updated_at, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence, position FROM
			    agent_instance_task
			WHERE history_id = $1
			  AND state NOT IN (
			      'TASK_STATE_COMPLETED',
			      'TASK_STATE_CANCELED',
			      'TASK_STATE_FAILED',
			      'TASK_STATE_REJECTED',
			      'TASK_STATE_INPUT_REQUIRED',
			      'TASK_STATE_AUTH_REQUIRED'
			  )
			FOR UPDATE
		`, pgx.RowToStructByName[agentInstanceTaskRow], instance.HistoryID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("lock active AgentInstance task: %w", err)
		}
		if row.ID != taskID {
			return nil
		}
		task := &a2apb.Task{}
		if err := proto.Unmarshal(row.Data, task); err != nil {
			return fmt.Errorf("decode interrupted task: %w", err)
		}
		if _, err := pbconv.FromProtoTask(task); err != nil {
			return err
		}
		interrupted := &a2apb.Message{
			MessageId: uuid.NewString(), TaskId: task.Id, ContextId: task.ContextId, Role: a2apb.Role_ROLE_AGENT,
			Parts: []*a2apb.Part{{Content: &a2apb.Part_Text{Text: taskInterruptedMessage}}},
		}
		now := time.Now()
		status := proto.Clone(task.Status).(*a2apb.TaskStatus)
		status.State = a2apb.TaskState_TASK_STATE_FAILED
		status.Message = interrupted
		status.Timestamp = timestamppb.New(now)
		messages := append(task.History, interrupted)
		event := &a2apb.StreamResponse{Payload: &a2apb.StreamResponse_StatusUpdate{StatusUpdate: &a2apb.TaskStatusUpdateEvent{
			TaskId: task.Id, ContextId: task.ContextId, Status: status,
		}}}
		task, err = applyTaskEvent(task, event)
		if err != nil {
			return err
		}
		task.History = nil
		data, err := proto.Marshal(task)
		if err != nil {
			return err
		}
		if _, err := saveTaskProjection(ctx, tx, instance.HistoryID, task.Id, string(a2a.TaskStateFailed), &now, data); err != nil {
			return fmt.Errorf("interrupt AgentInstance task %s: %w", task.Id, err)
		}
		if _, err := storeProtoTaskMessages(ctx, tx, instance.HistoryID, task.Id, task.ContextId, messages); err != nil {
			return fmt.Errorf("record AgentInstance task interruption: %w", err)
		}
		eventData, err := proto.Marshal(event)
		if err != nil {
			return err
		}
		if _, err := insertTaskEvent(ctx, tx, taskEventWrite{
			HistoryID: instance.HistoryID,
			TaskID:    &task.Id,
			Data:      eventData,
		}); err != nil {
			return fmt.Errorf("record task interruption status: %w", err)
		}
		interruptedTask = true
		return nil
	})
	if err != nil {
		return false, err
	}
	return interruptedTask, nil
}

// StoreAgentInstanceTaskEvent atomically saves the task state, archived messages, replay
// event, and optional snapshot boundary. It rejects inconsistent task/context identities
// and updates blocked by checkpoint creation. Snapshot references must already exist;
// callers authorize access and perform external snapshot work.
func (c *Client) StoreAgentInstanceTaskEvent(ctx context.Context, instanceID string, task *a2a.Task, event a2a.Event, snapshot *AgentInstanceTaskSnapshot) error {
	if task == nil || event == nil {
		return fmt.Errorf("task and event are required")
	}
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		instance, err := lockAgentInstance(ctx, tx, instanceID)
		if err != nil {
			return notFoundOr(err)
		}
		return storeAgentInstanceTaskEvent(ctx, tx, instance, task, event, snapshot, false)
	})
	if err != nil {
		return fmt.Errorf("store AgentInstance task update: %w", err)
	}
	return nil
}

// taskInterruptedMessage explains a task terminated because its runtime no
// longer has an active execution for it.
const taskInterruptedMessage = "The turn was interrupted before it completed, and the process running it is no longer reporting progress."

// GetActiveAgentInstanceTask returns the instance's current active task, or ErrNotFound if
// none exists. Completed, canceled, failed, rejected, input-required, and auth-required
// tasks are not active. Callers authorize instance access.
func (c *Client) GetActiveAgentInstanceTask(ctx context.Context, instanceID string) (*a2a.Task, error) {
	type activeTaskRow struct {
		HistoryID uuid.UUID
		Data      []byte
	}
	row, err := queryOne(ctx, c.db, `
		SELECT t.history_id, t.data
		FROM agent_instance_task t
		JOIN agent_instance i ON i.history_id = t.history_id
		WHERE i.id = $1 AND i.state <> 'AGENT_INSTANCE_STATE_DELETED'
		  AND t.state NOT IN (
		      'TASK_STATE_COMPLETED',
		      'TASK_STATE_CANCELED',
		      'TASK_STATE_FAILED',
		      'TASK_STATE_REJECTED',
		      'TASK_STATE_INPUT_REQUIRED',
		      'TASK_STATE_AUTH_REQUIRED'
		  )
	`, pgx.RowToStructByName[activeTaskRow], instanceID)
	if err != nil {
		return nil, fmt.Errorf("get active AgentInstance task: %w", notFoundOr(err))
	}
	task, err := unmarshalAgentInstanceTask(row.Data)
	if err == nil {
		err = loadAgentInstanceTaskHistories(ctx, c.db, row.HistoryID, []*a2a.Task{task}, nil, false)
	}
	return task, err
}

// taskMutationHash supplies a deterministic storage mutation digest to fixtures.
func taskMutationHash(value string) []byte {
	hash := sha256.Sum256([]byte(value))
	return hash[:]
}

func waitingTaskFixture(t *testing.T, client *Client) (*apiv1alpha1.AgentInstance, *a2a.Task) {
	t.Helper()
	instance, _, err := client.CreateAgentInstance(t.Context(), newAgentInstanceRequest(uuid.NewString(), "assistant", "kagent", ""), uuid.NewString())
	require.NoError(t, err)
	instance, err = markAgentInstanceReady(t.Context(), client, instance.Id, "agent.example")
	require.NoError(t, err)
	task := newAgentInstanceTask("task", "initial")
	task.ContextID = instance.ContextId
	_, err = client.CreateRuntimeTask(t.Context(), instance.Id, taskMutationHash("initial request"), task)
	require.NoError(t, err)
	task.Status = a2a.TaskStatus{State: a2a.TaskStateInputRequired, Message: a2a.NewMessage(a2a.MessageRoleAgent, a2a.NewTextPart("Which database?"))}
	require.NoError(t, client.StoreAgentInstanceTaskEvent(t.Context(), instance.Id, task, task, nil))
	return instance, task
}
