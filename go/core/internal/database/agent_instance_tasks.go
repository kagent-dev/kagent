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
	"google.golang.org/protobuf/proto"
)

func createAgentInstanceTask(ctx context.Context, tx pgx.Tx, instance agentInstanceRow, requestHash []byte, task *a2a.Task) (*a2a.Task, bool, error) {
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

	instanceID := instance.ID.String()
	result := task
	created := false
	err = func() error {
		if instance.State == "AGENT_INSTANCE_STATE_DELETED" {
			return ErrNotFound
		}
		historyID := instance.HistoryID
		if task.ContextID != instance.ContextID.String() {
			return fmt.Errorf("task context does not match AgentInstance")
		}
		existing, err := queryOne(ctx, tx, `
			SELECT history_id, id, state, status_timestamp, data, created_at, updated_at, initial_message_id,
			    request_hash, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence, position
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
			return loadAgentInstanceTaskHistories(ctx, tx, historyID, []*a2a.Task{result}, nil, false)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("get AgentInstance task for message %s: %w", message.ID, err)
		}
		if instance.State != "AGENT_INSTANCE_STATE_READY" || instance.Operation != "AGENT_INSTANCE_OPERATION_UNSPECIFIED" {
			return fmt.Errorf("AgentInstance %s cannot accept a task in state %s with operation %s: %w", instanceID, instance.State, instance.Operation, ErrConflict)
		}
		if err := requirePublishedTasks(ctx, tx, instance.HistoryID); err != nil {
			return err
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
			RETURNING history_id, id, state, status_timestamp, data, created_at, updated_at, initial_message_id,
			    request_hash, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence, position
		`,
			pgx.RowToStructByName[agentInstanceTaskRow], historyID, string(task.ID), string(task.Status.State),
			task.Status.Timestamp, taskData, &message.ID, requestHash, instance.ID,
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
			CreatedAt:        &row.CreatedAt,
		}); err != nil {
			return fmt.Errorf("record task creation: %w", err)
		}
		_, err = storeAgentInstanceTaskMessages(ctx, tx, historyID, string(task.ID), task.ContextID, task.History)
		return err
	}()
	return result, created, err
}

