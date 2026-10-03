package database

import (
	"context"
	"errors"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
)

const pushMaxDeliveryAttempts = 10

// PushDelivery is an immutable attempt snapshot. The claim token fences a
// worker whose lease expired while it was sending HTTP.
type PushDelivery struct {
	ID              uuid.UUID
	ClaimToken      uuid.UUID
	URL             string
	Token           string
	AuthCredentials string
	Payload         []byte
}

// enqueuePushBoundary records a task status update for each active push registration.
func enqueuePushBoundary(ctx context.Context, tx pgx.Tx, historyID uuid.UUID, taskID string, sequence int64, task *a2a.Task) error {
	if !pushBoundaryEligible(task.Status.State) {
		return nil
	}
	type destination struct {
		ID              int64
		Revision        int64
		URL             string
		Token           string
		AuthCredentials string
	}
	configs, err := queryMany(ctx, tx, `
        SELECT id, revision, url, token, auth_credentials FROM session_push_registration
        WHERE history_id = $1 AND task_id = $2 AND closed_at IS NULL
        ORDER BY id
    `, pgx.RowToStructByName[destination], historyID, taskID)
	if err != nil {
		return err
	}
	for _, config := range configs {
		id := uuid.New()
		event := &a2a.TaskStatusUpdateEvent{
			TaskID: task.ID, ContextID: task.ContextID, Status: task.Status,
		}
		wire, err := pbconv.ToProtoStreamResponse(event)
		if err != nil {
			return err
		}
		payload, err := proto.Marshal(wire)
		if err != nil {
			return err
		}
		if err := execSQL(ctx, tx, `
            INSERT INTO session_push_outbox
                (id, registration_id, history_id, task_id, config_revision,
                 source_event_sequence, url, token, auth_credentials, payload)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
            ON CONFLICT (registration_id, config_revision, source_event_sequence) DO NOTHING
        `, id, config.ID, historyID, taskID, config.Revision, sequence, config.URL, config.Token, config.AuthCredentials, payload); err != nil {
			return err
		}
	}
	return nil
}

func pushBoundaryEligible(state a2a.TaskState) bool {
	return state.Terminal() || state == a2a.TaskStateInputRequired || state == a2a.TaskStateAuthRequired
}

// ClaimDuePushDelivery acquires one due or abandoned delivery without holding
// any lock across HTTP. The new claim token invalidates prior acknowledgements.
func (c *Client) ClaimDuePushDelivery(ctx context.Context) (*PushDelivery, error) {
	var delivery *PushDelivery
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		// A worker can die after claiming its final attempt. Retire that row
		// when its lease expires instead of sending an eleventh request.
		if err := execSQL(ctx, tx, `
            UPDATE session_push_outbox SET state = 'failed', claim_token = NULL,
                lease_until = NULL, last_error = 'delivery lease expired after final attempt'
            WHERE state = 'sending' AND attempt_count >= $1
              AND lease_until <= clock_timestamp()
        `, pushMaxDeliveryAttempts); err != nil {
			return err
		}
		type row struct {
			ID              uuid.UUID
			URL             string
			Token           string
			AuthCredentials string
			Payload         []byte
		}
		candidate, err := queryOne(ctx, tx, `
            SELECT o.id, o.url, o.token, o.auth_credentials, o.payload
            FROM session_push_outbox o
            JOIN session_push_registration p ON p.id = o.registration_id
            JOIN session_record s ON s.history_id = p.history_id
            WHERE p.closed_at IS NULL AND p.revision = o.config_revision
              AND s.state <> 'RUNTIME_STATE_DELETED'
              AND o.attempt_count < $1
              AND ((o.state = 'pending' AND o.next_attempt_at <= clock_timestamp())
                OR (o.state = 'sending' AND o.lease_until <= clock_timestamp()))
            ORDER BY o.next_attempt_at, o.id LIMIT 1
            FOR UPDATE OF o SKIP LOCKED
        `, pgx.RowToStructByName[row], pushMaxDeliveryAttempts)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		claimToken := uuid.New()
		if err := execSQL(ctx, tx, `
            UPDATE session_push_outbox SET state = 'sending', attempt_count = attempt_count + 1,
                claim_token = $2, lease_until = clock_timestamp() + interval '30 seconds',
                delivered_at = NULL, last_error = NULL
            WHERE id = $1
        `, candidate.ID, claimToken); err != nil {
			return err
		}
		delivery = &PushDelivery{ID: candidate.ID, ClaimToken: claimToken, URL: candidate.URL, Token: candidate.Token, AuthCredentials: candidate.AuthCredentials, Payload: candidate.Payload}
		return nil
	})
	return delivery, err
}

// FinishPushDelivery records a matching attempt. A stale worker cannot
// acknowledge a newer claim or undo an API cancellation.
func (c *Client) FinishPushDelivery(ctx context.Context, delivery PushDelivery, delivered bool) error {
	if delivered {
		return execSQL(ctx, c.db, `
            UPDATE session_push_outbox SET state = 'delivered', delivered_at = clock_timestamp(),
                claim_token = NULL, lease_until = NULL
            WHERE id = $1 AND claim_token = $2 AND state = 'sending'
        `, delivery.ID, delivery.ClaimToken)
	}
	return execSQL(ctx, c.db, `
        UPDATE session_push_outbox SET
            state = CASE WHEN attempt_count >= $3 THEN 'failed' ELSE 'pending' END,
            next_attempt_at = CASE WHEN attempt_count >= $3 THEN clock_timestamp()
                ELSE clock_timestamp() + LEAST(interval '5 minutes',
                    interval '5 seconds' * power(2::double precision, LEAST(attempt_count - 1, 6))) END,
            claim_token = NULL, lease_until = NULL, last_error = 'delivery failed'
        WHERE id = $1 AND claim_token = $2 AND state = 'sending'
    `, delivery.ID, delivery.ClaimToken, pushMaxDeliveryAttempts)
}

func cancelPushDeliveries(ctx context.Context, tx pgx.Tx, registrationID int64) error {
	return execSQL(ctx, tx, `
        UPDATE session_push_outbox SET state = 'canceled', claim_token = NULL,
            lease_until = NULL, delivered_at = NULL
        WHERE registration_id = $1 AND state IN ('pending', 'sending')
    `, registrationID)
}
