package database

import (
	"context"
	"errors"
	"fmt"

	"github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"google.golang.org/protobuf/proto"
)

const pushMaxDeliveryAttempts = 10

// PushDelivery captures one attempt at the current published task. ID identifies
// the registration; Sequence and ClaimToken fence superseded and expired attempts.
type PushDelivery struct {
	ID         uuid.UUID
	Sequence   int64
	ClaimToken uuid.UUID
	TaskID     string
	URL        string
	Payload    []byte
}

// publishPushBoundary replaces pending work for this task's active registrations
// in the task publication transaction. A new generation resets retries and claims.
func publishPushBoundary(ctx context.Context, tx pgx.Tx, historyID uuid.UUID, taskID string, sequence int64, task *a2a.Task) error {
	if !pushBoundaryEligible(task.Status.State) {
		return nil
	}
	return execSQL(ctx, tx, `
        UPDATE session_push_registration SET pending_event_sequence = $3,
            attempt_count = 0, next_attempt_at = clock_timestamp(),
            claim_token = NULL, lease_until = NULL
        WHERE id IN (SELECT id FROM session_push_registration
            WHERE history_id = $1 AND task_id = $2 AND closed_at IS NULL
            ORDER BY id FOR UPDATE)
    `, historyID, taskID, sequence)
}

func pushBoundaryEligible(state a2a.TaskState) bool {
	return state.Terminal() || state == a2a.TaskStateInputRequired || state == a2a.TaskStateAuthRequired
}

// clearTaskPushDelivery invalidates obsolete waiting work atomically with an
// active task save. Callers hold the session lock; registrations lock in ID order.
func clearTaskPushDelivery(ctx context.Context, tx pgx.Tx, historyID uuid.UUID, taskID string) error {
	return execSQL(ctx, tx, `
        UPDATE session_push_registration SET pending_event_sequence = NULL,
            attempt_count = 0, next_attempt_at = NULL,
            claim_token = NULL, lease_until = NULL
        WHERE id IN (SELECT id FROM session_push_registration
            WHERE history_id = $1 AND task_id = $2 AND pending_event_sequence IS NOT NULL
            ORDER BY id FOR UPDATE)
    `, historyID, taskID)
}

// HasPendingPushDelivery includes future retries and leases awaiting recovery.
// Exhausted generations are retired and do not keep workers polling.
func (c *Client) HasPendingPushDelivery(ctx context.Context) (bool, error) {
	return queryOne(ctx, c.db, `
        SELECT EXISTS (
            SELECT 1 FROM session_push_registration p
            JOIN session_record s ON s.history_id = p.history_id
            WHERE p.pending_event_sequence IS NOT NULL AND p.closed_at IS NULL
              AND s.state <> 'RUNTIME_STATE_DELETED'
        )
    `, pgx.RowTo[bool])
}

// ClaimDuePushDelivery claims one due or abandoned registration and captures its
// current published status. Registration locks serialize replacement and claims;
// no session/task lock is acquired here and no lock is held across HTTP.
// Invalid task data commits a failed attempt before returning an error, allowing
// other registrations to proceed while this one waits for its retry deadline.
func (c *Client) ClaimDuePushDelivery(ctx context.Context) (*PushDelivery, error) {
	var delivery *PushDelivery
	var preparationErr error
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		// Recover a worker that died during its last allowed attempt without
		// issuing an eleventh request. Skip registrations another writer owns.
		if err := execSQL(ctx, tx, `
            UPDATE session_push_registration SET pending_event_sequence = NULL,
                attempt_count = 0, next_attempt_at = NULL, claim_token = NULL, lease_until = NULL
            WHERE id IN (SELECT id FROM session_push_registration
                WHERE claim_token IS NOT NULL AND attempt_count >= $1
                  AND lease_until <= clock_timestamp()
                ORDER BY id LIMIT 100 FOR UPDATE SKIP LOCKED)
        `, pushMaxDeliveryAttempts); err != nil {
			return err
		}
		type row struct {
			ID        uuid.UUID
			HistoryID uuid.UUID
			TaskID    string
			URL       string
			Sequence  int64
		}
		candidate, err := queryOne(ctx, tx, `
            SELECT p.id, p.history_id, p.task_id, p.url, p.pending_event_sequence AS sequence
            FROM session_push_registration p
            JOIN session_record s ON s.history_id = p.history_id
            WHERE p.pending_event_sequence IS NOT NULL AND p.closed_at IS NULL
              AND s.state <> 'RUNTIME_STATE_DELETED' AND p.attempt_count < $1
              AND ((p.claim_token IS NULL AND p.next_attempt_at <= clock_timestamp())
                OR (p.claim_token IS NOT NULL AND p.lease_until <= clock_timestamp()))
            ORDER BY p.next_attempt_at, p.id LIMIT 1
            FOR UPDATE OF p SKIP LOCKED
        `, pgx.RowToStructByName[row], pushMaxDeliveryAttempts)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		// Read after acquiring the registration lock: a publication that won
		// the lock must also have committed its matching task projection.
		stored, err := readSessionTask(ctx, tx, candidate.HistoryID, candidate.TaskID)
		if err != nil {
			return err
		}
		claimToken := uuid.New()
		if err := execSQL(ctx, tx, `
            UPDATE session_push_registration SET attempt_count = attempt_count + 1,
                claim_token = $2, lease_until = clock_timestamp() + interval '30 seconds'
            WHERE id = $1
        `, candidate.ID, claimToken); err != nil {
			return err
		}
		attempt := PushDelivery{ID: candidate.ID, Sequence: candidate.Sequence, ClaimToken: claimToken,
			TaskID: candidate.TaskID, URL: candidate.URL}
		payload, err := pushStatusPayload(stored.Data, candidate.TaskID)
		if err != nil {
			// Returning the preparation error from this transaction would undo
			// the retry deadline and leave the same bad row first in line.
			preparationErr = fmt.Errorf("failed to prepare push task %s: %w", candidate.TaskID, err)
			return retryPushDelivery(ctx, tx, attempt)
		}
		attempt.Payload = payload
		delivery = &attempt
		return nil
	})
	if err != nil {
		return nil, err
	}
	return delivery, preparationErr
}

