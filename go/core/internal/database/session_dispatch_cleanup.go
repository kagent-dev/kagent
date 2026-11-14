package database

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
)

const (
	// RuntimeLifecycleJobRecoverAbandonedDispatch recovers unused message delivery.
	RuntimeLifecycleJobRecoverAbandonedDispatch = "RECOVER_ABANDONED_DISPATCH"
	// RuntimeLifecycleJobRecovering permits recovery before suspension starts.
	RuntimeLifecycleJobRecovering = "RECOVERING"
	// RuntimeLifecycleJobSuspending records suspension intent across worker loss.
	RuntimeLifecycleJobSuspending = "SUSPENDING"

	dispatchCleanupMinimumClaim = time.Second
	dispatchCleanupParkInitial  = time.Minute
	dispatchCleanupParkMature   = 5 * time.Minute
	dispatchCleanupPruneBatch   = 64
)

// SessionDispatchCleanup identifies an abandoned delivery's runtime cleanup.
// Claims coordinate cleanup workers without blocking newer messages or lifecycle
// work. Every runtime step must revalidate the dispatch and executor.
type SessionDispatchCleanup struct {
	Session    *apiv1.Session
	DispatchID uuid.UUID
	ActorUID   string
	ExecutorID uuid.UUID
	// AvailableAt is the next scheduled claim time, or the owner's takeover deadline.
	AvailableAt time.Time
	// Suspending means suspension was authorized; replacements must not resume.
	Suspending bool
	boundary   *dispatchCleanupBoundary
}

type dispatchCleanupCandidate struct {
	SessionID  string
	DispatchID uuid.UUID
	ActorUID   string
}

type runtimeLifecycleJobClaim struct {
	Phase       string
	AvailableAt time.Time
}

type dispatchCleanupBoundary struct {
	TaskID  string
	Version int64
	State   string
}

// retireObsoleteSessionDispatchJobs removes a bounded batch after dispatch
// supersession or runtime deletion. Session locks keep an old observation from
// deleting a newly replaced job; temporarily blocked current work is retained.
func (c *Client) retireObsoleteSessionDispatchJobs(ctx context.Context) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		ids, err := queryMany(ctx, tx, `
			SELECT s.id FROM session s JOIN runtime_instance r USING (id)
			JOIN runtime_lifecycle_job j ON j.runtime_id = s.id AND j.kind = $1
			WHERE s.dispatch_id IS DISTINCT FROM j.generation OR r.state = 'RUNTIME_STATE_DELETED'
			ORDER BY s.id LIMIT $2 FOR UPDATE OF r, s SKIP LOCKED
		`, pgx.RowTo[uuid.UUID], RuntimeLifecycleJobRecoverAbandonedDispatch, dispatchCleanupPruneBatch)
		if err != nil || len(ids) == 0 {
			return err
		}
		return execSQL(ctx, tx, `
			DELETE FROM runtime_lifecycle_job j USING session s, runtime_instance r
			WHERE j.runtime_id = ANY($1::uuid[]) AND j.kind = $2
			  AND s.id = j.runtime_id AND r.id = s.id
			  AND (s.dispatch_id IS DISTINCT FROM j.generation OR r.state = 'RUNTIME_STATE_DELETED')
		`, ids, RuntimeLifecycleJobRecoverAbandonedDispatch)
	})
}

// enqueueSessionDispatchCleanup records revoked work under the Session lock.
// Rediscovery preserves the same generation's phase, claim, and parked schedule;
// a newer dispatch replaces obsolete work without waiting for its old owner.
func enqueueSessionDispatchCleanup(ctx context.Context, tx pgx.Tx, sessionID, dispatchID uuid.UUID) error {
	return execSQL(ctx, tx, `
		INSERT INTO runtime_lifecycle_job (runtime_id, kind, generation, phase, available_at)
		SELECT id, $3, dispatch_id, $4, clock_timestamp() FROM session
		WHERE id = $1 AND dispatch_id = $2 AND dispatch_expires_at <= clock_timestamp()
		ON CONFLICT (runtime_id, kind) DO UPDATE SET
		    generation = EXCLUDED.generation, phase = EXCLUDED.phase,
		    available_at = EXCLUDED.available_at, executor_id = NULL
		WHERE runtime_lifecycle_job.generation <> EXCLUDED.generation
	`, sessionID, dispatchID, RuntimeLifecycleJobRecoverAbandonedDispatch, RuntimeLifecycleJobRecovering)
}

