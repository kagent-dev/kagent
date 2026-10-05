package database

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// An embedded callback starts as a receipt for one input message. It gains a
// task ID only when that task accepts the message. Explicit Create already has
// a task ID and stores a bound row through SaveTaskPushConfig instead.
// Binding never queues delivery: SettleSessionTask inserts outbox rows when it
// publishes a later eligible task state.
//
// RegisterSessionPushNotification saves the callback supplied with SendMessage before
// dispatch. The message may name an existing task, but the receipt remains
// unbound until a task write proves that task accepted this input. Its original
// request fingerprint survives later edits or deletion, so retrying the send
// cannot restore old settings or attach the callback to another task.
func (c *Client) RegisterSessionPushNotification(ctx context.Context, sessionID, messageID, taskID string, config *a2a.PushConfig) error {
	requestHash, err := pushRegistrationHash(taskID, config)
	if err != nil {
		return err
	}
	return c.withTx(ctx, func(tx pgx.Tx) error {
		// Task publication takes this lock too, so an already-terminal task
		// cannot acquire a new callback while registration is in progress.
		session, err := lockSession(ctx, tx, sessionID)
		if err != nil {
			return notFoundOr(err)
		}
		if session.State == "RUNTIME_STATE_DELETED" {
			return ErrNotFound
		}
		if taskID != "" {
			row, err := readSessionTask(ctx, tx, session.HistoryID, taskID)
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrNotFound
			}
			if err != nil {
				return err
			}
			if a2a.TaskState(row.State).Terminal() {
				return fmt.Errorf("cannot register a callback on a terminal task: %w", ErrFailedPrecondition)
			}
		} else {
			// A retry with an existing receipt is safe, but a new callback on an
			// already-completed initial send would silently subscribe to no updates.
			terminal, err := queryOne(ctx, tx, `
                SELECT EXISTS (
                    SELECT 1 FROM session_task_event e
                    JOIN session_task t ON t.history_id = e.history_id AND t.id = e.task_id
                    WHERE e.history_id = $1 AND e.message_id = $2
                        AND t.state IN ('TASK_STATE_COMPLETED', 'TASK_STATE_FAILED',
                            'TASK_STATE_CANCELED', 'TASK_STATE_REJECTED')
                        AND NOT EXISTS (
                            SELECT 1 FROM session_push_registration p
                            WHERE p.history_id = $1 AND p.initial_message_id = $2
                        )
                )
            `, pgx.RowTo[bool], session.HistoryID, messageID)
			if err != nil {
				return err
			}
			if terminal {
				return fmt.Errorf("cannot register a callback on a terminal task: %w", ErrFailedPrecondition)
			}
		}
		tag, err := tx.Exec(ctx, `
            INSERT INTO session_push_registration
                (id, history_id, initial_message_id, initial_request_hash, config_id, url)
            VALUES ($1, $2, $3, $4, $5, $6)
            ON CONFLICT (history_id, initial_message_id) WHERE initial_message_id IS NOT NULL
            DO UPDATE SET config_id = session_push_registration.config_id
            WHERE session_push_registration.initial_request_hash = EXCLUDED.initial_request_hash
        `, uuid.New(), session.HistoryID, messageID, requestHash, config.ID, config.URL)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrIdempotencyConflict
		}
		return bindAcceptedPushMessage(ctx, tx, session.HistoryID, messageID)
	})
}

// bindAcceptedPushMessage covers a retry whose task write committed before
// registration. Both operations hold the Session lock, so they cannot miss each other.
func bindAcceptedPushMessage(ctx context.Context, tx pgx.Tx, historyID uuid.UUID, messageID string) error {
	taskIDs, err := queryMany(ctx, tx, `
        SELECT DISTINCT task_id FROM session_task_event
        WHERE history_id = $1 AND message_id = $2 LIMIT 2
    `, pgx.RowTo[string], historyID, messageID)
	if err != nil || len(taskIDs) == 0 {
		return err
	}
	if len(taskIDs) > 1 {
		return execSQL(ctx, tx, `
            UPDATE session_push_registration SET closed_at = clock_timestamp()
            WHERE history_id = $1 AND initial_message_id = $2 AND task_id IS NULL AND closed_at IS NULL
        `, historyID, messageID)
	}
	return bindInitialPushForTask(ctx, tx, historyID, taskIDs[0])
}

