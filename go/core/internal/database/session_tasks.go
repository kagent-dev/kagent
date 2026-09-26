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

// ErrDispatchBusy means a send has not been forwarded because another dispatch
// or idle lifecycle operation currently owns the session.
var ErrDispatchBusy = errors.New("session temporarily unavailable for dispatch")

// ErrMessageAccepted means an earlier dispatch already persisted this initial input.
var ErrMessageAccepted = errors.New("message already accepted")

// ReserveSessionDispatch prevents pause/suspend between routing a request
// and its first persisted task. The attempt expires after two minutes; a late
// first save must fail before native execution. This is not a native work lease.
func (c *Client) ReserveSessionDispatch(ctx context.Context, sessionID string, dispatchID uuid.UUID, initialMessageID string) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, sessionID)
		if err != nil {
			return notFoundOr(err)
		}
		if initialMessageID != "" {
			accepted, err := queryOne(ctx, tx, `
				SELECT EXISTS (SELECT 1 FROM session_task_event
				    WHERE history_id = $1 AND message_id = $2)
			`, pgx.RowTo[bool], session.HistoryID, initialMessageID)
			if err != nil {
				return err
			}
			if accepted {
				return ErrMessageAccepted
			}
		}
		if session.State != "SESSION_STATE_READY" || session.Operation != "SESSION_OPERATION_UNSPECIFIED" {
			return ErrConflict
		}
		blocked, err := queryOne(ctx, tx, `
			SELECT EXISTS (SELECT 1 FROM session_task WHERE history_id = $1
			    AND state IN ('TASK_STATE_SUBMITTED', 'TASK_STATE_WORKING'))
			    OR EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = $2 AND state = 'CREATING')
		`, pgx.RowTo[bool], session.HistoryID, session.ID)
		if err != nil {
			return err
		}
		if blocked {
			return ErrConflict
		}
		if err := requireSettledRuntime(ctx, tx, session.HistoryID, ""); err != nil {
			if errors.Is(err, ErrFailedPrecondition) {
				return ErrDispatchBusy
			}
			return err
		}
		return execSQL(ctx, tx, `
			UPDATE session SET dispatch_id = $2, dispatch_expires_at = clock_timestamp() + INTERVAL '2 minutes'
			WHERE id = $1
		`, session.ID, dispatchID)
	})
}

// RevokeSessionDispatch fences an unused attempt. True proves its first
// save did not commit and cannot commit later, so retrying the send is safe.
// False does not prove acceptance: callers must preserve ambiguous outcomes.
func (c *Client) RevokeSessionDispatch(ctx context.Context, sessionID string, dispatchID uuid.UUID, messageID string) (bool, error) {
	var notAccepted bool
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, sessionID)
		if err != nil {
			return notFoundOr(err)
		}
		tag, err := tx.Exec(ctx, `
			UPDATE session SET dispatch_id = NULL, dispatch_expires_at = NULL
			WHERE id = $1 AND dispatch_id = $2
		`, session.ID, dispatchID)
		if err != nil || tag.RowsAffected() == 0 {
			return err
		}
		// A continuation may have saved its input while still marked waiting.
		// Revoking that attempt is safe, but it is not an unaccepted input.
		notAccepted, err = queryOne(ctx, tx, `
			SELECT NOT EXISTS (SELECT 1 FROM session_task_event WHERE history_id = $1 AND message_id = $2)
		`, pgx.RowTo[bool], session.HistoryID, messageID)
		return err
	})
	return notAccepted, err
}

// UpdateSessionTask applies a runtime update only to the version it read.
// The transition and history commit together. An identical retry returns the
// original committed version, even after later updates; a different stale save
// returns ErrConflict. Callers authenticate the runtime's session authority and
// provide a SHA-256 digest of the complete mutation. This never creates a task.
func (c *Client) UpdateSessionTask(ctx context.Context, sessionID string, expectedVersion int64, mutationHash []byte, task *a2a.Task, event a2a.Event, dispatchID string) (int64, error) {
	if expectedVersion <= 0 {
		return 0, fmt.Errorf("task update requires a stored version")
	}
	return c.writeRuntimeTask(ctx, sessionID, expectedVersion, mutationHash, task, event, dispatchID)
}