// ClaimSessionDispatchCleanup finds and claims the next due abandoned dispatch.
// Discovery skips locked Sessions; eligibility is rechecked under the Session
// lock before creating a missing job or taking over an expired owner. Claims
// preserve suspension progress and capture history atomically. ErrNotFound means
// no eligible work. Pruning commits separately, even when no claim is available.
func (c *Client) ClaimSessionDispatchCleanup(ctx context.Context, afterSessionID string) (*SessionDispatchCleanup, error) {
	if afterSessionID == "" {
		if err := c.retireObsoleteSessionDispatchJobs(ctx); err != nil {
			return nil, err
		}
	}
	var result *SessionDispatchCleanup
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		candidate, err := lockNextDispatchCleanup(ctx, tx, afterSessionID)
		if err != nil {
			return err
		}
		session, err := readSession(ctx, tx, candidate.SessionID)
		if err != nil {
			return notFoundOr(err)
		}
		executor := uuid.New()
		claim, err := claimDispatchCleanupJob(ctx, tx, candidate, executor)
		if err != nil {
			return err
		}
		// Transfer pending task suspension to this durable dispatch. Older
		// controllers also honor this marker, so they cannot claim that task
		// while cleanup may need to resume the actor before suspending it.
		if err := execSQL(ctx, tx, `
			UPDATE session_task_event SET quiescence_pending = FALSE
			WHERE history_id = $1 AND quiescence_pending AND quiescence_executor_id IS NULL
		`, session.HistoryID); err != nil {
			return err
		}
		value, err := toSession(session)
		if err != nil {
			return err
		}
		result = &SessionDispatchCleanup{Session: value, DispatchID: candidate.DispatchID, ActorUID: candidate.ActorUID, ExecutorID: executor,
			Suspending: claim.Phase == RuntimeLifecycleJobSuspending, AvailableAt: claim.AvailableAt}
		result.boundary, err = readDispatchCleanupBoundary(ctx, tx, session.HistoryID)
		return err
	})
	return result, err
}

// lockNextDispatchCleanup selects an abandoned dispatch and locks its runtime and
// Session, skipping busy rows. The claim must recheck eligibility after this read.
// No candidate returns ErrNotFound; the caller owns the transaction.
func lockNextDispatchCleanup(ctx context.Context, tx pgx.Tx, afterSessionID string) (dispatchCleanupCandidate, error) {
	candidate, err := queryOne(ctx, tx, `
		SELECT s.id::text AS session_id, s.dispatch_id, s.actor_uid
		FROM session s JOIN runtime_instance r USING (id)
		LEFT JOIN runtime_lifecycle_job j ON j.runtime_id = s.id AND j.kind = $2
		WHERE s.dispatch_id IS NOT NULL
		  AND s.actor_uid IS NOT NULL
		  AND (j.runtime_id IS NULL OR j.generation <> s.dispatch_id
		      OR (j.phase IN ($3, $4) AND j.available_at <= clock_timestamp()))
		  AND ($1::text = '' OR s.id > NULLIF($1::text, '')::uuid)
		  AND r.state = 'RUNTIME_STATE_READY' AND r.operation = 'RUNTIME_OPERATION_NONE'
		  AND s.dispatch_expires_at <= clock_timestamp()
		  AND NOT EXISTS (SELECT 1 FROM session_task t WHERE t.history_id = s.history_id
		      AND t.state NOT IN ('TASK_STATE_COMPLETED', 'TASK_STATE_CANCELED', 'TASK_STATE_FAILED',
		          'TASK_STATE_REJECTED', 'TASK_STATE_INPUT_REQUIRED', 'TASK_STATE_AUTH_REQUIRED'))
		  AND NOT EXISTS (SELECT 1 FROM session_task_event e WHERE e.history_id = s.history_id
		      AND (NOT e.published OR (e.quiescence_pending AND e.quiescence_executor_id IS NOT NULL)))
		  AND NOT EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = s.id AND state = 'CREATING')
		  AND NOT EXISTS (SELECT 1 FROM runtime_lifecycle_job other WHERE other.runtime_id = s.id
		      AND other.kind <> $2 AND other.executor_id IS NOT NULL AND other.available_at > clock_timestamp())
		ORDER BY s.id LIMIT 1 FOR UPDATE OF r, s SKIP LOCKED
	`, pgx.RowToStructByName[dispatchCleanupCandidate], afterSessionID, RuntimeLifecycleJobRecoverAbandonedDispatch, RuntimeLifecycleJobRecovering, RuntimeLifecycleJobSuspending)
	return candidate, notFoundOr(err)
}

