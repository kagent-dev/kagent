package database

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	corea2a "github.com/kagent-dev/kagent/go/core/internal/a2a"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// TaskAdmission identifies the durable execution turn created for an accepted message.
// Retries return the receipt's original turn even when the task has since advanced.
type TaskAdmission struct {
	Task   *a2a.Task
	TurnID uuid.UUID
}

type admittedTaskTurn struct {
	id      uuid.UUID
	request []byte
}

// AdmitAgentInstanceTask atomically stores a task, its creation event, retry receipt,
// and complete normalized execution request.
func (c *Client) AdmitAgentInstanceTask(ctx context.Context, instanceID string, requestHash []byte, request *a2a.SendMessageRequest, task *a2a.Task) (*TaskAdmission, error) {
	requestData, err := marshalSendMessageRequest(request)
	if err != nil {
		return nil, fmt.Errorf("encode admitted AgentInstance task request: %w", err)
	}
	admission, _, err := c.createAgentInstanceTask(ctx, instanceID, requestHash, task, &admittedTaskTurn{id: uuid.New(), request: requestData})
	return admission, err
}

// CreateAgentInstanceTask atomically stores a task, its creation event, and initial
// messages for a READY instance with no lifecycle operation. It requires an initial
// message and matching context. Reusing the initial message ID returns the stored task if
// the request hash matches, or ErrIdempotencyConflict otherwise. An occupied active-task
// slot or checkpoint creation blocks new tasks with ErrConflict. The
// boolean reports a new reservation; callers authorize access and invoke the runtime
// separately.
func (c *Client) CreateAgentInstanceTask(ctx context.Context, instanceID string, requestHash []byte, task *a2a.Task) (*a2a.Task, bool, error) {
	admission, created, err := c.createAgentInstanceTask(ctx, instanceID, requestHash, task, nil)
	if admission == nil {
		return nil, created, err
	}
	return admission.Task, created, err
}

func (c *Client) createAgentInstanceTask(ctx context.Context, instanceID string, requestHash []byte, task *a2a.Task, turn *admittedTaskTurn) (*TaskAdmission, bool, error) {
	if task == nil || len(task.History) == 0 || task.History[0] == nil || task.History[0].ID == "" {
		return nil, false, fmt.Errorf("AgentInstance task requires an initial message")
	}
	message := task.History[0]
	initial, creation, err := taskTransition(nil, task, task)
	if err != nil {
		return nil, false, err
	}
	taskData, err := proto.Marshal(initial)
	if err != nil {
		return nil, false, err
	}
	creationData, err := proto.Marshal(creation)
	if err != nil {
		return nil, false, err
	}

	result := task
	turnID := uuid.Nil
	created := false
	var historyID uuid.UUID
	err = c.withTx(ctx, func(tx pgx.Tx) error {
		instance, err := lockAgentInstance(ctx, tx, instanceID)
		if err != nil {
			return fmt.Errorf("lock AgentInstance %s: %w", instanceID, err)
		}
		if instance.State == "AGENT_INSTANCE_STATE_DELETED" {
			return ErrNotFound
		}
		historyID = instance.HistoryID
		if task.ContextID != instance.ContextID.String() {
			return fmt.Errorf("task context does not match AgentInstance")
		}
		existing, err := queryOne(ctx, tx, `
			SELECT history_id, id, state, status_timestamp, data, created_at, updated_at, initial_message_id,
			    request_hash, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence,
			    turn_id, turn_phase, turn_request, turn_previous_task, turn_owner_id, turn_owner_expires_at,
			    turn_cancel_requested, position
			FROM agent_instance_task WHERE history_id = $1 AND initial_message_id = $2
		`, pgx.RowToStructByName[agentInstanceTaskRow], historyID, message.ID)
		if err == nil {
			if !bytes.Equal(existing.RequestHash, requestHash) {
				return ErrIdempotencyConflict
			}
			result, err = unmarshalAgentInstanceTask(existing.Data)
			if err != nil {
				return err
			}
			receiptTurnID, receiptErr := queryOne(ctx, tx, `
				SELECT turn_id FROM agent_instance_task_event
				WHERE history_id = $1 AND task_id = $2 AND task_position IS NOT NULL
			`, pgx.RowTo[*uuid.UUID], historyID, existing.ID)
			if receiptErr != nil && !errors.Is(receiptErr, pgx.ErrNoRows) {
				return fmt.Errorf("get AgentInstance task admission receipt: %w", receiptErr)
			}
			if receiptTurnID != nil {
				turnID = *receiptTurnID
			}
			return loadAgentInstanceTaskHistories(ctx, tx, historyID, []*a2a.Task{result}, nil)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("get AgentInstance task for message %s: %w", message.ID, err)
		}
		if instance.State != "AGENT_INSTANCE_STATE_READY" || instance.Operation != "AGENT_INSTANCE_OPERATION_UNSPECIFIED" {
			return fmt.Errorf("AgentInstance %s cannot accept a task in state %s with operation %s: %w", instanceID, instance.State, instance.Operation, ErrConflict)
		}
		var admittedTurnID *uuid.UUID
		var turnPhase *string
		var turnRequest []byte
		if turn != nil {
			admittedTurnID, turnID, turnRequest = &turn.id, turn.id, turn.request
			phase := "ADMITTED"
			turnPhase = &phase
		}
		row, err := queryOne(ctx, tx, `
			INSERT INTO agent_instance_task (
			    history_id, id, state, status_timestamp, data, initial_message_id, request_hash,
			    turn_id, turn_phase, turn_request
			)
			SELECT $1, $2, $3, $4, $5, $6, $7, $9, $10, $11
			WHERE NOT EXISTS (
			    SELECT 1 FROM agent_instance_checkpoint
			    WHERE source_instance_id = $8 AND state = 'CREATING'
			)
			RETURNING history_id, id, state, status_timestamp, data, created_at, updated_at, initial_message_id,
			    request_hash, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence,
			    turn_id, turn_phase, turn_request, turn_previous_task, turn_owner_id, turn_owner_expires_at,
			    turn_cancel_requested, position
		`,
			pgx.RowToStructByName[agentInstanceTaskRow], historyID, string(task.ID), string(task.Status.State),
			task.Status.Timestamp, taskData, &message.ID, requestHash, instance.ID,
			admittedTurnID, turnPhase, turnRequest,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("AgentInstance %s has a checkpoint being created: %w", instanceID, ErrConflict)
		}
		if err != nil {
			if isActiveTaskConflict(err) {
				return fmt.Errorf("AgentInstance %s already has an active task: %w", instanceID, ErrConflict)
			}
			return fmt.Errorf("create AgentInstance task %s: %w", task.ID, err)
		}
		created = true
		if _, err := insertTaskEvent(ctx, tx, taskEventWrite{
			HistoryID:        historyID,
			TaskID:           &row.ID,
			Data:             creationData,
			TaskPosition:     &row.Position,
			InitialMessageID: row.InitialMessageID,
			RequestHash:      row.RequestHash,
			TurnID:           admittedTurnID,
			CreatedAt:        &row.CreatedAt,
		}); err != nil {
			return fmt.Errorf("record task creation: %w", err)
		}
		_, err = storeAgentInstanceTaskMessages(ctx, tx, historyID, string(task.ID), task.ContextID, task.History)
		return err
	})
	if err != nil {
		return nil, false, fmt.Errorf("create AgentInstance task: %w", err)
	}
	return &TaskAdmission{Task: result, TurnID: turnID}, created, nil
}

