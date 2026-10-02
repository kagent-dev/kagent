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
// PushRegistration is the receipt shape used by the recovery worker.
type PushRegistration struct {
	ID               int64
	HistoryID        uuid.UUID
	InitialMessageID string
	SessionID        string
	ConfigID         string
	URL              string
	CreatedAt        time.Time
	TaskID           *string
}

// RegisterSessionPush saves the callback supplied with SendMessage before
// dispatch. The message may name an existing task, but the receipt remains
// unbound until a task write proves that task accepted this input. Its original
// request fingerprint survives later edits or deletion, so retrying the send
// cannot restore old settings or attach the callback to another task.
func (c *Client) RegisterSessionPush(ctx context.Context, sessionID, messageID, taskID string, config *a2a.PushConfig) error {
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
                (history_id, initial_message_id, initial_request_hash, config_id, url, token, auth_credentials)
            VALUES ($1, $2, $3, $4, $5, $6, $7)
            ON CONFLICT (history_id, initial_message_id) WHERE initial_message_id IS NOT NULL
            DO UPDATE SET config_id = session_push_registration.config_id
            WHERE session_push_registration.initial_request_hash = EXCLUDED.initial_request_hash
        `, session.HistoryID, messageID, requestHash, config.ID, config.URL, config.Token, pushCredentials(config))
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrIdempotencyConflict
		}
		return nil
	})
}

// ListOpenPushRegistrations scans open rows for the recovery worker. It pages
// over bound rows too; the worker skips those and resolves only pending inputs.
func (c *Client) ListOpenPushRegistrations(ctx context.Context, after *PushRegistration, limit int) ([]PushRegistration, error) {
	var afterID int64
	if after != nil {
		afterID = after.ID
	}
	return queryMany(ctx, c.db, `
        SELECT p.id, p.history_id, COALESCE(p.initial_message_id, '') AS initial_message_id,
            s.id::text AS session_id, p.config_id, p.url, p.created_at, p.task_id
        FROM session_push_registration p JOIN session_record s ON s.history_id = p.history_id
        WHERE p.closed_at IS NULL AND s.state <> 'RUNTIME_STATE_DELETED' AND p.id > $1
        ORDER BY p.id LIMIT $2
    `, pgx.RowToStructByName[PushRegistration], afterID, limit)
}

// BindSessionPush is the recovery path when a receipt is still unbound after
// its input was accepted. The normal path binds inside the task-write transaction.
func (c *Client) BindSessionPush(ctx context.Context, registration PushRegistration, taskID string) (bool, error) {
	var bound bool
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, registration.SessionID)
		if err != nil {
			return notFoundOr(err)
		}
		if session.HistoryID != registration.HistoryID || session.State == "RUNTIME_STATE_DELETED" {
			return nil
		}
		bound, err = bindPushRegistration(ctx, tx, registration.ID, taskID)
		return err
	})
	return err == nil && bound, err
}

// CloseUnboundSessionPush consumes a receipt whose input never resolved to one task.
func (c *Client) CloseUnboundSessionPush(ctx context.Context, registration PushRegistration) error {
	return execSQL(ctx, c.db, `
        UPDATE session_push_registration SET closed_at = clock_timestamp()
        WHERE id = $1 AND task_id IS NULL AND closed_at IS NULL
    `, registration.ID)
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
    `, pgx.RowTo[int64], historyID, taskID)
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
// recovery worker. A closed receipt stays closed. If an explicit Create has
// already claimed the same public ID, it wins; closing this embedded row still
// retains its fingerprint for retries of the original send.
func bindPushRegistration(ctx context.Context, tx pgx.Tx, id int64, taskID string) (bool, error) {
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
		// An embedded send might have been registered after the accepting task
		// write (for example, on a retry). Bind it before checking for this ID.
		if err := bindInitialPushForTask(ctx, tx, session.HistoryID, taskID); err != nil {
			return err
		}
		type existing struct {
			ID              int64
			URL             string
			Token           string
			AuthCredentials string
			Revision        int64
		}
		row, err := queryOne(ctx, tx, `
            SELECT id, url, token, auth_credentials, revision FROM session_push_registration
            WHERE history_id = $1 AND task_id = $2 AND config_id = $3 AND closed_at IS NULL
            FOR UPDATE
        `, pgx.RowToStructByName[existing], session.HistoryID, taskID, config.ID)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			err = execSQL(ctx, tx, `
                INSERT INTO session_push_registration (history_id, task_id, config_id, url, token, auth_credentials)
                VALUES ($1, $2, $3, $4, $5, $6)
            `, session.HistoryID, taskID, config.ID, config.URL, config.Token, pushCredentials(config))
		case err != nil:
			return err
		case row.URL == config.URL && row.Token == config.Token && row.AuthCredentials == pushCredentials(config):
			return nil
		default:
			// Pending snapshots still target the old destination. Cancel them
			// before advancing the revision; the next publication uses the new one.
			if err := cancelPushDeliveries(ctx, tx, row.ID); err != nil {
				return err
			}
			err = execSQL(ctx, tx, `
                UPDATE session_push_registration SET url = $2, token = $3, auth_credentials = $4,
                    revision = revision + 1 WHERE id = $1
            `, row.ID, config.URL, config.Token, pushCredentials(config))
		}
		return err
	})
}