// claimDispatchCleanupJob rechecks eligibility under the Session lock and leases
// the job, retaining the same dispatch's phase. ErrNotFound means work is no longer
// due or eligible. Calculate the lifetime after locking so wait time is excluded.
func claimDispatchCleanupJob(ctx context.Context, tx pgx.Tx, candidate dispatchCleanupCandidate, executor uuid.UUID) (runtimeLifecycleJobClaim, error) {
	lifetime, err := dispatchCleanupLifetime(ctx)
	if err != nil {
		return runtimeLifecycleJobClaim{}, err
	}
	claim, err := queryOne(ctx, tx, `
		INSERT INTO runtime_lifecycle_job (runtime_id, kind, generation, phase, executor_id, available_at)
		SELECT s.id, $6, s.dispatch_id, $7, $4, clock_timestamp() + $5 * interval '1 microsecond'
		FROM session s JOIN runtime_instance r USING (id)
		WHERE s.id = $1 AND s.dispatch_id = $2 AND s.actor_uid = $3
		  AND r.state = 'RUNTIME_STATE_READY' AND r.operation = 'RUNTIME_OPERATION_NONE'
		  AND s.dispatch_expires_at <= clock_timestamp()
		  AND NOT EXISTS (SELECT 1 FROM session_task t WHERE t.history_id = s.history_id
		      AND t.state NOT IN ('TASK_STATE_COMPLETED', 'TASK_STATE_CANCELED', 'TASK_STATE_FAILED',
		          'TASK_STATE_REJECTED', 'TASK_STATE_INPUT_REQUIRED', 'TASK_STATE_AUTH_REQUIRED'))
		  AND NOT EXISTS (SELECT 1 FROM session_task_event e WHERE e.history_id = s.history_id
		      AND (NOT e.published OR (e.quiescence_pending AND e.quiescence_executor_id IS NOT NULL)))
		  AND NOT EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = s.id AND state = 'CREATING')
		  AND NOT EXISTS (SELECT 1 FROM runtime_lifecycle_job other WHERE other.runtime_id = s.id
		      AND other.kind <> $6 AND other.executor_id IS NOT NULL AND other.available_at > clock_timestamp())
		ON CONFLICT (runtime_id, kind) DO UPDATE SET
		    generation = EXCLUDED.generation, executor_id = EXCLUDED.executor_id,
		    available_at = EXCLUDED.available_at,
		    phase = CASE WHEN runtime_lifecycle_job.generation = EXCLUDED.generation
		        THEN runtime_lifecycle_job.phase ELSE EXCLUDED.phase END
		WHERE runtime_lifecycle_job.generation <> EXCLUDED.generation
		   OR (runtime_lifecycle_job.phase IN ($7, $8) AND runtime_lifecycle_job.available_at <= clock_timestamp())
		RETURNING phase, available_at
	`, pgx.RowToStructByName[runtimeLifecycleJobClaim], candidate.SessionID, candidate.DispatchID, candidate.ActorUID, executor, lifetime.Microseconds(),
		RuntimeLifecycleJobRecoverAbandonedDispatch, RuntimeLifecycleJobRecovering, RuntimeLifecycleJobSuspending)
	return claim, notFoundOr(err)
}