// TaskContinuation describes the same task before and after reply admission.
type TaskContinuation struct {
	// Current is the persisted task after admission, or its latest state on a retry.
	Current *a2a.Task
	// Previous is the waiting task before a newly admitted reply. It is nil on
	// retries, which must not dispatch the reply again.
	Previous *a2a.Task
}

// AdmitAgentInstanceTaskContinuation atomically installs a new turn for an accepted
// reply and persists the exact waiting task used to restore runtime state.
func (c *Client) AdmitAgentInstanceTaskContinuation(ctx context.Context, instanceID string, requestHash []byte, request *a2a.SendMessageRequest) (*TaskAdmission, error) {
	if request == nil {
		return nil, fmt.Errorf("task reply requires a request")
	}
	requestData, err := marshalSendMessageRequest(request)
	if err != nil {
		return nil, fmt.Errorf("encode admitted AgentInstance task reply: %w", err)
	}
	_, admission, err := c.continueAgentInstanceTask(ctx, instanceID, requestHash, request.Message, &admittedTaskTurn{id: uuid.New(), request: requestData})
	return admission, err
}

// ContinueAgentInstanceTask atomically admits a reply to a waiting task, archives
// the question and answer, and changes the task to SUBMITTED. The instance must be
// READY with no lifecycle operation, creating checkpoint, or other active task.
// Identical message/hash retries return the current task without dispatch, even
// after its state advances; reused IDs with different content return
// ErrIdempotencyConflict. Invalid human-input replies return ErrFailedPrecondition.
// Previous is present only for a new admission, allowing the caller to restore
// runtime continuation state. Retries return Current without another dispatch.
// Missing instances/tasks return ErrNotFound; callers authorize access.
func (c *Client) ContinueAgentInstanceTask(ctx context.Context, instanceID string, requestHash []byte, message *a2a.Message) (*TaskContinuation, error) {
	continuation, _, err := c.continueAgentInstanceTask(ctx, instanceID, requestHash, message, nil)
	return continuation, err
}