// CreateRuntimeTask stores a new SDK task and its history atomically. An identical
// storage retry returns the original version; a different write to the same ID
// conflicts. It does not deduplicate user requests or grant execution permission.
func (c *Client) CreateRuntimeTask(ctx context.Context, sessionID string, mutationHash []byte, task *a2a.Task, dispatchID string) (int64, error) {
	return c.writeRuntimeTask(ctx, sessionID, 0, mutationHash, task, task, dispatchID)
}

func (c *Client) writeRuntimeTask(ctx context.Context, sessionID string, expectedVersion int64, mutationHash []byte, task *a2a.Task, event a2a.Event, dispatchID string) (int64, error) {
	if expectedVersion < 0 || len(mutationHash) != 32 || task == nil || event == nil {
		return 0, fmt.Errorf("runtime task update requires a version, digest, task, and event")
	}
	var version int64
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, sessionID)
		if err != nil {
			return notFoundOr(err)
		}
		if session.State == "SESSION_STATE_DELETED" {
			return ErrNotFound
		}
		if task.ContextID != session.ContextID.String() {
			return fmt.Errorf("task context does not match Session: %w", ErrFailedPrecondition)
		}
		type receipt struct {
			Sequence     int64
			MutationHash []byte
		}
		previous, err := queryOne(ctx, tx, `
			SELECT sequence, mutation_hash FROM session_task_event
			WHERE history_id = $1 AND task_id = $2 AND expected_version = $3
		`, pgx.RowToStructByName[receipt], session.HistoryID, string(task.ID), expectedVersion)
		if err == nil {
			if !bytes.Equal(previous.MutationHash, mutationHash) {
				if expectedVersion == 0 {
					return ErrIdempotencyConflict
				}
				return ErrConflict
			}
			version = previous.Sequence
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		var stored *a2a.Task
		row, err := readSessionTask(ctx, tx, session.HistoryID, string(task.ID))
		if expectedVersion == 0 {
			if err == nil {
				return ErrIdempotencyConflict
			}
			if !errors.Is(err, pgx.ErrNoRows) {
				return err
			}
		} else {
			if err != nil {
				return notFoundOr(err)
			}
			version, err = taskVersion(ctx, tx, session.HistoryID, string(task.ID))
			if err != nil {
				return err
			}
			if expectedVersion != version {
				return ErrConflict
			}
			stored, err = unmarshalSessionTask(row.Data)
			if err != nil {
				return err
			}
			if stored.Status.State.Terminal() {
				return fmt.Errorf("a terminal task cannot be updated: %w", ErrFailedPrecondition)
			}
		}
		if session.State != "SESSION_STATE_READY" || session.Operation != "SESSION_OPERATION_UNSPECIFIED" {
			return fmt.Errorf("session cannot accept runtime updates during a lifecycle operation: %w", ErrConflict)
		}
		if (stored == nil || stored.Status.State == a2a.TaskStateInputRequired || stored.Status.State == a2a.TaskStateAuthRequired) && dispatchID != "" {
			tag, err := tx.Exec(ctx, `
				UPDATE session SET dispatch_id = CASE WHEN $3 THEN dispatch_id END,
				    dispatch_expires_at = CASE WHEN $3 THEN dispatch_expires_at END
				WHERE id = $1 AND dispatch_id = $2 AND dispatch_expires_at > clock_timestamp()
			`, session.ID, dispatchID, stored != nil && stored.Status.State == task.Status.State)
			if err != nil {
				return err
			}
			if tag.RowsAffected() != 1 {
				return fmt.Errorf("dispatch expired or was revoked before acceptance: %w", ErrFailedPrecondition)
			}
		}
		if err := requireSettledRuntime(ctx, tx, session.HistoryID, dispatchID); err != nil {
			return err
		}
		// New execution supersedes idle work that no worker has claimed yet.
		// Keep the marker for idempotent acknowledgements of the older boundary.
		if stored == nil || stored.Status.State == a2a.TaskStateInputRequired || stored.Status.State == a2a.TaskStateAuthRequired {
			if err := execSQL(ctx, tx, `
				UPDATE session_task_event SET quiescence_pending = FALSE
				WHERE history_id = $1 AND quiescence_pending
			`, session.HistoryID); err != nil {
				return err
			}
		}
		boundary := task.Status.State.Terminal() || ((task.Status.State == a2a.TaskStateInputRequired || task.Status.State == a2a.TaskStateAuthRequired) && (stored == nil || stored.Status.State != task.Status.State))
		if expectedVersion == 0 && boundary {
			// A first event may already finish the task. Retain a non-final
			// projection until native cleanup finishes.
			initial := *task
			initial.Status = a2a.TaskStatus{State: a2a.TaskStateSubmitted}
			if err := storeSessionTaskEvent(ctx, tx, session, &initial, &initial, false); err != nil {
				return err
			}
		}
		if err := storeSessionTaskEvent(ctx, tx, session, task, event, boundary); err != nil {
			return err
		}
		version, err = taskVersion(ctx, tx, session.HistoryID, string(task.ID))
		if err != nil {
			return err
		}
		if boundary {
			if err := execSQL(ctx, tx, `
				UPDATE session_task_event SET published = FALSE
				WHERE history_id = $1 AND task_id = $2 AND sequence > $3 AND sequence <= $4
			`, session.HistoryID, string(task.ID), expectedVersion, version); err != nil {
				return err
			}
		}
		return execSQL(ctx, tx, `
			UPDATE session_task_event SET expected_version = $2, mutation_hash = $3, quiescence_pending = CASE WHEN $4 THEN TRUE ELSE NULL END
			WHERE sequence = $1
		`, version, expectedVersion, mutationHash, boundary)
	})
	if err != nil {
		return 0, fmt.Errorf("update Session task %s: %w", task.ID, err)
	}
	return version, nil
}