// ExpireUnboundPushRegistrations closes receipts whose send never accepted a task.
func (c *Client) ExpireUnboundPushRegistrations(ctx context.Context) error {
	return execSQL(ctx, c.db, `
        UPDATE session_push_registration SET closed_at = clock_timestamp()
        WHERE id IN (SELECT id FROM session_push_registration
            WHERE task_id IS NULL AND closed_at IS NULL
                AND created_at < clock_timestamp() - interval '10 minutes'
            ORDER BY created_at, id LIMIT 100)
    `)
}

// bindInitialPushForTask runs after the task write records accepted message
// events. It binds all matching embedded receipts in that same transaction,
// whether the send named this task or only its conversation. A published task
// update can then find the callback by task ID without a worker round trip.
func bindInitialPushForTask(ctx context.Context, tx pgx.Tx, historyID uuid.UUID, taskID string) error {
	// An accepted message must identify this task and no other task in the
	// Session. Merely naming taskID in the request does not establish ownership.
	ids, err := queryMany(ctx, tx, `
        SELECT p.id FROM session_push_registration p
        WHERE p.history_id = $1 AND p.task_id IS NULL AND p.closed_at IS NULL
            AND p.initial_message_id IS NOT NULL
            AND EXISTS (SELECT 1 FROM session_task_event e WHERE e.history_id = p.history_id
                AND e.message_id = p.initial_message_id AND e.task_id = $2)
            AND NOT EXISTS (SELECT 1 FROM session_task_event e WHERE e.history_id = p.history_id
                AND e.message_id = p.initial_message_id AND e.task_id <> $2)
        ORDER BY p.id
    `, pgx.RowTo[uuid.UUID], historyID, taskID)
	if err != nil {
		return err
	}
	for _, id := range ids {
		if _, err := bindPushRegistration(ctx, tx, id, taskID); err != nil {
			return err
		}
	}
	return nil
}

// bindPushRegistration performs the association found by the task writer or
// registration. A closed receipt stays closed. If an explicit Create has
// already claimed the same public ID, it wins; closing this embedded row still
// retains its fingerprint for retries of the original send.
func bindPushRegistration(ctx context.Context, tx pgx.Tx, id uuid.UUID, taskID string) (bool, error) {
	type receipt struct {
		HistoryID uuid.UUID
		ConfigID  string
		TaskID    *string
		ClosedAt  *time.Time
	}
	row, err := queryOne(ctx, tx, `
        SELECT history_id, config_id, task_id, closed_at
        FROM session_push_registration WHERE id = $1 FOR UPDATE
    `, pgx.RowToStructByName[receipt], id)
	if err != nil {
		return false, err
	}
	if row.ClosedAt != nil {
		return false, nil
	}
	if row.TaskID != nil {
		return *row.TaskID == taskID, nil
	}
	occupied, err := queryOne(ctx, tx, `
        SELECT EXISTS (SELECT 1 FROM session_push_registration
            WHERE history_id = $1 AND task_id = $2 AND config_id = $3 AND id <> $4)
    `, pgx.RowTo[bool], row.HistoryID, taskID, row.ConfigID, id)
	if err != nil {
		return false, err
	}
	if occupied {
		return false, execSQL(ctx, tx, `
            UPDATE session_push_registration SET closed_at = clock_timestamp() WHERE id = $1
        `, id)
	}
	return true, execSQL(ctx, tx, `
        UPDATE session_push_registration SET task_id = $2 WHERE id = $1
    `, id, taskID)
}