// continueAgentInstanceTask admits a reply under the instance transaction;
// Previous is present only for newly accepted input. Retries return current state.
func continueAgentInstanceTask(ctx context.Context, tx pgx.Tx, instance agentInstanceRow, requestHash []byte, message *a2a.Message) (*TaskAdmission, error) {
	if message == nil || message.ID == "" || message.TaskID == "" || len(requestHash) == 0 {
		return nil, fmt.Errorf("task reply requires message ID, task ID, and request hash")
	}
	var result, waiting *a2a.Task
	err := func() error {
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
		hash, err := queryOne(ctx, tx, `
			SELECT request_hash FROM agent_instance_task_event
			WHERE history_id = $1 AND task_id = $2 AND message_id = $3
		`, pgx.RowTo[[]byte], instance.HistoryID, string(message.TaskID), message.ID)
		if err == nil {
			if !bytes.Equal(hash, requestHash) {
				return ErrIdempotencyConflict
			}
			return loadAgentInstanceTaskHistories(ctx, tx, instance.HistoryID, []*a2a.Task{result}, nil, false)
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if instance.State != "AGENT_INSTANCE_STATE_READY" || instance.Operation != "AGENT_INSTANCE_OPERATION_UNSPECIFIED" {
			return fmt.Errorf("AgentInstance cannot accept a reply in state %s with operation %s: %w", instance.State, instance.Operation, ErrConflict)
		}
		if err := requirePublishedTasks(ctx, tx, instance.HistoryID); err != nil {
			return err
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
		if err := loadAgentInstanceTaskHistories(ctx, tx, instance.HistoryID, []*a2a.Task{result}, nil, false); err != nil {
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
		if err := storeAgentInstanceTaskEvent(ctx, tx, instance, &submitted, message, nil, false); err != nil {
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
	}()
	if err != nil {
		return nil, err
	}
	return &TaskAdmission{Current: result, Previous: waiting}, nil
}

// TaskAdmission describes the committed input and whether this runtime attempt
// may dispatch it. Previous restores a waiting SDK task for a newly admitted reply.
// Version belongs to Current, including when Previous is supplied.
type TaskAdmission struct {
	Current  *a2a.Task
	Previous *a2a.Task
	Version  int64
	Admitted bool
}

// AdmitAgentInstanceMessage atomically admits a public input and records the
// private RPC attempt. A lost admission response may retry that same attempt
// until execution writes its first update. A different public request attempt
// only replays the task; it never obtains another execution grant.
func (c *Client) AdmitAgentInstanceMessage(ctx context.Context, instanceID, reservedTaskID string, attemptID uuid.UUID, requestHash []byte, message *a2a.Message) (*TaskAdmission, error) {
	if attemptID == uuid.Nil || message == nil {
		return nil, fmt.Errorf("runtime admission requires an attempt ID and message")
	}
	// Assigning an initial task ID must not turn a retry of the caller's
	// original message into a continuation request.
	input := *message
	message = &input
	result := &TaskAdmission{}
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		instance, err := lockAgentInstance(ctx, tx, instanceID)
		if err != nil {
			return notFoundOr(err)
		}
		var previousVersion int64
		if message.TaskID == "" {
			message.TaskID = a2a.NewTaskID()
			task := a2a.NewSubmittedTask(message, message)
			apia2a.SetTaskCreatedAt(task, time.Now().UTC())
			result.Current, result.Admitted, err = createAgentInstanceTask(ctx, tx, instance, requestHash, task)
		} else {
			previousVersion, err = taskVersion(ctx, tx, instance.HistoryID, string(message.TaskID))
			if err != nil {
				return err
			}
			var continuation *TaskAdmission
			continuation, err = continueAgentInstanceTask(ctx, tx, instance, requestHash, message)
			if err == nil {
				result.Current, result.Previous = continuation.Current, continuation.Previous
				result.Admitted = continuation.Previous != nil
			}
		}
		if err != nil {
			return err
		}
		result.Version, err = taskVersion(ctx, tx, instance.HistoryID, string(result.Current.ID))
		if err != nil {
			return err
		}
		if result.Admitted {
			// Roll back the entire input if the native session is reserved.
			// Replay is checked first, so old inputs still return their result.
			if reservedTaskID != "" && string(result.Current.ID) != reservedTaskID {
				return fmt.Errorf("native session is waiting for another task: %w", ErrConflict)
			}
			return execSQL(ctx, tx, `
				UPDATE agent_instance_task_event SET admission_id = $2, admission_previous_sequence = $3
				WHERE sequence = $1
			`, result.Version, attemptID, previousVersion)
		}
		// Only a retry of the still-undispatched private call can recover its
		// grant. Once any runtime update commits, this receipt is historical.
		previousVersion, err = queryOne(ctx, tx, `
			SELECT admission_previous_sequence FROM agent_instance_task_event
			WHERE sequence = $1 AND admission_id = $2
		`, pgx.RowTo[int64], result.Version, attemptID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		result.Admitted = true
		if previousVersion > 0 {
			result.Previous, err = readTaskAtVersion(ctx, tx, instance, string(result.Current.ID), previousVersion)
		}
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("admit runtime input: %w", err)
	}
	return result, nil
}

// Reconstruct a retried continuation from its retained boundary, not the task's
// newer projection. This path runs only after a lost private admission response.
func readTaskAtVersion(ctx context.Context, db dbExecutor, instance agentInstanceRow, taskID string, version int64) (*a2a.Task, error) {
	events, err := queryMany(ctx, db, `
		SELECT sequence, history_id, task_id, data, created_at, message_id,
		    task_position, initial_message_id, request_hash, snapshot_atespace,
		    snapshot_uri, snapshot_content_scope
		FROM agent_instance_task_event WHERE history_id = $1 AND task_id = $2 AND sequence <= $3
		ORDER BY sequence
	`, pgx.RowToStructByName[agentInstanceTaskEventRow], instance.HistoryID, taskID, version)
	if err != nil {
		return nil, err
	}
	rows, err := replayTaskEvents(events, instance.ContextID.String())
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, fmt.Errorf("admitted task has no retained boundary")
	}
	task, err := unmarshalAgentInstanceTask(rows[0].Data)
	if err != nil {
		return nil, err
	}
	// Replay builds the task projection; archived messages remain independent
	// retained events and must also stop at the original admission boundary.
	for _, source := range events {
		if source.MessageID == nil {
			continue
		}
		event := &a2apb.StreamResponse{}
		if err := proto.Unmarshal(source.Data, event); err != nil {
			return nil, err
		}
		message, err := pbconv.FromProtoMessage(event.GetMessage())
		if err != nil {
			return nil, err
		}
		task.History = append(task.History, message)
	}
	if task.Status.Message != nil {
		task.Status.Message.TaskID, task.Status.Message.ContextID = task.ID, task.ContextID
	}
	return task, nil
}

// UpdateAgentInstanceTask applies a runtime update only to the version it read.
// The transition and history commit together. An identical retry returns the
// original committed version, even after later updates; a different stale save
// returns ErrConflict. Callers authenticate the runtime's instance authority and
// provide a SHA-256 digest of the complete mutation. This never creates a task:
// an input must have been admitted before its executor can save updates.
func (c *Client) UpdateAgentInstanceTask(ctx context.Context, instanceID string, expectedVersion int64, mutationHash []byte, task *a2a.Task, event a2a.Event) (int64, error) {
	if expectedVersion <= 0 || len(mutationHash) != 32 || task == nil || event == nil {
		return 0, fmt.Errorf("runtime task update requires a version, digest, task, and event")
	}
	var version int64
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		instance, err := lockAgentInstance(ctx, tx, instanceID)
		if err != nil {
			return notFoundOr(err)
		}
		if instance.State == "AGENT_INSTANCE_STATE_DELETED" {
			return ErrNotFound
		}
		if task.ContextID != instance.ContextID.String() {
			return fmt.Errorf("task context does not match AgentInstance: %w", ErrFailedPrecondition)
		}
		type receipt struct {
			Sequence     int64
			MutationHash []byte
		}
		previous, err := queryOne(ctx, tx, `
			SELECT sequence, mutation_hash FROM agent_instance_task_event
			WHERE history_id = $1 AND task_id = $2 AND expected_version = $3
		`, pgx.RowToStructByName[receipt], instance.HistoryID, string(task.ID), expectedVersion)
		if err == nil {
			if !bytes.Equal(previous.MutationHash, mutationHash) {
				return ErrConflict
			}
			version = previous.Sequence
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		row, err := readAgentInstanceTask(ctx, tx, instance.HistoryID, string(task.ID))
		if err != nil {
			return notFoundOr(err)
		}
		version, err = taskVersion(ctx, tx, instance.HistoryID, string(task.ID))
		if err != nil {
			return err
		}
		if expectedVersion != version {
			return ErrConflict
		}
		stored, err := unmarshalAgentInstanceTask(row.Data)
		if err != nil {
			return err
		}
		if err := requirePublishedTasks(ctx, tx, instance.HistoryID); err != nil {
			return err
		}
		if stored.Status.State.Terminal() {
			return fmt.Errorf("a terminal task cannot be updated: %w", ErrFailedPrecondition)
		}
		if (stored.Status.State == a2a.TaskStateInputRequired || stored.Status.State == a2a.TaskStateAuthRequired) && !task.Status.State.Terminal() {
			return fmt.Errorf("a waiting task requires an admitted reply: %w", ErrFailedPrecondition)
		}
		if instance.State != "AGENT_INSTANCE_STATE_READY" || instance.Operation != "AGENT_INSTANCE_OPERATION_UNSPECIFIED" {
			return fmt.Errorf("AgentInstance cannot accept runtime updates during a lifecycle operation: %w", ErrConflict)
		}
		boundary := task.Status.State.Terminal() || task.Status.State == a2a.TaskStateInputRequired || task.Status.State == a2a.TaskStateAuthRequired
		if err := storeAgentInstanceTaskEvent(ctx, tx, instance, task, event, nil, boundary); err != nil {
			return err
		}
		version, err = taskVersion(ctx, tx, instance.HistoryID, string(task.ID))
		if err != nil {
			return err
		}
		if boundary {
			if err := execSQL(ctx, tx, `
				UPDATE agent_instance_task_event SET published = FALSE
				WHERE history_id = $1 AND task_id = $2 AND sequence > $3 AND sequence <= $4
			`, instance.HistoryID, string(task.ID), expectedVersion, version); err != nil {
				return err
			}
		}
		return execSQL(ctx, tx, `
			UPDATE agent_instance_task_event SET expected_version = $2, mutation_hash = $3
			WHERE sequence = $1
		`, version, expectedVersion, mutationHash)
	})
	if err != nil {
		return 0, fmt.Errorf("update AgentInstance task %s: %w", task.ID, err)
	}
	return version, nil
}

// GetVersionedAgentInstanceTask returns one consistent task, full history, and
// storage version for a runtime read. Versions come from the retained history,
// so forks have independent versions even when their wire task IDs are shared.
// Missing or deleted instances and absent tasks return ErrNotFound. Callers
// authenticate instance authority before reading.
func (c *Client) GetVersionedAgentInstanceTask(ctx context.Context, instanceID, taskID string) (*a2a.Task, int64, error) {
	var task *a2a.Task
	var version int64
	err := pgx.BeginTxFunc(ctx, c.db, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		type storedTask struct {
			HistoryID uuid.UUID
			Data      []byte
			Version   int64
		}
		row, err := queryOne(ctx, tx, `
			SELECT t.history_id, t.data,
			    (SELECT MAX(e.sequence) FROM agent_instance_task_event e
			     WHERE e.history_id = t.history_id AND e.task_id = t.id) AS version
			FROM agent_instance_task t JOIN agent_instance i ON i.history_id = t.history_id
			WHERE i.id = $1 AND i.state <> 'AGENT_INSTANCE_STATE_DELETED' AND t.id = $2
		`, pgx.RowToStructByName[storedTask], instanceID, taskID)
		if err != nil {
			return notFoundOr(err)
		}
		task, err = unmarshalAgentInstanceTask(row.Data)
		if err != nil {
			return err
		}
		version = row.Version
		pending, err := queryOne(ctx, tx, `
			SELECT data FROM agent_instance_task_event WHERE sequence = $1 AND NOT published
		`, pgx.RowTo[[]byte], version)
		if err == nil {
			wire, err := pbconv.ToProtoTask(task)
			if err != nil {
				return err
			}
			event := &a2apb.StreamResponse{}
			if err := proto.Unmarshal(pending, event); err != nil {
				return err
			}
			wire, err = applyTaskEvent(wire, event)
			if err != nil {
				return err
			}
			task, err = pbconv.FromProtoTask(wire)
			if err != nil {
				return err
			}
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		return loadAgentInstanceTaskHistories(ctx, tx, row.HistoryID, []*a2a.Task{task}, nil, true)
	})
	if err != nil {
		return nil, 0, fmt.Errorf("get versioned AgentInstance task %s: %w", taskID, err)
	}
	return task, version, nil
}

// taskVersion reads the last committed event for a task. Callers serialize
// mutations with the instance lock or use a consistent read transaction.
func taskVersion(ctx context.Context, db dbExecutor, historyID uuid.UUID, taskID string) (int64, error) {
	return queryOne(ctx, db, `
		SELECT sequence FROM agent_instance_task_event
		WHERE history_id = $1 AND task_id = $2 ORDER BY sequence DESC LIMIT 1
	`, pgx.RowTo[int64], historyID, taskID)
}

// storeAgentInstanceTaskEvent persists a task transition, its messages, and optional
// snapshot in the caller's transaction. The caller must hold the instance row lock;
// checkpoint creation blocks the write. Continuation admission validates the waiting
// state and retry identity before invoking this shared persistence operation.
func storeAgentInstanceTaskEvent(ctx context.Context, tx pgx.Tx, instance agentInstanceRow, task *a2a.Task, event a2a.Event, snapshot *AgentInstanceTaskSnapshot, deferPublication bool) error {
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
	var taskRow agentInstanceTaskRow
	if row, err := queryOne(ctx, tx, `
		SELECT history_id, id, state, status_timestamp, data, created_at, updated_at, initial_message_id,
		    request_hash, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence, position FROM
		    agent_instance_task WHERE history_id = $1 AND id = $2 FOR UPDATE
	`, pgx.RowToStructByName[agentInstanceTaskRow], historyID, string(task.ID)); err == nil {
		taskRow = row
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
	if !deferPublication {
		taskRow, err = saveTaskProjection(ctx, tx, historyID, string(task.ID), string(task.Status.State), task.Status.Timestamp, data)
		if err != nil {
			if isActiveTaskConflict(err) {
				return fmt.Errorf("AgentInstance %s already has an active task: %w", instance.ID, ErrConflict)
			}
			return fmt.Errorf("store AgentInstance task %s: %w", task.ID, err)
		}
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

// GetAgentInstanceTask returns a task with up to historyLength latest archived messages,
// or ErrNotFound if the instance or task is absent. Nil or negative historyLength loads
// all history; zero skips it. Callers authorize instance access.
func (c *Client) GetAgentInstanceTask(ctx context.Context, instanceID, taskID string, historyLength *int) (*a2a.Task, error) {
	return c.getPublicTask(ctx, instanceID, taskID, historyLength, false)
}

// GetSettledAgentInstanceTask reads the public task only after its pending native
// boundary is published. ErrConflict means publication is still pending. A later
// admitted turn may already be current; callers must not wait for an old status
// value to recur. Ownership and missing-record semantics match GetAgentInstanceTask.
func (c *Client) GetSettledAgentInstanceTask(ctx context.Context, instanceID, taskID string, historyLength *int) (*a2a.Task, error) {
	return c.getPublicTask(ctx, instanceID, taskID, historyLength, true)
}

// getPublicTask keeps the public projection and archived messages at one read
// snapshot. Checking settlement never waits while holding a transaction or lock.
func (c *Client) getPublicTask(ctx context.Context, instanceID, taskID string, historyLength *int, settled bool) (*a2a.Task, error) {
	var task *a2a.Task
	err := pgx.BeginTxFunc(ctx, c.db, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		type publicTask struct {
			HistoryID uuid.UUID
			Data      []byte
			Pending   bool
		}
		row, err := queryOne(ctx, tx, `
			SELECT t.history_id, t.data,
			    ($3::boolean AND EXISTS (SELECT 1 FROM agent_instance_task_event e
			      WHERE e.history_id = t.history_id AND e.task_id = t.id AND NOT e.published)) AS pending
			FROM agent_instance_task t JOIN agent_instance i ON i.history_id = t.history_id
			WHERE i.id = $1 AND i.state <> 'AGENT_INSTANCE_STATE_DELETED' AND t.id = $2
		`, pgx.RowToStructByName[publicTask], instanceID, taskID, settled)
		if err != nil {
			return notFoundOr(err)
		}
		if row.Pending {
			return ErrConflict
		}
		task, err = unmarshalAgentInstanceTask(row.Data)
		if err != nil {
			return err
		}
		return loadAgentInstanceTaskHistories(ctx, tx, row.HistoryID, []*a2a.Task{task}, historyLength, false)
	})
	if err != nil {
		return nil, fmt.Errorf("read public AgentInstance task %s: %w", taskID, err)
	}
	return task, nil
}

// ListAgentInstanceTasks returns tasks with archived messages in immutable creation order
// after afterID, with optional state and exclusive status-timestamp filters. The total
// counts all matching tasks before pagination; it is read separately and can differ under
// concurrent writes. History limits have the same semantics as GetAgentInstanceTask.
// Callers authorize instance access.
func (c *Client) ListAgentInstanceTasks(ctx context.Context, instanceID, afterID string, state a2a.TaskState, statusTimestampAfter *time.Time, limit int, historyLength *int) ([]*a2a.Task, int, error) {
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
	if err := loadAgentInstanceTaskHistories(ctx, c.db, instance.HistoryID, tasks, historyLength, false); err != nil {
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
// order, limited to the latest historyLength per task. Zero clears history without a
// query; nil or negative loads it all. Tasks without archived messages otherwise retain
// their inline history, subject to the same limit. Malformed messages return errors.
func loadAgentInstanceTaskHistories(ctx context.Context, db dbExecutor, historyID uuid.UUID, tasks []*a2a.Task, historyLength *int, includeUnpublished bool) error {
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
	rows, err := readTaskMessages(ctx, db, historyID, ids, historyLength, includeUnpublished)
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

// readTaskMessages returns the latest historyLength archived messages per requested task
// in event order. Nil or negative limits load all messages. Callers authorize the history
// and decode the returned payloads.
func readTaskMessages(ctx context.Context, db dbExecutor, historyID uuid.UUID, taskIDs []string, historyLength *int, includeUnpublished bool) ([]taskHistoryRow, error) {
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
		      AND (published OR $4::boolean)
		    ORDER BY sequence DESC
		    LIMIT $3
		) messages
		ORDER BY messages.sequence
	`, pgx.RowToStructByName[taskHistoryRow], historyID, taskIDs, historyLength, includeUnpublished)
}

// The instance lock makes this admission barrier atomic with boundary staging.
func requirePublishedTasks(ctx context.Context, db dbExecutor, historyID uuid.UUID) error {
	pending, err := queryOne(ctx, db, `
		SELECT EXISTS (SELECT 1 FROM agent_instance_task_event WHERE history_id = $1 AND NOT published)
	`, pgx.RowTo[bool], historyID)
	if err != nil {
		return err
	}
	if pending {
		return fmt.Errorf("runtime snapshot is not yet settled: %w", ErrFailedPrecondition)
	}
	return nil
}

// GetAgentInstanceInput reads an accepted public input without waking its
// runtime. Missing input returns ErrNotFound; reusing an ID with different
// content returns ErrIdempotencyConflict. This lookup never grants execution.
func (c *Client) GetAgentInstanceInput(ctx context.Context, instanceID, taskID, messageID string, requestHash []byte) (*a2a.Task, error) {
	instance, err := readAgentInstance(ctx, c.db, instanceID)
	if err != nil {
		return nil, notFoundOr(err)
	}
	type input struct {
		TaskID      string
		RequestHash []byte
	}
	row, err := queryOne(ctx, c.db, `
		SELECT id AS task_id, request_hash FROM agent_instance_task
		WHERE history_id = $1 AND $2::text = '' AND initial_message_id = $3
		UNION ALL
		SELECT task_id, request_hash FROM agent_instance_task_event
		WHERE history_id = $1 AND $2::text <> '' AND task_id = $2 AND message_id = $3 AND request_hash IS NOT NULL
	`, pgx.RowToStructByName[input], instance.HistoryID, taskID, messageID)
	if err != nil {
		return nil, notFoundOr(err)
	}
	if !bytes.Equal(row.RequestHash, requestHash) {
		return nil, ErrIdempotencyConflict
	}
	return c.GetAgentInstanceTask(ctx, instanceID, row.TaskID, nil)
}