// GetVersionedSessionTask returns one consistent task, full history, and
// storage version for a runtime read. Versions come from the retained history,
// so forks have independent versions even when their wire task IDs are shared.
// Missing or deleted sessions and absent tasks return ErrNotFound. Callers
// authenticate session authority before reading.
func (c *Client) GetVersionedSessionTask(ctx context.Context, sessionID, taskID string) (*a2a.Task, int64, error) {
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
			    (SELECT MAX(e.sequence) FROM session_task_event e
			     WHERE e.history_id = t.history_id AND e.task_id = t.id) AS version
			FROM session_task t JOIN session i ON i.history_id = t.history_id
			WHERE i.id = $1 AND i.state <> 'SESSION_STATE_DELETED' AND t.id = $2
		`, pgx.RowToStructByName[storedTask], sessionID, taskID)
		if err != nil {
			return notFoundOr(err)
		}
		task, err = unmarshalSessionTask(row.Data)
		if err != nil {
			return err
		}
		version = row.Version
		pending, err := queryOne(ctx, tx, `
			SELECT data FROM session_task_event WHERE sequence = $1 AND NOT published
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
		return loadSessionTaskHistories(ctx, tx, row.HistoryID, []*a2a.Task{task}, nil, true)
	})
	if err != nil {
		return nil, 0, fmt.Errorf("get versioned Session task %s: %w", taskID, err)
	}
	return task, version, nil
}

// taskVersion reads the last committed event for a task. Callers serialize
// mutations with the session lock or use a consistent read transaction.
func taskVersion(ctx context.Context, db dbExecutor, historyID uuid.UUID, taskID string) (int64, error) {
	return queryOne(ctx, db, `
		SELECT sequence FROM session_task_event
		WHERE history_id = $1 AND task_id = $2 ORDER BY sequence DESC LIMIT 1
	`, pgx.RowTo[int64], historyID, taskID)
}