// readDispatchCleanupBoundary captures the latest task event under the Session
// lock. Nil means no task has ever started; tasks without history are malformed.
func readDispatchCleanupBoundary(ctx context.Context, tx pgx.Tx, historyID uuid.UUID) (*dispatchCleanupBoundary, error) {
	boundary, err := queryOne(ctx, tx, `
		SELECT e.task_id, e.sequence AS version, t.state FROM session_task_event e
		JOIN session_task t ON t.history_id = e.history_id AND t.id = e.task_id
		WHERE e.history_id = $1 ORDER BY e.sequence DESC LIMIT 1
	`, pgx.RowToStructByName[dispatchCleanupBoundary], historyID)
	if err == nil {
		return &boundary, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	hasTasks, err := queryOne(ctx, tx, `SELECT EXISTS (SELECT 1 FROM session_task WHERE history_id = $1)`, pgx.RowTo[bool], historyID)
	if err != nil {
		return nil, err
	}
	if hasTasks {
		return nil, fmt.Errorf("session task history is missing: %w", ErrFailedPrecondition)
	}
	return nil, nil
}

// ContinueSessionDispatchCleanup revalidates and renews ownership before a runtime
// step. Passing suspending records suspension intent for replacement workers.
// Superseded work returns ErrConflict; a missing Session returns ErrNotFound.
func (c *Client) ContinueSessionDispatchCleanup(ctx context.Context, work *SessionDispatchCleanup, suspending bool) error {
	if work == nil || work.Session == nil || work.DispatchID == uuid.Nil || work.ExecutorID == uuid.Nil || work.ActorUID == "" {
		return ErrFailedPrecondition
	}
	var availableAt time.Time
	var phase string
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := lockSession(ctx, tx, work.Session.Id); err != nil {
			return notFoundOr(err)
		}
		var err error
		phase, err = requireCurrentDispatchCleanup(ctx, tx, work)
		if err != nil {
			return err
		}
		lifetime, err := dispatchCleanupLifetime(ctx)
		if err != nil {
			return err
		}
		if suspending {
			phase = RuntimeLifecycleJobSuspending
		}
		availableAt, err = queryOne(ctx, tx, `
			UPDATE runtime_lifecycle_job SET
			    available_at = clock_timestamp() + $5 * interval '1 microsecond', phase = $6
			WHERE runtime_id = $1 AND generation = $2 AND executor_id = $3 AND kind = $4
			RETURNING available_at
		`, pgx.RowTo[time.Time], work.Session.Id, work.DispatchID, work.ExecutorID, RuntimeLifecycleJobRecoverAbandonedDispatch, lifetime.Microseconds(), phase)
		return err
	})
	if err == nil {
		work.AvailableAt, work.Suspending = availableAt, phase == RuntimeLifecycleJobSuspending
	}
	return err
}

// ParkSessionDispatchCleanup releases a recovering job after observing an older
// suspension. Its persisted observation schedule survives worker restarts and
// repeated revocation. Stale owners or suspension already started return ErrConflict.
func (c *Client) ParkSessionDispatchCleanup(ctx context.Context, work *SessionDispatchCleanup) error {
	if work == nil || work.Session == nil || work.DispatchID == uuid.Nil || work.ExecutorID == uuid.Nil || work.ActorUID == "" {
		return ErrFailedPrecondition
	}
	var availableAt time.Time
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		if _, err := lockSession(ctx, tx, work.Session.Id); err != nil {
			return notFoundOr(err)
		}
		phase, err := requireCurrentDispatchCleanup(ctx, tx, work)
		if err != nil {
			return err
		}
		if phase != RuntimeLifecycleJobRecovering {
			return ErrConflict
		}
		// Parked age starts at cancellation if it precedes expiry; otherwise it
		// starts at the original expiry, including recovery after controller loss.
		availableAt, err = queryOne(ctx, tx, `
			UPDATE runtime_lifecycle_job j SET executor_id = NULL,
			    available_at = clock_timestamp() + CASE
			        WHEN clock_timestamp() - s.dispatch_expires_at < $5 * interval '1 second'
			        THEN $6 * interval '1 second' ELSE $5 * interval '1 second' END
			FROM session s WHERE s.id = j.runtime_id
			  AND j.runtime_id = $1 AND j.generation = $2 AND j.executor_id = $3 AND j.kind = $4
			RETURNING j.available_at
		`, pgx.RowTo[time.Time], work.Session.Id, work.DispatchID, work.ExecutorID, RuntimeLifecycleJobRecoverAbandonedDispatch,
			dispatchCleanupParkMature.Seconds(), dispatchCleanupParkInitial.Seconds())
		return err
	})
	if err == nil {
		work.AvailableAt = availableAt
	}
	return err
}