func (c *Client) continueAgentInstanceTask(ctx context.Context, instanceID string, requestHash []byte, message *a2a.Message, turn *admittedTaskTurn) (*TaskContinuation, *TaskAdmission, error) {
	if message == nil || message.ID == "" || message.TaskID == "" || len(requestHash) == 0 {
		return nil, nil, fmt.Errorf("task reply requires message ID, task ID, and request hash")
	}
	var result, waiting *a2a.Task
	turnID := uuid.Nil
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		instance, err := lockAgentInstance(ctx, tx, instanceID)
		if err != nil {
			return notFoundOr(err)
		}
		if instance.State == "AGENT_INSTANCE_STATE_DELETED" {
			return ErrNotFound
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
		type receiptRow struct {
			RequestHash []byte
			TurnID      *uuid.UUID
		}
		receipt, err := queryOne(ctx, tx, `
			SELECT request_hash, turn_id FROM agent_instance_task_event
			WHERE history_id = $1 AND task_id = $2 AND message_id = $3
		`, pgx.RowToStructByName[receiptRow], instance.HistoryID, string(message.TaskID), message.ID)
		if err == nil {
			if !bytes.Equal(receipt.RequestHash, requestHash) {
				return ErrIdempotencyConflict
			}
			if receipt.TurnID != nil {
				turnID = *receipt.TurnID
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
		if pending, parseErr := apia2a.ParseToolApprovalRequest(result.Status.Message); parseErr != nil {
			return fmt.Errorf("stored tool approval request is invalid: %w", parseErr)
		} else if pending != nil {
			response, responseErr := apia2a.ParseToolApprovalResponse(message)
			if responseErr != nil || apia2a.ValidateToolApprovalResponse(pending, response) != nil {
				return fmt.Errorf("tool approval response does not match the pending request: %w", ErrFailedPrecondition)
			}
		} else if pending, parseErr := apia2a.ParseAskUserRequest(result.Status.Message); parseErr != nil {
			return fmt.Errorf("stored ask-user request is invalid: %w", parseErr)
		} else if pending != nil && pending.Nested == nil {
			// Nested ask-user correlation remains owned by the ADK adapter. Native
			// Harness requests use the top-level ID and can be rejected before the
			// paused Actor is resumed.
			response, responseErr := apia2a.ParseAskUserResponse(message)
			if responseErr != nil || apia2a.ValidateAskUserResponse(pending, response) != nil {
				return fmt.Errorf("ask-user response does not match the pending request: %w", ErrFailedPrecondition)
			}
		}
		if err := loadAgentInstanceTaskHistories(ctx, tx, instance.HistoryID, []*a2a.Task{result}, nil); err != nil {
			return err
		}
		waiting = result
		var previousTaskData []byte
		if turn != nil {
			previousTaskData, err = marshalTask(waiting)
			if err != nil {
				return fmt.Errorf("encode previous AgentInstance task: %w", err)
			}
		}
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
		var admittedTurnID *uuid.UUID
		if turn != nil {
			admittedTurnID, turnID = &turn.id, turn.id
			if err := execSQL(ctx, tx, `
				UPDATE agent_instance_task SET turn_id = $3, turn_phase = 'ADMITTED', turn_request = $4,
				    turn_previous_task = $5, turn_owner_id = NULL, turn_owner_expires_at = NULL,
				    turn_cancel_requested = FALSE
				WHERE history_id = $1 AND id = $2
			`, instance.HistoryID, string(message.TaskID), turn.id, turn.request, previousTaskData); err != nil {
				return err
			}
		}
		if err := execSQL(ctx, tx, `
			UPDATE agent_instance_task_event SET request_hash = $4, turn_id = $5
			WHERE history_id = $1 AND task_id = $2 AND message_id = $3
		`, instance.HistoryID, string(message.TaskID), message.ID, requestHash, admittedTurnID); err != nil {
			return err
		}
		result = &submitted
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("continue AgentInstance task: %w", err)
	}
	return &TaskContinuation{Current: result, Previous: waiting}, &TaskAdmission{Task: result, TurnID: turnID}, nil
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
		err = loadAgentInstanceTaskHistories(ctx, c.db, row.HistoryID, []*a2a.Task{task}, nil)
	}
	return task, err
}

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
			SELECT history_id, id, state, status_timestamp, data, created_at, updated_at, initial_message_id,
			    request_hash, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence,
			    turn_id, turn_phase, turn_request, turn_previous_task, turn_owner_id, turn_owner_expires_at,
			    turn_cancel_requested, position FROM
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
		task, err = corea2a.ApplyTaskEvent(task, event)
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
		return storeAgentInstanceTaskEvent(ctx, tx, instance, task, event, snapshot)
	})
	if err != nil {
		return fmt.Errorf("store AgentInstance task update: %w", err)
	}
	return nil
}