// SaveTaskPushConfig creates or replaces a task configuration. Its revision
// fences queued notifications for an older destination.
func (c *Client) SaveTaskPushConfig(ctx context.Context, sessionID, taskID string, config *a2a.PushConfig) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, sessionID)
		if err != nil {
			return notFoundOr(err)
		}
		if session.State == "RUNTIME_STATE_DELETED" {
			return ErrNotFound
		}
		task, err := readSessionTask(ctx, tx, session.HistoryID, taskID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if a2a.TaskState(task.State).Terminal() {
			return fmt.Errorf("cannot register a callback on a terminal task: %w", ErrFailedPrecondition)
		}
		type existing struct {
			ID       uuid.UUID
			URL      string
			Revision int64
		}
		row, err := queryOne(ctx, tx, `
            SELECT id, url, revision FROM session_push_registration
            WHERE history_id = $1 AND task_id = $2 AND config_id = $3 AND closed_at IS NULL
            FOR UPDATE
        `, pgx.RowToStructByName[existing], session.HistoryID, taskID, config.ID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			err = execSQL(ctx, tx, `
                INSERT INTO session_push_registration (id, history_id, task_id, config_id, url)
                VALUES ($1, $2, $3, $4, $5)
            `, uuid.New(), session.HistoryID, taskID, config.ID, config.URL)
		case err != nil:
			return err
		case row.URL == config.URL:
			return nil
		default:
			// Pending snapshots still target the old destination. Cancel them
			// before advancing the revision; the next publication uses the new one.
			if err := cancelPushDeliveries(ctx, tx, row.ID); err != nil {
				return err
			}
			err = execSQL(ctx, tx, `
                UPDATE session_push_registration SET url = $2,
                    revision = revision + 1 WHERE id = $1
            `, row.ID, config.URL)
		}
		return err
	})
}

// The original send must remain distinguishable from later edits to its
// active callback. JSON encodes the fixed-order tuple without delimiter
// ambiguity, including the optional task reference.
func pushRegistrationHash(taskID string, config *a2a.PushConfig) ([]byte, error) {
	encoded, err := json.Marshal([3]string{taskID, config.ID, config.URL})
	if err != nil {
		return nil, fmt.Errorf("failed to encode initial push configuration: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return hash[:], nil
}

// TaskPushConfig is a stored active configuration; closed receipts are hidden.
type TaskPushConfig struct {
	ID  string
	URL string
}

// GetTaskPushConfig reads one active configuration.
func (c *Client) GetTaskPushConfig(ctx context.Context, sessionID, taskID, configID string) (*TaskPushConfig, error) {
	session, err := readSession(ctx, c.db, sessionID)
	if err != nil {
		return nil, notFoundOr(err)
	}
	row, err := queryOne(ctx, c.db, `
        SELECT config_id AS id, url FROM session_push_registration p
        WHERE p.history_id = $1 AND p.config_id = $3 AND p.closed_at IS NULL AND p.task_id = $2
    `, pgx.RowToStructByName[TaskPushConfig], session.HistoryID, taskID, configID)
	return &row, notFoundOr(err)
}

// ListTaskPushConfigs pages by configuration ID within the authorized task.
func (c *Client) ListTaskPushConfigs(ctx context.Context, sessionID, taskID, afterID string, limit int) ([]TaskPushConfig, error) {
	session, err := readSession(ctx, c.db, sessionID)
	if err != nil {
		return nil, notFoundOr(err)
	}
	return queryMany(ctx, c.db, `
        SELECT config_id AS id, url FROM session_push_registration p
        WHERE p.history_id = $1 AND p.closed_at IS NULL AND p.config_id > $3
            AND p.task_id = $2
        ORDER BY p.config_id LIMIT $4
    `, pgx.RowToStructByName[TaskPushConfig], session.HistoryID, taskID, afterID, limit)
}

// DeleteTaskPushConfig is idempotent. A closed receipt prevents initial send
// retries from restoring a deleted embedded callback.
func (c *Client) DeleteTaskPushConfig(ctx context.Context, sessionID, taskID, configID string) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, sessionID)
		if err != nil {
			return notFoundOr(err)
		}
		if session.State == "RUNTIME_STATE_DELETED" {
			return ErrNotFound
		}
		ids, err := queryMany(ctx, tx, `
            UPDATE session_push_registration p SET closed_at = clock_timestamp()
            WHERE p.history_id = $1 AND p.config_id = $3 AND p.closed_at IS NULL AND p.task_id = $2
            RETURNING p.id
        `, pgx.RowTo[uuid.UUID], session.HistoryID, taskID, configID)
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := cancelPushDeliveries(ctx, tx, id); err != nil {
				return err
			}
		}
		return nil
	})
}