// ReleaseSessionDispatchCleanup releases only the exact claiming owner and
// preserves the dispatch. The caller must stop issuing runtime calls first.
// Missing Sessions return ErrNotFound. Stale, mismatched, or already released
// claims return ErrConflict without changing the current owner's authority.
func (c *Client) ReleaseSessionDispatchCleanup(ctx context.Context, work *SessionDispatchCleanup) error {
	if work == nil || work.Session == nil || work.DispatchID == uuid.Nil || work.ExecutorID == uuid.Nil || work.ActorUID == "" {
		return ErrFailedPrecondition
	}
	return c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, work.Session.Id)
		if err != nil {
			return notFoundOr(err)
		}
		tag, err := tx.Exec(ctx, `
			DELETE FROM runtime_lifecycle_job j USING session s
			WHERE j.runtime_id = $1 AND j.generation = $2 AND j.executor_id = $3
			  AND s.id = j.runtime_id AND s.actor_uid = $4 AND j.kind = $5
		`, session.ID, work.DispatchID, work.ExecutorID, work.ActorUID, RuntimeLifecycleJobRecoverAbandonedDispatch)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
		return nil
	})
}

// FinishSessionDispatchCleanup records the claimed snapshot without inventing
// history or changing logical Session state. Snapshot and exact dispatch retirement
// commit together. Retired dispatches are no-ops; superseded work returns ErrConflict.
func (c *Client) FinishSessionDispatchCleanup(ctx context.Context, work *SessionDispatchCleanup, snapshot *SessionTaskSnapshot) error {
	if work == nil || work.Session == nil || work.DispatchID == uuid.Nil || work.ExecutorID == uuid.Nil || work.ActorUID == "" {
		return ErrFailedPrecondition
	}
	if snapshot == nil || snapshot.Atespace == "" || snapshot.URI == "" || snapshot.Fidelity == "" {
		return fmt.Errorf("dispatch cleanup requires a runtime snapshot: %w", ErrFailedPrecondition)
	}
	return c.withTx(ctx, func(tx pgx.Tx) error {
		session, err := lockSession(ctx, tx, work.Session.Id)
		if err != nil {
			return notFoundOr(err)
		}
		current, err := queryOne(ctx, tx, `
			SELECT dispatch_id FROM session WHERE id = $1
		`, pgx.RowTo[*uuid.UUID], session.ID)
		if err != nil {
			return err
		}
		if current == nil {
			return nil
		}
		phase, err := requireCurrentDispatchCleanup(ctx, tx, work)
		if err != nil {
			return err
		}
		if phase != RuntimeLifecycleJobSuspending {
			return ErrConflict
		}
		if err := saveDispatchCleanupSnapshot(ctx, tx, session.HistoryID, work.boundary, snapshot); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `
			UPDATE session SET dispatch_id = NULL, dispatch_expires_at = NULL
			WHERE id = $1 AND dispatch_id = $2
		`, session.ID, work.DispatchID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
		tag, err = tx.Exec(ctx, `
			DELETE FROM runtime_lifecycle_job
			WHERE runtime_id = $1 AND generation = $2 AND executor_id = $3 AND kind = $4
		`, session.ID, work.DispatchID, work.ExecutorID, RuntimeLifecycleJobRecoverAbandonedDispatch)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return ErrConflict
		}
		return nil
	})
}

// saveDispatchCleanupSnapshot attaches the snapshot only if task history still
// matches the claimed boundary. With no prior tasks, verify history remains empty.
// The caller holds the Session lock; changed history returns ErrConflict.
func saveDispatchCleanupSnapshot(ctx context.Context, tx pgx.Tx, historyID uuid.UUID, target *dispatchCleanupBoundary, snapshot *SessionTaskSnapshot) error {
	if target == nil {
		hasTasks, err := queryOne(ctx, tx, `SELECT EXISTS (SELECT 1 FROM session_task WHERE history_id = $1)`, pgx.RowTo[bool], historyID)
		if err != nil {
			return err
		}
		if hasTasks {
			return ErrConflict
		}
		return nil
	}
	latest, err := queryOne(ctx, tx, `SELECT MAX(sequence) FROM session_task_event WHERE history_id = $1`, pgx.RowTo[int64], historyID)
	if err != nil {
		return err
	}
	stored, err := readSessionTask(ctx, tx, historyID, target.TaskID)
	if err != nil {
		return err
	}
	if latest != target.Version || stored.State != target.State {
		return ErrConflict
	}
	return saveSessionTaskSnapshot(ctx, tx, historyID, target.TaskID, target.Version, snapshot)
}