// storeSessionTaskEvent persists a task transition and its messages in the
// caller's transaction. The caller must hold the session row lock;
// checkpoint creation blocks the write. The caller checks the storage version
// before invoking this shared persistence operation.
func storeSessionTaskEvent(ctx context.Context, tx pgx.Tx, session sessionRow, task *a2a.Task, event a2a.Event, deferPublication bool) error {
	if session.State == "SESSION_STATE_DELETED" {
		return ErrNotFound
	}
	historyID := session.HistoryID
	if event.TaskInfo().ContextID != session.ContextID.String() || task.ContextID != session.ContextID.String() {
		return fmt.Errorf("task event context does not match Session")
	}
	creating, err := queryOne(ctx, tx, `
		SELECT EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = $1 AND state = 'CREATING')
	`, pgx.RowTo[bool], session.ID)
	if err != nil {
		return err
	}
	if creating {
		return fmt.Errorf("session %s has a checkpoint being created: %w", session.ID, ErrConflict)
	}
	var stored *a2apb.Task
	var taskRow sessionTaskRow
	if row, err := queryOne(ctx, tx, `
		SELECT history_id, id, state, status_timestamp, data, created_at, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence, position FROM
		    session_task WHERE history_id = $1 AND id = $2 FOR UPDATE
	`, pgx.RowToStructByName[sessionTaskRow], historyID, string(task.ID)); err == nil {
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
			if err := storeProtoTaskMessages(ctx, tx, historyID, string(task.ID), task.ContextID, messages); err != nil {
				return fmt.Errorf("archive Session task history: %w", err)
			}
		}
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("get Session task %s: %w", task.ID, err)
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
				return fmt.Errorf("session %s already has an active task: %w", session.ID, ErrConflict)
			}
			return fmt.Errorf("store Session task %s: %w", task.ID, err)
		}
	}

	if newTask {
		data, err := proto.Marshal(durable)
		if err != nil {
			return err
		}
		_, err = insertTaskEvent(ctx, tx, taskEventWrite{
			HistoryID:    historyID,
			TaskID:       taskRow.ID,
			Data:         data,
			TaskPosition: &taskRow.Position,
			CreatedAt:    &taskRow.CreatedAt,
		})
		if err != nil {
			return fmt.Errorf("record task creation: %w", err)
		}
	}

	messages := sessionTaskEventMessages(task, event)
	if len(messages) > 0 {
		if err := storeSessionTaskMessages(ctx, tx, historyID, string(event.TaskInfo().TaskID), session.ContextID.String(), messages); err != nil {
			return fmt.Errorf("store Session task history: %w", err)
		}
	}
	if !newTask {
		eventData, err := proto.Marshal(durable)
		if err != nil {
			return err
		}
		_, err = insertTaskEvent(ctx, tx, taskEventWrite{
			HistoryID: historyID, TaskID: string(task.ID), Data: eventData,
		})
		if err != nil {
			return fmt.Errorf("store Session task event: %w", err)
		}
	}

	return nil
}

// GetSessionTask returns a task with up to historyLength latest archived messages,
// or ErrNotFound if the session or task is absent. Nil or negative historyLength loads
// all history; zero skips it. Callers authorize session access.
func (c *Client) GetSessionTask(ctx context.Context, sessionID, taskID string, historyLength *int) (*a2a.Task, error) {
	return c.getPublicTask(ctx, sessionID, taskID, historyLength, false)
}

// GetSettledSessionTask reads the public task after native cleanup publishes
// its pending update. ErrConflict means cleanup is still pending. A later
// admitted turn may already be current; callers must not wait for an old status
// value to recur. Ownership and missing-record semantics match GetSessionTask.
func (c *Client) GetSettledSessionTask(ctx context.Context, sessionID, taskID string, historyLength *int) (*a2a.Task, error) {
	return c.getPublicTask(ctx, sessionID, taskID, historyLength, true)
}

// getPublicTask keeps the public projection and archived messages at one read
// snapshot. Checking settlement never waits while holding a transaction or lock.
func (c *Client) getPublicTask(ctx context.Context, sessionID, taskID string, historyLength *int, settled bool) (*a2a.Task, error) {
	var task *a2a.Task
	err := pgx.BeginTxFunc(ctx, c.db, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		type publicTask struct {
			HistoryID uuid.UUID
			Data      []byte
			CreatedAt time.Time
			Pending   bool
		}
		row, err := queryOne(ctx, tx, `
			SELECT t.history_id, t.data, t.created_at,
			    ($3::boolean AND EXISTS (SELECT 1 FROM session_task_event e
			      WHERE e.history_id = t.history_id AND e.task_id = t.id AND NOT e.published)) AS pending
			FROM session_task t JOIN session i ON i.history_id = t.history_id
			WHERE i.id = $1 AND i.state <> 'SESSION_STATE_DELETED' AND t.id = $2
		`, pgx.RowToStructByName[publicTask], sessionID, taskID, settled)
		if err != nil {
			return notFoundOr(err)
		}
		if row.Pending {
			return ErrConflict
		}
		task, err = unmarshalSessionTask(row.Data)
		if err != nil {
			return err
		}
		apia2a.SetTaskCreatedAt(task, row.CreatedAt)
		return loadSessionTaskHistories(ctx, tx, row.HistoryID, []*a2a.Task{task}, historyLength, false)
	})
	if err != nil {
		return nil, fmt.Errorf("read public Session task %s: %w", taskID, err)
	}
	return task, nil
}