// storeAgentInstanceTaskEvent persists a task transition, its messages, and optional
// snapshot in the caller's transaction. The caller must hold the instance row lock;
// checkpoint creation blocks the write. Continuation admission validates the waiting
// state and retry identity before invoking this shared persistence operation.
func storeAgentInstanceTaskEvent(ctx context.Context, tx pgx.Tx, instance agentInstanceRow, task *a2a.Task, event a2a.Event, snapshot *AgentInstanceTaskSnapshot) error {
	if instance.State == "AGENT_INSTANCE_STATE_DELETED" {
		return ErrNotFound
	}
	historyID := instance.HistoryID
	if event.TaskInfo().ContextID != instance.ContextID.String() || task.ContextID != instance.ContextID.String() {
		return fmt.Errorf("task event context does not match AgentInstance")
	}
	creating, err := queryOne(ctx, tx, `
		SELECT EXISTS (SELECT 1 FROM agent_instance_checkpoint WHERE source_instance_id = $1 AND state = 'CREATING')
	`, pgx.RowTo[bool], instance.ID)
	if err != nil {
		return err
	}
	if creating {
		return fmt.Errorf("AgentInstance %s has a checkpoint being created: %w", instance.ID, ErrConflict)
	}
	var sequence int64
	var stored *a2apb.Task
	if row, err := queryOne(ctx, tx, `
		SELECT history_id, id, state, status_timestamp, data, created_at, updated_at, initial_message_id,
		    request_hash, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence,
		    turn_id, turn_phase, turn_request, turn_previous_task, turn_owner_id, turn_owner_expires_at,
		    turn_cancel_requested, position FROM
		    agent_instance_task WHERE history_id = $1 AND id = $2 FOR UPDATE
	`, pgx.RowToStructByName[agentInstanceTaskRow], historyID, string(task.ID)); err == nil {
		stored = &a2apb.Task{}
		if err := proto.Unmarshal(row.Data, stored); err != nil {
			return fmt.Errorf("decode stored task: %w", err)
		}
		if _, err := pbconv.FromProtoTask(stored); err != nil {
			return err
		}
		messages := stored.History
		// Replies and status updates archive the old status message before replacing it.
		// Use the stored protobuf so its nested fields survive in history too.
		switch event.(type) {
		case *a2a.Message, *a2a.TaskStatusUpdateEvent:
			if message := stored.Status.Message; message != nil {
				message.TaskId, message.ContextId = string(task.ID), task.ContextID
				messages = append(messages, message)
			}
		}
		if len(messages) > 0 {
			sequence, err = storeProtoTaskMessages(ctx, tx, historyID, string(task.ID), task.ContextID, messages)
			if err != nil {
				return fmt.Errorf("archive AgentInstance task history: %w", err)
			}
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("get AgentInstance task %s: %w", task.ID, err)
	}
	newTask := stored == nil
	stored, durable, err := taskTransition(stored, task, event)
	if err != nil {
		return err
	}
	data, err := proto.Marshal(stored)
	if err != nil {
		return err
	}
	taskRow, err := saveTaskProjection(ctx, tx, historyID, string(task.ID), string(task.Status.State), task.Status.Timestamp, data)
	if err != nil {
		if isActiveTaskConflict(err) {
			return fmt.Errorf("AgentInstance %s already has an active task: %w", instance.ID, ErrConflict)
		}
		return fmt.Errorf("store AgentInstance task %s: %w", task.ID, err)
	}
	if newTask {
		data, err := proto.Marshal(durable)
		if err != nil {
			return err
		}
		sequence, err = insertTaskEvent(ctx, tx, taskEventWrite{
			HistoryID:        historyID,
			TaskID:           &taskRow.ID,
			Data:             data,
			TaskPosition:     &taskRow.Position,
			InitialMessageID: taskRow.InitialMessageID,
			RequestHash:      taskRow.RequestHash,
			CreatedAt:        &taskRow.CreatedAt,
		})
		if err != nil {
			return fmt.Errorf("record task creation: %w", err)
		}
	}

	messages := agentInstanceTaskEventMessages(task, event)
	if len(messages) > 0 {
		var err error
		sequence, err = storeAgentInstanceTaskMessages(ctx, tx, historyID, string(event.TaskInfo().TaskID), instance.ContextID.String(), messages)
		if err != nil {
			return fmt.Errorf("store AgentInstance task history: %w", err)
		}
	}
	if !newTask || snapshot != nil {
		eventData, err := proto.Marshal(durable)
		if err != nil {
			return err
		}
		insert := taskEventWrite{
			HistoryID: historyID, TaskID: strPtrIfNotEmpty(string(event.TaskInfo().TaskID)), Data: eventData,
		}
		if snapshot != nil {
			insert.SnapshotAtespace, insert.SnapshotURI, insert.SnapshotContentScope = &snapshot.Atespace, &snapshot.URI, &snapshot.ContentScope
		}
		sequence, err = insertTaskEvent(ctx, tx, insert)
		if err != nil {
			return fmt.Errorf("store AgentInstance task event: %w", err)
		}
	}

	if snapshot != nil {
		if sequence == 0 {
			return fmt.Errorf("snapshot has no history boundary")
		}
		if err := execSQL(ctx, tx, `
			UPDATE agent_instance_task SET
			    snapshot_atespace = $3,
			    snapshot_uri = $4,
			    snapshot_content_scope = $5,
			    history_sequence = $6
			WHERE history_id = $1 AND id = $2
		`, historyID, string(task.ID), &snapshot.Atespace, &snapshot.URI, &snapshot.ContentScope, &sequence); err != nil {
			return fmt.Errorf("store AgentInstance task snapshot: %w", err)
		}
	}
	return nil
}

// TaskObservation pairs a task with its last committed event in the same snapshot.
// Sequence belongs to this instance history and task; it must not be reused across forks.
type TaskObservation struct {
	Task     *a2a.Task
	Sequence int64
}

// TaskEvent is one canonical task update or archived message from the existing log.
// Consumers fold these records into the observed task; this is not a wire-event archive.
// Positions increase but need not be consecutive.
type TaskEvent struct {
	Sequence int64
	Event    a2a.Event
}

// GetAgentInstanceTask returns a task with up to historyLength latest archived messages
// from one database snapshot, without locking writers. Nil or negative historyLength
// loads all history; zero skips it. Missing/deleted instances or tasks return ErrNotFound;
// malformed durable data returns an error. Callers authorize instance access.
func (c *Client) GetAgentInstanceTask(ctx context.Context, instanceID, taskID string, historyLength *int) (*a2a.Task, error) {
	observation, err := c.GetAgentInstanceTaskObservation(ctx, instanceID, taskID, historyLength)
	if err != nil {
		return nil, err
	}
	return observation.Task, nil
}

// GetAgentInstanceTaskObservation reads task state, selected history, and the last
// event position in one statement. History limits match GetAgentInstanceTask.
// Missing/deleted instances or tasks return ErrNotFound. Callers authorize access.
func (c *Client) GetAgentInstanceTaskObservation(ctx context.Context, instanceID, taskID string, historyLength *int) (*TaskObservation, error) {
	type taskRow struct {
		Sequence  int64
		Data      []byte
		Messages  [][]byte
		ContextID string
	}
	row, err := queryOne(ctx, c.db, `
		SELECT t.data, i.context_id::text AS context_id,
		    COALESCE((SELECT MAX(e.sequence) FROM agent_instance_task_event e
		        WHERE e.history_id = t.history_id AND e.task_id = t.id), 0) AS sequence,
		    ARRAY(SELECT messages.data FROM (
		        SELECT e.sequence, e.data FROM agent_instance_task_event e
		        WHERE e.history_id = t.history_id AND e.task_id = t.id AND e.message_id IS NOT NULL
		        ORDER BY e.sequence DESC
		        LIMIT CASE WHEN $3::int < 0 THEN NULL ELSE $3 END
		    ) messages ORDER BY messages.sequence) AS messages
		FROM agent_instance_task t
		JOIN agent_instance i ON i.history_id = t.history_id
		WHERE i.id = $1 AND i.state <> 'AGENT_INSTANCE_STATE_DELETED' AND t.id = $2
	`, pgx.RowToStructByName[taskRow], instanceID, taskID, historyLength)
	if err != nil {
		return nil, fmt.Errorf("get AgentInstance task %s: %w", taskID, notFoundOr(err))
	}
	task, err := unmarshalAgentInstanceTask(row.Data)
	if err != nil {
		return nil, err
	}
	if string(task.ID) != taskID || task.ContextID != row.ContextID {
		return nil, fmt.Errorf("AgentInstance task has inconsistent identity")
	}
	if len(row.Messages) > 0 || (historyLength != nil && *historyLength == 0) {
		task.History = make([]*a2a.Message, 0, len(row.Messages))
	} else if historyLength != nil && *historyLength > 0 && *historyLength < len(task.History) {
		task.History = task.History[len(task.History)-*historyLength:]
	}
	for _, data := range row.Messages {
		event, err := unmarshalAgentInstanceTaskEvent(data)
		if err != nil {
			return nil, err
		}
		message, ok := event.(*a2a.Message)
		if !ok || message.TaskID != task.ID || message.ContextID != task.ContextID {
			return nil, fmt.Errorf("AgentInstance task history contains an inconsistent message")
		}
		task.History = append(task.History, message)
	}
	return &TaskObservation{Task: task, Sequence: row.Sequence}, nil
}

// ListAgentInstanceTaskEvents reads at most limit committed records strictly after
// afterSequence from the task's existing log. It includes archived messages and
// canonical task updates; callers restore history and state rather than treating
// these records as the original runtime responses. Start from a TaskObservation
// and consume each position once to avoid repeating artifact appends.
// Empty results mean the caller has caught up; missing/deleted instances or tasks
// return ErrNotFound. Callers authorize access and arrange wakeups. No runtime is contacted.
func (c *Client) ListAgentInstanceTaskEvents(ctx context.Context, instanceID, taskID string, afterSequence int64, limit int) ([]TaskEvent, error) {
	if afterSequence < 0 || limit < 1 || limit > 1000 {
		return nil, fmt.Errorf("event position must be nonnegative and limit must be between 1 and 1000")
	}
	type eventRow struct {
		Sequence  *int64
		ContextID string
		Data      []byte
		MessageID *string
	}
	rows, err := queryMany(ctx, c.db, `
		SELECT e.sequence, i.context_id::text AS context_id, e.data, e.message_id
		FROM agent_instance i
		JOIN agent_instance_task t ON t.history_id = i.history_id AND t.id = $2
		LEFT JOIN LATERAL (
		    SELECT sequence, data, message_id FROM agent_instance_task_event
		    WHERE history_id = t.history_id AND task_id = t.id AND sequence > $3
		    ORDER BY sequence LIMIT $4
		) e ON true
		WHERE i.id = $1 AND i.state <> 'AGENT_INSTANCE_STATE_DELETED'
		ORDER BY e.sequence
	`, pgx.RowToStructByName[eventRow], instanceID, taskID, afterSequence, limit)
	if err != nil {
		return nil, fmt.Errorf("list AgentInstance task events: %w", err)
	}
	if len(rows) == 0 {
		return nil, ErrNotFound
	}
	events := make([]TaskEvent, 0, len(rows))
	for _, row := range rows {
		if row.Sequence == nil {
			continue // Existing task with no later records.
		}
		event, err := unmarshalAgentInstanceTaskEvent(row.Data)
		if err != nil {
			return nil, err
		}
		if info := event.TaskInfo(); string(info.TaskID) != taskID || info.ContextID != row.ContextID {
			return nil, fmt.Errorf("event %d has inconsistent task identity", *row.Sequence)
		}
		if message, ok := event.(*a2a.Message); ok {
			if row.MessageID == nil || message.ID != *row.MessageID {
				return nil, fmt.Errorf("event %d has inconsistent message identity", *row.Sequence)
			}
		} else if row.MessageID != nil {
			return nil, fmt.Errorf("event %d has an invalid message index", *row.Sequence)
		}
		events = append(events, TaskEvent{Sequence: *row.Sequence, Event: event})
	}
	return events, nil
}

// ListAgentInstanceTasks returns tasks with archived messages in immutable creation order
// after afterID, with optional state and exclusive status-timestamp filters. The total
// counts all matching tasks before pagination. State, totals, and history are read in
// one read-only snapshot. History limits have the same semantics as GetAgentInstanceTask.
// Callers authorize instance access.
func (c *Client) ListAgentInstanceTasks(ctx context.Context, instanceID, afterID string, state a2a.TaskState, statusTimestampAfter *time.Time, limit int, historyLength *int) ([]*a2a.Task, int, error) {
	var tasks []*a2a.Task
	var total int
	err := pgx.BeginTxFunc(ctx, c.db, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		instance, err := readAgentInstance(ctx, tx, instanceID)
		if err != nil {
			return fmt.Errorf("get AgentInstance history: %w", notFoundOr(err))
		}
		count, err := queryOne(ctx, tx, `
			SELECT COUNT(*) FROM agent_instance_task
			WHERE history_id = $1
			  AND ($2::text = '' OR state = $2)
			  AND ($3::timestamptz IS NULL
			       OR status_timestamp > $3)
		`, pgx.RowTo[int64], instance.HistoryID, string(state), statusTimestampAfter)
		if err != nil {
			return fmt.Errorf("count AgentInstance tasks: %w", err)
		}
		rows, err := queryMany(ctx, tx, `
			SELECT t.history_id, t.id, t.state, t.status_timestamp, t.data, t.created_at, t.updated_at,
			    t.initial_message_id, t.request_hash, t.snapshot_atespace, t.snapshot_uri, t.snapshot_content_scope,
			    t.history_sequence, t.turn_id, t.turn_phase, t.turn_request, t.turn_previous_task,
			    t.turn_owner_id, t.turn_owner_expires_at, t.turn_cancel_requested, t.position
			FROM agent_instance_task t
			WHERE t.history_id = $1
			  AND ($2::text = '' OR t.position > (
			      SELECT cursor.position FROM agent_instance_task cursor
			      WHERE cursor.history_id = $1 AND cursor.id = $2
			  ))
			  AND ($3::text = '' OR t.state = $3)
			  AND ($4::timestamptz IS NULL
			       OR t.status_timestamp > $4)
			ORDER BY t.position
			LIMIT $5
		`,
			pgx.RowToStructByName[agentInstanceTaskRow], instance.HistoryID, afterID, string(state), statusTimestampAfter,
			int32(limit),
		)
		if err != nil {
			return fmt.Errorf("list AgentInstance tasks: %w", err)
		}
		tasks = make([]*a2a.Task, 0, len(rows))
		for _, row := range rows {
			task, err := unmarshalAgentInstanceTask(row.Data)
			if err != nil {
				return fmt.Errorf("decode AgentInstance task %s: %w", row.ID, err)
			}
			tasks = append(tasks, task)
		}
		if err := loadAgentInstanceTaskHistories(ctx, tx, instance.HistoryID, tasks, historyLength); err != nil {
			return err
		}
		total = int(count)
		return nil
	})
	if err != nil {
		return nil, 0, fmt.Errorf("list AgentInstance tasks: %w", err)
	}
	return tasks, total, nil
}