// pushStatusPayload builds a status update from a valid published task projection.
// It rejects invalid task identities, ineligible states, and unarchived history.
func pushStatusPayload(data []byte, taskID string) ([]byte, error) {
	wireTask := &a2apb.Task{}
	if err := proto.Unmarshal(data, wireTask); err != nil {
		return nil, fmt.Errorf("failed to decode published task: %w", err)
	}
	task, err := pbconv.FromProtoTask(wireTask)
	if err != nil {
		return nil, fmt.Errorf("failed to convert published task: %w", err)
	}
	if !pushBoundaryEligible(task.Status.State) || string(task.ID) != taskID || len(wireTask.History) != 0 {
		return nil, fmt.Errorf("invalid published push task %s", taskID)
	}
	payload, err := proto.Marshal(&a2apb.StreamResponse{Payload: &a2apb.StreamResponse_StatusUpdate{
		StatusUpdate: &a2apb.TaskStatusUpdateEvent{
			TaskId: wireTask.Id, ContextId: wireTask.ContextId, Status: wireTask.Status,
		},
	}})
	if err != nil {
		return nil, fmt.Errorf("failed to encode push status update: %w", err)
	}
	return payload, nil
}

// IsPushDeliveryCurrent checks immediately before HTTP whether publication,
// resume, configuration edits, or lease recovery have superseded this attempt.
// A transition after this check can still make an in-flight request stale.
func (c *Client) IsPushDeliveryCurrent(ctx context.Context, delivery PushDelivery) (bool, error) {
	return queryOne(ctx, c.db, `
        SELECT EXISTS (SELECT 1 FROM session_push_registration p
            JOIN session_record s ON s.history_id = p.history_id
            WHERE p.id = $1 AND p.pending_event_sequence = $2 AND p.claim_token = $3
              AND p.closed_at IS NULL AND s.state <> 'RUNTIME_STATE_DELETED'
              AND p.lease_until > clock_timestamp())
    `, pgx.RowTo[bool], delivery.ID, delivery.Sequence, delivery.ClaimToken)
}

// FinishPushDelivery records only a matching generation and claim. Stale success
// and failure responses cannot clear newer work or change its retry schedule.
// Exhausted failures retire pending work without closing the registration.
func (c *Client) FinishPushDelivery(ctx context.Context, delivery PushDelivery, delivered bool) error {
	if delivered {
		return execSQL(ctx, c.db, `
            UPDATE session_push_registration SET pending_event_sequence = NULL,
                attempt_count = 0, next_attempt_at = NULL,
                claim_token = NULL, lease_until = NULL
            WHERE id = $1 AND pending_event_sequence = $2 AND claim_token = $3
        `, delivery.ID, delivery.Sequence, delivery.ClaimToken)
	}
	return retryPushDelivery(ctx, c.db, delivery)
}

// retryPushDelivery records a matching failed attempt through the pool or the
// claim transaction, retiring exhausted work while retaining its registration.
// Superseded claims do not change newer work or its backoff.
func retryPushDelivery(ctx context.Context, db dbExecutor, delivery PushDelivery) error {
	return execSQL(ctx, db, `
        UPDATE session_push_registration SET
            pending_event_sequence = CASE WHEN attempt_count >= $4 THEN NULL ELSE pending_event_sequence END,
            attempt_count = CASE WHEN attempt_count >= $4 THEN 0 ELSE attempt_count END,
            next_attempt_at = CASE WHEN attempt_count >= $4 THEN NULL
                ELSE clock_timestamp() + LEAST(interval '5 minutes',
                    interval '5 seconds' * power(2::double precision, LEAST(attempt_count - 1, 6))) END,
            claim_token = NULL, lease_until = NULL
        WHERE id = $1 AND pending_event_sequence = $2 AND claim_token = $3
    `, delivery.ID, delivery.Sequence, delivery.ClaimToken, pushMaxDeliveryAttempts)
}