// ListSessionTasks returns tasks with archived messages in immutable creation order
// after afterID, with optional state and exclusive status-timestamp filters. The total
// counts all matching tasks before pagination; it is read separately and can differ under
// concurrent writes. History limits have the same semantics as GetSessionTask.
// Callers authorize session access.
func (c *Client) ListSessionTasks(ctx context.Context, sessionID, afterID string, state a2a.TaskState, statusTimestampAfter *time.Time, limit int, historyLength *int) ([]*a2a.Task, int, error) {
	session, err := readSession(ctx, c.db, sessionID)
	if err != nil {
		return nil, 0, fmt.Errorf("get Session history: %w", notFoundOr(err))
	}

	total, err := queryOne(ctx, c.db, `
		SELECT COUNT(*) FROM session_task
		WHERE history_id = $1
		  AND ($2::text = '' OR state = $2)
		  AND ($3::timestamptz IS NULL
		       OR status_timestamp > $3)
	`, pgx.RowTo[int64], session.HistoryID, string(state), statusTimestampAfter)
	if err != nil {
		return nil, 0, fmt.Errorf("count Session tasks: %w", err)
	}
	rows, err := queryMany(ctx, c.db, `
		SELECT t.history_id, t.id, t.state, t.status_timestamp, t.data, t.created_at,
		    t.snapshot_atespace, t.snapshot_uri, t.snapshot_content_scope,
		    t.history_sequence, t.position FROM session_task t
		WHERE t.history_id = $1
		  AND ($2::text = '' OR t.position > (
		      SELECT cursor.position FROM session_task cursor
		      WHERE cursor.history_id = $1 AND cursor.id = $2
		  ))
		  AND ($3::text = '' OR t.state = $3)
		  AND ($4::timestamptz IS NULL
		       OR t.status_timestamp > $4)
		ORDER BY t.position
		LIMIT $5
	`,
		pgx.RowToStructByName[sessionTaskRow], session.HistoryID, afterID, string(state), statusTimestampAfter,
		int32(limit),
	)
	if err != nil {
		return nil, 0, fmt.Errorf("list Session tasks: %w", err)
	}
	tasks := make([]*a2a.Task, 0, len(rows))
	for _, row := range rows {
		task, err := unmarshalSessionTask(row.Data)
		if err != nil {
			return nil, 0, fmt.Errorf("decode Session task %s: %w", row.ID, err)
		}
		apia2a.SetTaskCreatedAt(task, row.CreatedAt)
		tasks = append(tasks, task)
	}
	if err := loadSessionTaskHistories(ctx, c.db, session.HistoryID, tasks, historyLength, false); err != nil {
		return nil, 0, err
	}
	return tasks, int(total), nil
}

// unmarshalSessionTaskEvent decodes a stored A2A event, returning an error for
// malformed or unsupported payloads.
func unmarshalSessionTaskEvent(data []byte) (a2a.Event, error) {
	var pb a2apb.StreamResponse
	if err := proto.Unmarshal(data, &pb); err != nil {
		return nil, fmt.Errorf("unmarshal Session task event: %w", err)
	}
	event, err := pbconv.FromProtoStreamResponse(&pb)
	if err != nil {
		return nil, fmt.Errorf("convert Session task event: %w", err)
	}
	return event, nil
}