// unmarshalAgentInstanceTaskEvent decodes a stored A2A event, returning an error for
// malformed or unsupported payloads.
func unmarshalAgentInstanceTaskEvent(data []byte) (a2a.Event, error) {
	var pb a2apb.StreamResponse
	if err := proto.Unmarshal(data, &pb); err != nil {
		return nil, fmt.Errorf("unmarshal AgentInstance task event: %w", err)
	}
	event, err := pbconv.FromProtoStreamResponse(&pb)
	if err != nil {
		return nil, fmt.Errorf("convert AgentInstance task event: %w", err)
	}
	return event, nil
}

// agentInstanceTaskEventMessages selects the messages contributed by an event: the message
// itself, a task's history, or the latest history message for a status update. Other
// events contribute no messages.
func agentInstanceTaskEventMessages(task *a2a.Task, event a2a.Event) []*a2a.Message {
	switch event := event.(type) {
	case *a2a.Message:
		return []*a2a.Message{event}
	case *a2a.Task:
		return event.History
	case *a2a.TaskStatusUpdateEvent:
		if task != nil && len(task.History) > 0 {
			return task.History[len(task.History)-1:]
		}
	}
	return nil
}

// storeAgentInstanceTaskMessages converts and archives messages with valid IDs, returning
// the last message's event sequence or zero for an empty list. The caller supplies a
// transaction when these writes must commit atomically with task state.
func storeAgentInstanceTaskMessages(ctx context.Context, db dbExecutor, historyID uuid.UUID, taskID, contextID string, messages []*a2a.Message) (int64, error) {
	converted := make([]*a2apb.Message, 0, len(messages))
	for _, message := range messages {
		if message == nil || message.ID == "" {
			return 0, fmt.Errorf("AgentInstance task history contains a message without an ID")
		}
		event, err := pbconv.ToProtoStreamResponse(message)
		if err != nil {
			return 0, err
		}
		converted = append(converted, event.GetMessage())
	}
	return storeProtoTaskMessages(ctx, db, historyID, taskID, contextID, converted)
}