func pushCredentials(config *a2a.PushConfig) string {
	if config.Auth == nil {
		return ""
	}
	return config.Auth.Credentials
}

// The original send must remain distinguishable from later edits to its
// active callback. JSON encodes the fixed-order tuple without delimiter
// ambiguity, including the optional task reference and credentials.
func pushRegistrationHash(taskID string, config *a2a.PushConfig) ([]byte, error) {
	encoded, err := json.Marshal([5]string{taskID, config.ID, config.URL, config.Token, pushCredentials(config)})
	if err != nil {
		return nil, fmt.Errorf("failed to encode initial push configuration: %w", err)
	}
	hash := sha256.Sum256(encoded)
	return hash[:], nil
}

// TaskPushConfig is a stored active configuration; closed receipts are hidden.
type TaskPushConfig struct {
	ID    string
	URL   string
	Token string
}

// GetTaskPushConfig reads one active config, including an embedded registration
// that has not yet been associated by the polling worker.
func (c *Client) GetTaskPushConfig(ctx context.Context, sessionID, taskID, configID string) (*TaskPushConfig, error) {
	session, err := readSession(ctx, c.db, sessionID)
	if err != nil {
		return nil, notFoundOr(err)
	}
	row, err := queryOne(ctx, c.db, `
        SELECT config_id AS id, url, token FROM session_push_registration p
        WHERE p.history_id = $1 AND p.config_id = $3 AND p.closed_at IS NULL
            AND (p.task_id = $2 OR (p.task_id IS NULL AND EXISTS (
                SELECT 1 FROM session_task_event e WHERE e.history_id = p.history_id
                    AND e.message_id = p.initial_message_id AND e.task_id = $2
            ) AND NOT EXISTS (
                SELECT 1 FROM session_task_event e WHERE e.history_id = p.history_id
                    AND e.message_id = p.initial_message_id AND e.task_id <> $2
            )))
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
        SELECT config_id AS id, url, token FROM session_push_registration p
        WHERE p.history_id = $1 AND p.closed_at IS NULL AND p.config_id > $3
            AND (p.task_id = $2 OR (p.task_id IS NULL AND EXISTS (
                SELECT 1 FROM session_task_event e WHERE e.history_id = p.history_id
                    AND e.message_id = p.initial_message_id AND e.task_id = $2
            ) AND NOT EXISTS (
                SELECT 1 FROM session_task_event e WHERE e.history_id = p.history_id
                    AND e.message_id = p.initial_message_id AND e.task_id <> $2
            )))
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
            WHERE p.history_id = $1 AND p.config_id = $3 AND p.closed_at IS NULL
                AND (p.task_id = $2 OR (p.task_id IS NULL AND EXISTS (
                    SELECT 1 FROM session_task_event e WHERE e.history_id = p.history_id
                        AND e.message_id = p.initial_message_id AND e.task_id = $2
                ) AND NOT EXISTS (
                    SELECT 1 FROM session_task_event e WHERE e.history_id = p.history_id
                        AND e.message_id = p.initial_message_id AND e.task_id <> $2
			)))
            RETURNING p.id
        `, pgx.RowTo[int64], session.HistoryID, taskID, configID)
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