// sessionTaskEventMessages selects the messages contributed by an event: the message
// itself, a task's history, or the latest history message for a status update. Other
// events contribute no messages.
func sessionTaskEventMessages(task *a2a.Task, event a2a.Event) []*a2a.Message {
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

// storeSessionTaskMessages converts and archives messages with valid IDs. The caller supplies a
// transaction when these writes must commit atomically with task state.
func storeSessionTaskMessages(ctx context.Context, db dbExecutor, historyID uuid.UUID, taskID, contextID string, messages []*a2a.Message) error {
	converted := make([]*a2apb.Message, 0, len(messages))
	for _, message := range messages {
		if message == nil || message.ID == "" {
			return fmt.Errorf("session task history contains a message without an ID")
		}
		event, err := pbconv.ToProtoStreamResponse(message)
		if err != nil {
			return err
		}
		converted = append(converted, event.GetMessage())
	}
	return storeProtoTaskMessages(ctx, db, historyID, taskID, contextID, converted)
}

// storeProtoTaskMessages archives messages without changing the inputs or dropping unknown
// protobuf fields. It fills missing task/context IDs and rejects conflicting identities.
// Duplicate message IDs retain their original event sequence.
// Callers own the transaction.
func storeProtoTaskMessages(ctx context.Context, db dbExecutor, historyID uuid.UUID, taskID, contextID string, messages []*a2apb.Message) error {
	for _, message := range messages {
		if message.GetMessageId() == "" {
			return fmt.Errorf("session task history contains a message without an ID")
		}
		message = proto.Clone(message).(*a2apb.Message)
		if (message.TaskId != "" && message.TaskId != taskID) || (message.ContextId != "" && message.ContextId != contextID) {
			return fmt.Errorf("history message changes task identity")
		}
		message.TaskId, message.ContextId = taskID, contextID
		data, err := proto.Marshal(&a2apb.StreamResponse{Payload: &a2apb.StreamResponse_Message{Message: message}})
		if err != nil {
			return err
		}
		_, err = insertTaskEvent(ctx, db, taskEventWrite{
			HistoryID: historyID,
			TaskID:    taskID,
			MessageID: &message.MessageId,
			Data:      data,
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// loadSessionTaskHistories attaches archived messages to the supplied tasks in event
// order, limited to the latest historyLength per task. Zero clears history without a
// query; nil or negative loads it all. Tasks without archived messages otherwise retain
// their inline history, subject to the same limit. Malformed messages return errors.
func loadSessionTaskHistories(ctx context.Context, db dbExecutor, historyID uuid.UUID, tasks []*a2a.Task, historyLength *int, includeUnpublished bool) error {
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
		return fmt.Errorf("list Session task history: %w", err)
	}
	histories := make(map[string][]*a2a.Message, len(tasks))
	for _, row := range rows {
		event, err := unmarshalSessionTaskEvent(row.Data)
		if err != nil {
			return err
		}
		message, ok := event.(*a2a.Message)
		if !ok {
			return fmt.Errorf("session task history event is %T, not a message", event)
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
	return errors.As(err, &pgErr) && pgErr.ConstraintName == "session_one_active_task_idx"
}

// unmarshalSessionTask decodes a stored A2A task, returning an error for malformed
// or unsupported payloads.
func unmarshalSessionTask(data []byte) (*a2a.Task, error) {
	var pb a2apb.Task
	if err := proto.Unmarshal(data, &pb); err != nil {
		return nil, fmt.Errorf("unmarshal Session task: %w", err)
	}
	task, err := pbconv.FromProtoTask(&pb)
	if err != nil {
		return nil, fmt.Errorf("convert Session task: %w", err)
	}
	return task, nil
}

type sessionTaskRow struct {
	HistoryID            uuid.UUID
	ID                   string
	State                string
	StatusTimestamp      *time.Time
	Data                 []byte
	CreatedAt            time.Time
	SnapshotAtespace     *string
	SnapshotURI          *string
	SnapshotContentScope *string
	HistorySequence      *int64
	Position             int64
}

type sessionTaskEventRow struct {
	Sequence             int64
	HistoryID            uuid.UUID
	TaskID               string
	Data                 []byte
	CreatedAt            time.Time
	MessageID            *string
	TaskPosition         *int64
	SnapshotAtespace     *string
	SnapshotURI          *string
	SnapshotContentScope *string
}

type taskEventWrite struct {
	HistoryID            uuid.UUID
	TaskID               string
	MessageID            *string
	Data                 []byte
	SnapshotAtespace     *string
	SnapshotURI          *string
	SnapshotContentScope *string
	TaskPosition         *int64
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
		    INSERT INTO session_task_event
		        (history_id, task_id, message_id, data, snapshot_atespace, snapshot_uri, snapshot_content_scope,
		         task_position, created_at)
		    VALUES ($1, $2, $3, $4, $5, $6, $7, $8, COALESCE($9::timestamptz, NOW()))
		    ON CONFLICT (history_id, task_id, message_id)
		        WHERE message_id IS NOT NULL
		    DO NOTHING
		    RETURNING sequence
		)
		SELECT sequence FROM inserted
		UNION ALL
		SELECT sequence FROM session_task_event
		WHERE history_id = $1 AND task_id = $2 AND message_id = $3
		LIMIT 1
	`,
		pgx.RowTo[int64], event.HistoryID, event.TaskID, event.MessageID, event.Data, event.SnapshotAtespace,
		event.SnapshotURI, event.SnapshotContentScope, event.TaskPosition,
		event.CreatedAt,
	)
}

// readSessionTask reads a task's stored projection without decoding or attaching
// history. Missing tasks return pgx.ErrNoRows; callers authorize access.
func readSessionTask(ctx context.Context, db dbExecutor, historyID uuid.UUID, taskID string) (sessionTaskRow, error) {
	return queryOne(ctx, db, `
		SELECT history_id, id, state, status_timestamp, data, created_at, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence, position FROM
		    session_task
		WHERE history_id = $1 AND id = $2
	`, pgx.RowToStructByName[sessionTaskRow], historyID, taskID)
}

// saveTaskProjection inserts or replaces current task state while preserving existing
// creation and snapshot metadata. Callers validate the transition and persist its
// events in the same transaction.
func saveTaskProjection(ctx context.Context, db dbExecutor, historyID uuid.UUID, taskID, state string, statusTimestamp *time.Time, data []byte) (sessionTaskRow, error) {
	return queryOne(ctx, db, `
		INSERT INTO session_task (history_id, id, state, status_timestamp, data)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (history_id, id) DO UPDATE SET
		    state = EXCLUDED.state,
		    status_timestamp = EXCLUDED.status_timestamp,
		    data = EXCLUDED.data
		RETURNING history_id, id, state, status_timestamp, data, created_at, snapshot_atespace, snapshot_uri, snapshot_content_scope, history_sequence, position
	`, pgx.RowToStructByName[sessionTaskRow], historyID, taskID, state, statusTimestamp, data)
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
		    FROM session_task_event
		    WHERE history_id = $1 AND task_id = tasks.task_id AND message_id IS NOT NULL
		      AND (published OR $4::boolean)
		    ORDER BY sequence DESC
		    LIMIT $3
		) messages
		ORDER BY messages.sequence
	`, pgx.RowToStructByName[taskHistoryRow], historyID, taskIDs, historyLength, includeUnpublished)
}

// requireSettledRuntime rejects unfinished native cleanup, claimed idle work, or
// another unexpired dispatch. A writer may pass its own dispatch ID.
// Callers hold the session lock so new execution and lifecycle claims cannot race.
func requireSettledRuntime(ctx context.Context, db dbExecutor, historyID uuid.UUID, dispatchID string) error {
	pending, err := queryOne(ctx, db, `
		SELECT EXISTS (SELECT 1 FROM session_task_event WHERE history_id = $1
		    AND (NOT published OR (quiescence_pending AND quiescence_executor_id IS NOT NULL)))
		    OR EXISTS (SELECT 1 FROM session WHERE history_id = $1 AND dispatch_expires_at > clock_timestamp()
		        AND ($2::text = '' OR dispatch_id::text <> $2))
	`, pgx.RowTo[bool], historyID, dispatchID)
	if err != nil {
		return err
	}
	if pending {
		return fmt.Errorf("runtime cleanup or lifecycle work is not yet settled: %w", ErrFailedPrecondition)
	}
	return nil
}

// GetSessionTaskByMessage finds the task containing an input after a runtime
// disconnects before returning its unary response. Ambiguous message IDs fail;
// this read never deduplicates sends or authorizes another execution.
func (c *Client) GetSessionTaskByMessage(ctx context.Context, sessionID, taskID, messageID string) (*a2a.Task, error) {
	session, err := readSession(ctx, c.db, sessionID)
	if err != nil {
		return nil, notFoundOr(err)
	}
	ids, err := queryMany(ctx, c.db, `
  SELECT task_id FROM session_task_event
  WHERE history_id = $1 AND ($2::text = '' OR task_id = $2) AND message_id = $3
  LIMIT 2
 `, pgx.RowTo[string], session.HistoryID, taskID, messageID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, ErrNotFound
	}
	if len(ids) != 1 {
		return nil, ErrConflict
	}
	return c.GetSessionTask(ctx, sessionID, ids[0], nil)
}