// storeProtoTaskMessages archives messages without changing the inputs or dropping unknown
// protobuf fields. It fills missing task/context IDs and rejects conflicting identities.
// Duplicate message IDs retain their original event sequence; an empty list returns zero.
// Callers own the transaction.
func storeProtoTaskMessages(ctx context.Context, db dbExecutor, historyID uuid.UUID, taskID, contextID string, messages []*a2apb.Message) (int64, error) {
	var sequence int64
	for _, message := range messages {
		if message.GetMessageId() == "" {
			return 0, fmt.Errorf("AgentInstance task history contains a message without an ID")
		}
		message = proto.Clone(message).(*a2apb.Message)
		if (message.TaskId != "" && message.TaskId != taskID) || (message.ContextId != "" && message.ContextId != contextID) {
			return 0, fmt.Errorf("history message changes task identity")
		}
		message.TaskId, message.ContextId = taskID, contextID
		data, err := proto.Marshal(&a2apb.StreamResponse{Payload: &a2apb.StreamResponse_Message{Message: message}})
		if err != nil {
			return 0, err
		}
		sequence, err = insertTaskEvent(ctx, db, taskEventWrite{
			HistoryID: historyID,
			TaskID:    &taskID,
			MessageID: &message.MessageId,
			Data:      data,
		})
		if err != nil {
			return 0, err
		}
	}
	return sequence, nil
}