// requireCurrentDispatchCleanup validates one owner's next step against delivery,
// runtime, task, and checkpoint state and returns its supported phase. The caller
// holds the Session lock. An obsolete or temporarily blocked owner returns ErrConflict.
func requireCurrentDispatchCleanup(ctx context.Context, tx pgx.Tx, work *SessionDispatchCleanup) (string, error) {
	phase, err := queryOne(ctx, tx, `
		SELECT j.phase FROM session s JOIN runtime_instance r USING (id)
		    JOIN runtime_lifecycle_job j ON j.runtime_id = s.id AND j.kind = $5
		    WHERE s.id = $1 AND s.dispatch_id = $2 AND s.actor_uid = $3
		      AND j.generation = $2 AND j.executor_id = $4 AND j.phase IN ($6, $7)
		      AND s.dispatch_expires_at <= clock_timestamp()
		      AND r.state = 'RUNTIME_STATE_READY' AND r.operation = 'RUNTIME_OPERATION_NONE'
		      AND NOT EXISTS (SELECT 1 FROM session_task t WHERE t.history_id = s.history_id
		          AND t.state NOT IN ('TASK_STATE_COMPLETED', 'TASK_STATE_CANCELED', 'TASK_STATE_FAILED',
		              'TASK_STATE_REJECTED', 'TASK_STATE_INPUT_REQUIRED', 'TASK_STATE_AUTH_REQUIRED'))
		      AND NOT EXISTS (SELECT 1 FROM session_task_event e WHERE e.history_id = s.history_id
		          AND (NOT e.published OR (e.quiescence_pending AND e.quiescence_executor_id IS NOT NULL)))
		      AND NOT EXISTS (SELECT 1 FROM session_checkpoint WHERE source_session_id = s.id AND state = 'CREATING')
		      AND NOT EXISTS (SELECT 1 FROM runtime_lifecycle_job other WHERE other.runtime_id = s.id
		          AND other.kind <> $5 AND other.executor_id IS NOT NULL AND other.available_at > clock_timestamp())
	`, pgx.RowTo[string], work.Session.Id, work.DispatchID, work.ActorUID, work.ExecutorID,
		RuntimeLifecycleJobRecoverAbandonedDispatch, RuntimeLifecycleJobRecovering, RuntimeLifecycleJobSuspending)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrConflict
	}
	return phase, err
}

// dispatchCleanupLifetime lets another worker take over after at most one runtime
// timeout, shortened by the remaining context duration. Rejecting attempts with
// less than a second remaining avoids short-lived claims without extending them.
func dispatchCleanupLifetime(ctx context.Context) (time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	lifetime := RuntimeOperationTimeout
	if deadline, ok := ctx.Deadline(); ok {
		lifetime = min(lifetime, time.Until(deadline))
	}
	if lifetime < dispatchCleanupMinimumClaim {
		return 0, context.DeadlineExceeded
	}
	return lifetime, nil
}

// retireSessionDispatchCleanup removes compensation after an
// authoritative lifecycle outcome takes responsibility. Callers hold the Session
// lock and must have checked admission or established runtime termination.
func retireSessionDispatchCleanup(ctx context.Context, tx pgx.Tx, sessionID uuid.UUID) error {
	if err := execSQL(ctx, tx, `
		UPDATE session SET dispatch_id = NULL, dispatch_expires_at = NULL
		WHERE id = $1
	`, sessionID); err != nil {
		return err
	}
	return execSQL(ctx, tx, `DELETE FROM runtime_lifecycle_job WHERE runtime_id = $1 AND kind = $2`, sessionID, RuntimeLifecycleJobRecoverAbandonedDispatch)
}
