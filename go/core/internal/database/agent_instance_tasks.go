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
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// CreateAgentInstanceTask atomically stores a task, its creation event, and initial
// messages for a READY instance with no lifecycle operation. It requires an initial
// message and matching context. Reusing the initial message ID returns the stored task if
// the request hash matches, or ErrIdempotencyConflict otherwise. An occupied active-task
// slot or checkpoint creation blocks new tasks with ErrAgentInstanceTaskConflict. The
// boolean reports a new reservation; callers authorize access and invoke the runtime
// separately.
func (c *Client) CreateAgentInstanceTask(ctx context.Context, instanceID string, requestHash []byte, task *a2a.Task) (*a2a.Task, bool, error) {
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
	created := false
	var historyID uuid.UUID
	err = c.withTx(ctx, func(tx pgx.Tx) error {
		instance, err := lockAgentInstance(ctx, tx, instanceID)
		if err != nil {
			return fmt.Errorf("lock AgentInstance %s: %w", instanceID, err)
		}
		if instance.State != "AGENT_INSTANCE_STATE_READY" || instance.Operation != "AGENT_INSTANCE_OPERATION_UNSPECIFIED" {
			return ErrAgentInstanceTaskConflict
		}
		historyID = instance.HistoryID
		if task.ContextID != instance.ContextID.String() {
			return fmt.Errorf("task context does not match AgentInstance")
		}
		row, err := queryOne(ctx, tx, `
			INSERT INTO agent_instance_task (
			    history_id, id, state, status_timestamp, data, initial_message_id, request_hash
			)
			SELECT $1, $2, $3, $4, $5, $6, $7
			WHERE NOT EXISTS (
			    SELECT 1 FROM agent_instance_checkpoint
			    WHERE source_instance_id = $8 AND state = 'CREATING'
			)
			ON CONFLICT (history_id, initial_message_id)
			    WHERE initial_message_id IS NOT NULL
			DO NOTHING
			RETURNING history_id, id, state, status_timestamp, data, created_at, updated_at, initial_message_id,
			    request_hash, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence, position
		`,
			pgx.RowToStructByName[agentInstanceTaskRow], historyID, string(task.ID), string(task.Status.State),
			task.Status.Timestamp, taskData, &message.ID, requestHash, instance.ID,
		)
		if errors.Is(err, pgx.ErrNoRows) {
			row, err := queryOne(ctx, tx, `
				SELECT history_id, id, state, status_timestamp, data, created_at, updated_at, initial_message_id,
				    request_hash, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence, position FROM
				    agent_instance_task
				WHERE history_id = $1 AND initial_message_id = $2
			`, pgx.RowToStructByName[agentInstanceTaskRow], historyID, &message.ID)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrAgentInstanceTaskConflict
			}
			if err != nil {
				return fmt.Errorf("get AgentInstance task for message %s: %w", message.ID, err)
			}
			if !bytes.Equal(row.RequestHash, requestHash) {
				return ErrIdempotencyConflict
			}
			result, err = unmarshalAgentInstanceTask(row.Data)
			if err != nil {
				return err
			}
			return loadAgentInstanceTaskHistories(ctx, tx, historyID, []*a2a.Task{result})
		}
		if err != nil {
			if isActiveTaskConflict(err) {
				return ErrAgentInstanceTaskConflict
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
	return result, created, nil
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
		WHERE i.id = $1
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
		err = loadAgentInstanceTaskHistories(ctx, c.db, row.HistoryID, []*a2a.Task{task})
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
			    request_hash, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence, position FROM
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
		// Serialize task transitions with checkpoint reservation, without holding
		// a transaction across runtime or snapshot network calls.
		instance, err := lockAgentInstance(ctx, tx, instanceID)
		if err != nil {
			return notFoundOr(err)
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
			return ErrAgentInstanceConflict
		}
		var sequence int64
		var stored *a2apb.Task
		if row, err := queryOne(ctx, tx, `
			SELECT history_id, id, state, status_timestamp, data, created_at, updated_at, initial_message_id,
			    request_hash, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence, position FROM
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
				return ErrAgentInstanceTaskConflict
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
	})
	if err != nil {
		return fmt.Errorf("store AgentInstance task update: %w", err)
	}
	return nil
}

// GetAgentInstanceTask returns a task with its archived message history, or ErrNotFound if
// the instance or task is absent. Callers authorize instance access.
func (c *Client) GetAgentInstanceTask(ctx context.Context, instanceID, taskID string) (*a2a.Task, error) {
	instance, err := readAgentInstance(ctx, c.db, instanceID)
	if err != nil {
		return nil, fmt.Errorf("get AgentInstance history: %w", notFoundOr(err))
	}
	row, err := readAgentInstanceTask(ctx, c.db, instance.HistoryID, taskID)
	if err != nil {
		return nil, fmt.Errorf("get AgentInstance task %s: %w", taskID, notFoundOr(err))
	}
	task, err := unmarshalAgentInstanceTask(row.Data)
	if err == nil {
		err = loadAgentInstanceTaskHistories(ctx, c.db, instance.HistoryID, []*a2a.Task{task})
	}
	return task, err
}

// ListAgentInstanceTasks returns tasks with archived messages in immutable creation order
// after afterID, with optional state and exclusive status-timestamp filters. The total
// counts all matching tasks before pagination; it is read separately and can differ under
// concurrent writes. Callers authorize instance access.
func (c *Client) ListAgentInstanceTasks(ctx context.Context, instanceID, afterID string, state a2a.TaskState, statusTimestampAfter *time.Time, limit int) ([]*a2a.Task, int, error) {
	instance, err := readAgentInstance(ctx, c.db, instanceID)
	if err != nil {
		return nil, 0, fmt.Errorf("get AgentInstance history: %w", notFoundOr(err))
	}

	total, err := queryOne(ctx, c.db, `
		SELECT COUNT(*) FROM agent_instance_task
		WHERE history_id = $1
		  AND ($2::text = '' OR state = $2)
		  AND ($3::timestamptz IS NULL
		       OR status_timestamp > $3)
	`, pgx.RowTo[int64], instance.HistoryID, string(state), statusTimestampAfter)
	if err != nil {
		return nil, 0, fmt.Errorf("count AgentInstance tasks: %w", err)
	}
	rows, err := queryMany(ctx, c.db, `
		SELECT t.history_id, t.id, t.state, t.status_timestamp, t.data, t.created_at, t.updated_at,
		    t.initial_message_id, t.request_hash, t.snapshot_atespace, t.snapshot_uri, t.snapshot_content_scope,
		    t.history_sequence, t.position FROM agent_instance_task t
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
		return nil, 0, fmt.Errorf("list AgentInstance tasks: %w", err)
	}
	tasks := make([]*a2a.Task, 0, len(rows))
	for _, row := range rows {
		task, err := unmarshalAgentInstanceTask(row.Data)
		if err != nil {
			return nil, 0, fmt.Errorf("decode AgentInstance task %s: %w", row.ID, err)
		}
		tasks = append(tasks, task)
	}
	if err := loadAgentInstanceTaskHistories(ctx, c.db, instance.HistoryID, tasks); err != nil {
		return nil, 0, err
	}
	return tasks, int(total), nil
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
// order. It leaves tasks with no archived messages unchanged and rejects malformed message
// payloads.
func loadAgentInstanceTaskHistories(ctx context.Context, db dbExecutor, historyID uuid.UUID, tasks []*a2a.Task) error {
	if len(tasks) == 0 {
		return nil
	}
	ids := make([]string, len(tasks))
	byID := make(map[string]*a2a.Task, len(tasks))
	for index, task := range tasks {
		ids[index] = string(task.ID)
		byID[string(task.ID)] = task
	}
	rows, err := readTaskMessages(ctx, db, historyID, ids)
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
		         task_position, initial_message_id, request_hash, created_at)
		    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, COALESCE($11::timestamptz, NOW()))
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
		event.CreatedAt,
	)
}

// readAgentInstanceTask reads a task's stored projection without decoding or attaching
// history. Missing tasks return pgx.ErrNoRows; callers authorize access.
func readAgentInstanceTask(ctx context.Context, db dbExecutor, historyID uuid.UUID, taskID string) (agentInstanceTaskRow, error) {
	return queryOne(ctx, db, `
		SELECT history_id, id, state, status_timestamp, data, created_at, updated_at, initial_message_id,
		    request_hash, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence, position FROM
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
		    request_hash, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence, position
	`, pgx.RowToStructByName[agentInstanceTaskRow], historyID, taskID, state, statusTimestamp, data)
}

// readTaskMessages returns archived message payloads for the requested tasks in event
// order. Callers authorize the history and decode the returned payloads.
func readTaskMessages(ctx context.Context, db dbExecutor, historyID uuid.UUID, taskIDs []string) ([]taskHistoryRow, error) {
	return queryMany(ctx, db, `
		SELECT task_id, data
		FROM agent_instance_task_event
		WHERE history_id = $1
		  AND task_id = ANY($2::text[])
		  AND message_id IS NOT NULL
		ORDER BY sequence
	`, pgx.RowToStructByName[taskHistoryRow], historyID, taskIDs)
}