// loadAgentInstanceTaskHistories attaches archived messages to the supplied tasks in event
// order, limited to the latest historyLength per task. Zero clears history without a
// query; nil or negative loads it all. Tasks without archived messages otherwise retain
// their inline history, subject to the same limit. Malformed messages return errors.
func loadAgentInstanceTaskHistories(ctx context.Context, db dbExecutor, historyID uuid.UUID, tasks []*a2a.Task, historyLength *int) error {
	if len(tasks) == 0 {
		return nil
	}
	if historyLength != nil && *historyLength == 0 {
		for _, task := range tasks {
			task.History = []*a2a.Message{}
		}
		return nil
	}
	ids := make([]string, len(tasks))
	byID := make(map[string]*a2a.Task, len(tasks))
	for index, task := range tasks {
		ids[index] = string(task.ID)
		byID[string(task.ID)] = task
		if historyLength != nil && *historyLength > 0 && *historyLength < len(task.History) {
			task.History = task.History[len(task.History)-*historyLength:]
		}
	}
	rows, err := readTaskMessages(ctx, db, historyID, ids, historyLength)
	if err != nil {
		return fmt.Errorf("list AgentInstance task history: %w", err)
	}
	histories := make(map[string][]*a2a.Message, len(tasks))
	for _, row := range rows {
		event, err := unmarshalAgentInstanceTaskEvent(row.Data)
		if err != nil {
			return err
		}
		message, ok := event.(*a2a.Message)
		if !ok {
			return fmt.Errorf("AgentInstance task history event is %T, not a message", event)
		}
		histories[row.TaskID] = append(histories[row.TaskID], message)
	}
	for taskID, history := range histories {
		if task := byID[taskID]; task != nil {
			task.History = history
		}
	}
	return nil
}

// isActiveTaskConflict reports whether a PostgreSQL error identifies the constraint
// enforcing one active task per conversation.
func isActiveTaskConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.ConstraintName == "agent_instance_one_active_task_idx"
}

// unmarshalAgentInstanceTask decodes a stored A2A task, returning an error for malformed
// or unsupported payloads.
func unmarshalAgentInstanceTask(data []byte) (*a2a.Task, error) {
	var pb a2apb.Task
	if err := proto.Unmarshal(data, &pb); err != nil {
		return nil, fmt.Errorf("unmarshal AgentInstance task: %w", err)
	}
	task, err := pbconv.FromProtoTask(&pb)
	if err != nil {
		return nil, fmt.Errorf("convert AgentInstance task: %w", err)
	}
	return task, nil
}

type agentInstanceTaskRow struct {
	HistoryID            uuid.UUID
	ID                   string
	State                string
	StatusTimestamp      *time.Time
	Data                 []byte
	CreatedAt            time.Time
	UpdatedAt            time.Time
	InitialMessageID     *string
	RequestHash          []byte
	SnapshotAtespace     *string
	SnapshotURI          *string
	SnapshotContentScope *string
	HistorySequence      *int64
	TurnID               *uuid.UUID
	TurnPhase            *taskTurnPhase
	TurnRequest          []byte
	TurnPreviousTask     []byte
	TurnOwnerID          *uuid.UUID
	TurnOwnerExpiresAt   *time.Time
	TurnCancelRequested  bool
	Position             int64
}

type agentInstanceTaskEventRow struct {
	Sequence             int64
	HistoryID            uuid.UUID
	TaskID               *string
	Data                 []byte
	CreatedAt            time.Time
	MessageID            *string
	TaskPosition         *int64
	InitialMessageID     *string
	RequestHash          []byte
	TurnID               *uuid.UUID
	SnapshotAtespace     *string
	SnapshotURI          *string
	SnapshotContentScope *string
}

type taskEventWrite struct {
	HistoryID            uuid.UUID
	TaskID               *string
	MessageID            *string
	Data                 []byte
	SnapshotAtespace     *string
	SnapshotURI          *string
	SnapshotContentScope *string
	TaskPosition         *int64
	InitialMessageID     *string
	RequestHash          []byte
	TurnID               *uuid.UUID
	CreatedAt            *time.Time
}

type taskHistoryRow struct {
	TaskID string
	Data   []byte
}

// insertTaskEvent appends an event and returns its sequence. Repeated message identities
// within the same history and task return the original sequence without replacing content;
// events without a message ID append independently. CreatedAt defaults to database time.
// Callers serialize writes within a history and supply the transaction when persisting
// related task changes.
func insertTaskEvent(ctx context.Context, db dbExecutor, event taskEventWrite) (int64, error) {
	return queryOne(ctx, db, `
		WITH inserted AS (
		    INSERT INTO agent_instance_task_event
		        (history_id, task_id, message_id, data, snapshot_atespace, snapshot_uri, snapshot_content_scope,
		         task_position, initial_message_id, request_hash, turn_id, created_at)
		    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, COALESCE($12::timestamptz, NOW()))
		    ON CONFLICT (history_id, task_id, message_id)
		        WHERE message_id IS NOT NULL
		    DO NOTHING
		    RETURNING sequence
		)
		SELECT sequence FROM inserted
		UNION ALL
		SELECT sequence FROM agent_instance_task_event
		WHERE history_id = $1 AND task_id IS NOT DISTINCT FROM $2 AND message_id = $3
		LIMIT 1
	`,
		pgx.RowTo[int64], event.HistoryID, event.TaskID, event.MessageID, event.Data, event.SnapshotAtespace,
		event.SnapshotURI, event.SnapshotContentScope, event.TaskPosition, event.InitialMessageID, event.RequestHash,
		event.TurnID, event.CreatedAt,
	)
}

// readAgentInstanceTask reads a task's stored projection without decoding or attaching
// history. Missing tasks return pgx.ErrNoRows; callers authorize access.
func readAgentInstanceTask(ctx context.Context, db dbExecutor, historyID uuid.UUID, taskID string) (agentInstanceTaskRow, error) {
	return queryOne(ctx, db, `
		SELECT history_id, id, state, status_timestamp, data, created_at, updated_at, initial_message_id,
		    request_hash, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence,
		    turn_id, turn_phase, turn_request, turn_previous_task, turn_owner_id, turn_owner_expires_at,
		    turn_cancel_requested, position FROM
		    agent_instance_task
		WHERE history_id = $1 AND id = $2
	`, pgx.RowToStructByName[agentInstanceTaskRow], historyID, taskID)
}

// saveTaskProjection inserts or replaces current task state while preserving existing
// creation, retry, and snapshot metadata. Callers validate the transition and persist its
// events in the same transaction.
func saveTaskProjection(ctx context.Context, db dbExecutor, historyID uuid.UUID, taskID, state string, statusTimestamp *time.Time, data []byte) (agentInstanceTaskRow, error) {
	return queryOne(ctx, db, `
		INSERT INTO agent_instance_task (history_id, id, state, status_timestamp, data)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (history_id, id) DO UPDATE SET
		    state = EXCLUDED.state,
		    status_timestamp = EXCLUDED.status_timestamp,
		    data = EXCLUDED.data,
		    updated_at = NOW()
		RETURNING history_id, id, state, status_timestamp, data, created_at, updated_at, initial_message_id,
		    request_hash, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence,
		    turn_id, turn_phase, turn_request, turn_previous_task, turn_owner_id, turn_owner_expires_at,
		    turn_cancel_requested, position
	`, pgx.RowToStructByName[agentInstanceTaskRow], historyID, taskID, state, statusTimestamp, data)
}

// readTaskMessages returns the latest historyLength archived messages per requested task
// in event order. Nil or negative limits load all messages. Callers authorize the history
// and decode the returned payloads.
func readTaskMessages(ctx context.Context, db dbExecutor, historyID uuid.UUID, taskIDs []string, historyLength *int) ([]taskHistoryRow, error) {
	if historyLength != nil && *historyLength < 0 {
		historyLength = nil
	}
	return queryMany(ctx, db, `
		SELECT messages.task_id, messages.data
		FROM unnest($2::text[]) AS tasks(task_id)
		CROSS JOIN LATERAL (
		    SELECT task_id, data, sequence
		    FROM agent_instance_task_event
		    WHERE history_id = $1 AND task_id = tasks.task_id AND message_id IS NOT NULL
		    ORDER BY sequence DESC
		    LIMIT $3
		) messages
		ORDER BY messages.sequence
	`, pgx.RowToStructByName[taskHistoryRow], historyID, taskIDs, historyLength)
}
