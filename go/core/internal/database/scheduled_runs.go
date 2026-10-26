package database

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/scheduling"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

// SyncScheduledRun reconciles a Kubernetes UID/generation into the durable
// scheduling projection. Stale generations and deleted UIDs cannot be revived.
// Only changes to cron, timezone, or pause state reset the next occurrence.
func (c *Client) SyncScheduledRun(ctx context.Context, request *ScheduledRun) (*ScheduledRun, error) {
	if request == nil {
		return nil, fmt.Errorf("schedule projection is required")
	}
	id, err := uuid.Parse(request.ID)
	if err != nil || request.Generation < 1 || request.Creator == "" || request.Ref.Namespace == "" || request.Ref.Name == "" || request.AgentUID == "" {
		return nil, fmt.Errorf("invalid schedule projection")
	}
	spec := scheduling.Normalize(request.Spec)
	if err := scheduling.Validate(spec); err != nil {
		return nil, fmt.Errorf("invalid schedule spec: %w", err)
	}
	if request.ResourceVersion == "" {
		return nil, fmt.Errorf("schedule resource version is required")
	}
	data, err := json.Marshal(scheduledRunSnapshot{Spec: spec, ResourceVersion: request.ResourceVersion, AgentUID: request.AgentUID})
	if err != nil {
		return nil, fmt.Errorf("failed to encode schedule: %w", err)
	}
	var result scheduledRunRow
	err = c.withTx(ctx, func(tx pgx.Tx) error {
		inserted, err := tx.Exec(ctx, `
   INSERT INTO scheduled_run (id, creator, namespace, name, generation, data)
   VALUES ($1, $2, $3, $4, $5, $6) ON CONFLICT (id) DO NOTHING
  `, id, request.Creator, request.Ref.Namespace, request.Ref.Name, request.Generation, data)
		if err != nil {
			return err
		}
		result, err = getScheduledRunForUpdate(ctx, tx, id, "")
		if err != nil {
			return err
		}
		if result.DeletedAt != nil || result.Generation > request.Generation {
			return nil
		}
		if result.Namespace != request.Ref.Namespace || result.Name != request.Ref.Name {
			return fmt.Errorf("schedule UID cannot change its resource reference: %w", ErrConflict)
		}
		var previous *ScheduledRun
		if inserted.RowsAffected() == 0 {
			previous, err = toScheduledRun(result)
			if err != nil {
				return err
			}
			if !result.Disabled && result.Generation == request.Generation && reflect.DeepEqual(previous.Spec, spec) && previous.Creator == request.Creator && previous.AgentUID == request.AgentUID && (result.NextExecutionTime != nil || *spec.Paused) {
				return nil
			}
		}
		next := result.NextExecutionTime
		if previous == nil || (next == nil && !*spec.Paused) || previous.Spec.Schedule != spec.Schedule || *previous.Spec.TimeZone != *spec.TimeZone || *previous.Spec.Paused != *spec.Paused {
			now, err := queryOne(ctx, tx, `SELECT statement_timestamp()::timestamptz`, pgx.RowTo[time.Time])
			if err != nil {
				return err
			}
			next, err = nextExecutionTime(spec, now)
			if err != nil {
				return err
			}
		}
		result, err = queryOne(ctx, tx, `
   UPDATE scheduled_run SET creator=$2, generation=$3, disabled=FALSE, data=$4, next_execution_time=$5, updated_at=CASE WHEN $6::boolean THEN created_at ELSE statement_timestamp() END
   WHERE id=$1 RETURNING id, creator, namespace, name, generation, disabled, data, created_at, updated_at, next_execution_time, deleted_at
  `, pgx.RowToStructByName[scheduledRunRow], id, request.Creator, request.Generation, data, next, inserted.RowsAffected() == 1)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to synchronize schedule: %w", err)
	}
	return toScheduledRun(result)
}

// GetScheduledRun reads a projection, including its retained tombstone. Callers
// authorize its trusted resource reference before returning protected content.
func (c *Client) GetScheduledRun(ctx context.Context, id uuid.UUID, creator string) (*ScheduledRun, error) {
	row, err := readScheduledRun(ctx, c.db, id, creator)
	if err != nil {
		return nil, fmt.Errorf("failed to get schedule: %w", notFoundOr(err))
	}
	return toScheduledRun(row)
}

// GetActiveScheduledRunID reads the active projection for a Kubernetes name,
// returning ErrNotFound when absent. Reconciliation uses this identity before
// checking the live resource, so a delayed deletion cannot retire a replacement.
func (c *Client) GetActiveScheduledRunID(ctx context.Context, ref types.NamespacedName) (uuid.UUID, error) {
	id, err := queryOne(ctx, c.db, `
  SELECT id FROM scheduled_run WHERE namespace=$1 AND name=$2 AND deleted_at IS NULL
 `, pgx.RowTo[uuid.UUID], ref.Namespace, ref.Name)
	return id, notFoundOr(err)
}

// RetireScheduledRun stops new reservations for a deleted UID, retaining accepted
// executions and history. Repeated retirement and unknown UIDs are harmless.
func (c *Client) RetireScheduledRun(ctx context.Context, id uuid.UUID) error {
	return execSQL(ctx, c.db, `
  UPDATE scheduled_run SET deleted_at=COALESCE(deleted_at, statement_timestamp()),
   next_execution_time=NULL, updated_at=CASE WHEN deleted_at IS NULL THEN statement_timestamp() ELSE updated_at END
  WHERE id=$1
 `, id)
}

// DisableScheduledRun removes an invalid or unresolved generation from the due
// queue and rejects new manual executions without deleting its history. The
// observed generation fences stale reconciliations; only a valid sync re-enables it.
func (c *Client) DisableScheduledRun(ctx context.Context, id uuid.UUID, generation int64) error {
	return execSQL(ctx, c.db, `UPDATE scheduled_run SET next_execution_time=NULL, disabled=TRUE, generation=$2 WHERE id=$1 AND generation <= $2`, id, generation)
}

// TriggerScheduledRun reserves a manual execution for an owned schedule, including when
// paused, without starting runtime work or changing the next scheduled occurrence. Reusing
// requestID for that schedule returns the same execution, even after deletion; new
// triggers on a deleted schedule return ErrFailedPrecondition.
func (c *Client) TriggerScheduledRun(ctx context.Context, id uuid.UUID, initiator, requestID string, generation int64) (*apiv1.ScheduledRunExecution, error) {
	var result scheduledRunExecutionRow
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := getScheduledRunForUpdate(ctx, tx, id, "")
		if err != nil {
			return err
		}
		result, err = queryOne(ctx, tx, `
			SELECT id, scheduled_run_id, scheduled_time, manual_request_id, data, created_at, deadline, session_id,
			    task_id, completed_at, state FROM scheduled_run_execution WHERE
			    scheduled_run_id = $1 AND manual_request_id = $2 AND initiator = $3
		`, pgx.RowToStructByName[scheduledRunExecutionRow], row.ID, &requestID, initiator)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if row.DeletedAt != nil || row.Disabled || generation == 0 {
			return fmt.Errorf("ScheduledRun %s is not reconciled for new executions: %w", id, ErrFailedPrecondition)
		}
		schedule, err := toScheduledRun(row)
		if err != nil {
			return err
		}
		if schedule.Generation != generation {
			return fmt.Errorf("schedule has not reconciled the requested generation: %w", ErrConflict)
		}
		result, err = reserveScheduledRunExecution(ctx, tx, schedule, nil, &requestID, initiator)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to trigger schedule: %w", err)
	}
	return toScheduledRunExecution(result)
}

// ListDueScheduledRuns reads up to limit due source identities in the watched
// namespaces in ID order, strictly after afterID (uuid.Nil starts the scan).
// An empty namespace list watches all namespaces. It does not lock
// rows or reserve work. Callers must validate these sources against Kubernetes
// before passing them to ReserveDueScheduledRuns. Limit must be between 1 and 100.
func (c *Client) ListDueScheduledRuns(ctx context.Context, namespaces []string, afterID uuid.UUID, limit int) ([]ScheduledRunSource, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("reservation limit must be between 1 and 100")
	}
	return queryMany(ctx, c.db, `
  SELECT id, namespace, name, generation FROM scheduled_run
  WHERE deleted_at IS NULL AND NOT disabled AND next_execution_time <= statement_timestamp()
   AND (COALESCE(cardinality($1::text[]), 0) = 0 OR namespace = ANY($1))
  AND id > $2
  ORDER BY id LIMIT $3
 `, pgx.RowToStructByName[ScheduledRunSource], namespaces, afterID, int32(limit))
}

// ReserveDueScheduledRuns atomically reserves due occurrences for up to 100
// Kubernetes-validated sources, advancing only unchanged generations and skipping
// rows held by other workers. It performs no external I/O within the transaction.
// Occurrences over thirty seconds late are skipped; malformed schedules are removed from
// the due queue with their payloads retained for repair. Execution workers claim the
// stored reservations separately.
func (c *Client) ReserveDueScheduledRuns(ctx context.Context, sources []ScheduledRunSource) error {
	if len(sources) == 0 {
		return nil
	}
	if len(sources) > 100 {
		return fmt.Errorf("reservation batch must contain at most 100 sources")
	}
	ids := make([]uuid.UUID, len(sources))
	generations := make([]int64, len(sources))
	for i, source := range sources {
		ids[i], generations[i] = source.ID, source.Generation
	}
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		rows, err := queryMany(ctx, tx, `
			SELECT scheduled_run.id, scheduled_run.creator, scheduled_run.namespace, scheduled_run.name, scheduled_run.generation, scheduled_run.disabled,
			    scheduled_run.data, scheduled_run.created_at, scheduled_run.updated_at, scheduled_run.next_execution_time,
			    scheduled_run.deleted_at, statement_timestamp()::timestamptz AS db_time FROM scheduled_run
			WHERE deleted_at IS NULL AND NOT disabled AND next_execution_time <= statement_timestamp()
			 AND (id, generation) IN (SELECT * FROM unnest($1::uuid[], $2::bigint[]))
			ORDER BY next_execution_time, id FOR UPDATE SKIP LOCKED
		`, pgx.RowToStructByName[dueScheduledRunRow], ids, generations)
		if err != nil {
			return err
		}
		for _, due := range rows {
			row, now := due.scheduledRunRow, due.DBTime
			schedule, err := toScheduledRun(row)
			var next *time.Time
			if err == nil {
				next, err = nextExecutionTime(schedule.Spec, now)
			}
			if err != nil {
				// Remove malformed schedules from the due queue so even a full
				// batch cannot starve healthy rows. Keep their payloads for repair.
				logging.FromContext(ctx).ErrorContext(ctx, "malformed schedule requires repair before scheduling can resume", "scheduled_run_id", row.ID, "error", err)
				if err := advanceScheduledRun(ctx, tx, row.ID, nil); err != nil {
					return err
				}
				continue
			}
			if now.Sub(*row.NextExecutionTime) <= 30*time.Second {
				_, err := reserveScheduledRunExecution(ctx, tx, schedule, row.NextExecutionTime, nil, "")
				if errors.Is(err, ErrFailedPrecondition) {
					// A concurrently retired or not-yet-prepared source remains retryable.
					continue
				}
				if err != nil {
					return err
				}
			} else {
				logging.FromContext(ctx).WarnContext(ctx, "skipping late scheduled occurrence", "scheduled_run_id", row.ID, "scheduled_time", *row.NextExecutionTime, "lateness", now.Sub(*row.NextExecutionTime))
			}
			if err := advanceScheduledRun(ctx, tx, row.ID, next); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to reserve due executions: %w", err)
	}
	return nil
}

// getScheduledRunForUpdate locks an owned schedule, including tombstones, until the
// caller's transaction ends. A missing or unowned schedule returns ErrNotFound; callers
// must supply a transaction to retain the lock.
func getScheduledRunForUpdate(ctx context.Context, db dbExecutor, id uuid.UUID, creator string) (scheduledRunRow, error) {
	row, err := queryOne(ctx, db, `
		SELECT id, creator, namespace, name, generation, disabled, data, created_at, updated_at, next_execution_time, deleted_at
		    FROM scheduled_run WHERE ($1::text = '' OR creator = $1) AND id = $2 FOR UPDATE
	`, pgx.RowToStructByName[scheduledRunRow], creator, id)
	return row, notFoundOr(err)
}

// reserveScheduledRunExecution records a pending execution with the current prompt and
// timeout, using either a scheduled time or manual request ID. Callers hold the schedule
// lock and handle deduplication in the same transaction; this function does not launch
// runtime work.
func reserveScheduledRunExecution(ctx context.Context, db pgx.Tx, schedule *ScheduledRun, due *time.Time, manualRequestID *string, initiator string) (scheduledRunExecutionRow, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return scheduledRunExecutionRow{}, fmt.Errorf("failed to generate execution ID: %w", err)
	}
	execution := &apiv1.ScheduledRunExecution{
		Id: id.String(), ScheduledRunId: schedule.ID, Creator: schedule.Creator,
		Prompt: schedule.Spec.Prompt, Initiator: initiator,
		ScheduledRun: &apiv1.ResourceReference{Namespace: schedule.Ref.Namespace, Name: schedule.Ref.Name}, ScheduledRunGeneration: schedule.Generation,
		State: apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_PENDING,
	}
	if due != nil {
		execution.Trigger = &apiv1.ScheduledRunExecution_ScheduledTime{ScheduledTime: timestamppb.New(*due)}
	} else if manualRequestID != nil {
		execution.Trigger = &apiv1.ScheduledRunExecution_ManualRequestId{ManualRequestId: *manualRequestID}
	}
	sessionID, err := uuid.NewV7()
	if err != nil {
		return scheduledRunExecutionRow{}, err
	}
	session, err := insertSession(ctx, db, &apiv1.Session{
		Id: sessionID.String(), Creator: schedule.Creator,
		Agent: &apiv1.AgentProvenance{Ref: &apiv1.ResourceReference{Namespace: schedule.Ref.Namespace, Name: schedule.Spec.AgentRef.Name}, Uid: schedule.AgentUID},
		ScheduledRun: &apiv1.ScheduledRunProvenance{
			Ref: proto.CloneOf(execution.ScheduledRun), Uid: schedule.ID, ExecutionId: id.String(),
		},
	}, "scheduled-run/"+id.String())
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return scheduledRunExecutionRow{}, fmt.Errorf("schedule has no successful runtime revision: %w", ErrFailedPrecondition)
		}
		return scheduledRunExecutionRow{}, fmt.Errorf("reserve scheduled Session: %w", err)
	}
	execution.SessionId = session.ID.String()
	execution.RuntimeRevision = *session.PreparedRevision
	data, err := proto.Marshal(execution)
	if err != nil {
		return scheduledRunExecutionRow{}, err
	}
	// PostgreSQL timestamps have microsecond precision; don't shorten a timeout.
	timeout := (schedule.Spec.ExecutionTimeout.Duration + time.Microsecond - 1) / time.Microsecond
	return queryOne(ctx, db, `
		INSERT INTO scheduled_run_execution (id, scheduled_run_id, scheduled_time, manual_request_id, data, deadline, session_id, initiator)
		VALUES ($1, $2, $3, $4, $5,
		    statement_timestamp() + $6::interval, $7, $8) RETURNING id, scheduled_run_id, scheduled_time, manual_request_id, data, created_at, deadline, session_id, task_id, completed_at, state
	`,
		pgx.RowToStructByName[scheduledRunExecutionRow], id, schedule.ID, due, manualRequestID, data,
		pgtype.Interval{Microseconds: int64(timeout), Valid: true}, session.ID, initiator,
	)
}

// GetScheduledRunExecution returns an execution only for its schedule's creator, including
// after schedule deletion. Missing executions and other owners return ErrNotFound.
func (c *Client) GetScheduledRunExecution(ctx context.Context, id uuid.UUID, creator string) (*apiv1.ScheduledRunExecution, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT e.id, e.scheduled_run_id, e.scheduled_time, e.manual_request_id, e.data, e.created_at, e.deadline,
		    e.session_id, e.task_id, e.completed_at, e.state FROM
		    scheduled_run_execution e JOIN scheduled_run s ON s.id = e.scheduled_run_id
		WHERE ($1::text = '' OR s.creator = $1) AND e.id = $2
	`, pgx.RowToStructByName[scheduledRunExecutionRow], creator, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get execution: %w", notFoundOr(err))
	}
	return toScheduledRunExecution(row)
}

// ListScheduledRunExecutions returns a creator's execution history for one schedule in
// descending ID order after AfterID, up to Limit. History remains available after schedule
// deletion.
func (c *Client) ListScheduledRunExecutions(ctx context.Context, query ScheduledRunExecutionQuery) ([]*apiv1.ScheduledRunExecution, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT e.id, e.scheduled_run_id, e.scheduled_time, e.manual_request_id, e.data, e.created_at, e.deadline,
		    e.session_id, e.task_id, e.completed_at, e.state FROM
		    scheduled_run_execution e JOIN scheduled_run s ON s.id = e.scheduled_run_id
		WHERE ($1::text = '' OR s.creator = $1) AND e.scheduled_run_id = $2
		  AND ($4::uuid IS NULL OR e.id < $4::uuid)
		ORDER BY e.id DESC LIMIT $3
	`,
		pgx.RowToStructByName[scheduledRunExecutionRow], query.Creator, query.ScheduledRunID, int32(query.Limit),
		query.AfterID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list executions: %w", err)
	}
	result := make([]*apiv1.ScheduledRunExecution, 0, len(rows))
	for _, row := range rows {
		execution, err := toScheduledRunExecution(row)
		if err != nil {
			return nil, err
		}
		result = append(result, execution)
	}
	return result, nil
}

// toScheduledRunExecution decodes an execution and validates its payload, state, and
// trigger. Indexed columns supply identity, timestamps, state, and session/task links.
func toScheduledRunExecution(row scheduledRunExecutionRow) (*apiv1.ScheduledRunExecution, error) {
	execution := &apiv1.ScheduledRunExecution{}
	if err := proto.Unmarshal(row.Data, execution); err != nil {
		return nil, fmt.Errorf("failed to decode execution %s: %w", row.ID, err)
	}
	execution.CreatedAt, execution.Deadline = timestamppb.New(row.CreatedAt), timestamppb.New(row.Deadline)
	if execution.GetCreator() == "" || strings.TrimSpace(execution.GetPrompt()) == "" || len(execution.GetPrompt()) > 32768 ||
		execution.CreatedAt.CheckValid() != nil || execution.Deadline.CheckValid() != nil ||
		!execution.Deadline.AsTime().After(execution.CreatedAt.AsTime()) {
		return nil, fmt.Errorf("invalid execution payload %s", row.ID)
	}
	state, ok := apiv1.ScheduledRunExecutionState_value[row.State]
	if !ok {
		return nil, fmt.Errorf("invalid execution state %q", row.State)
	}
	execution.Id, execution.ScheduledRunId = row.ID.String(), row.ScheduledRunID.String()
	execution.State, execution.CompletedAt = apiv1.ScheduledRunExecutionState(state), optionalTimestamp(row.CompletedAt)
	execution.SessionId, execution.TaskId = "", ""
	if row.SessionID != nil {
		execution.SessionId = row.SessionID.String()
	}
	if row.TaskID != nil {
		execution.TaskId = *row.TaskID
	}
	if row.ScheduledTime != nil {
		execution.Trigger = &apiv1.ScheduledRunExecution_ScheduledTime{ScheduledTime: timestamppb.New(*row.ScheduledTime)}
	} else if row.ManualRequestID != nil {
		execution.Trigger = &apiv1.ScheduledRunExecution_ManualRequestId{ManualRequestId: *row.ManualRequestID}
	} else {
		return nil, fmt.Errorf("execution %s has no trigger", row.ID)
	}
	return execution, nil
}

// LeaseScheduledRunExecutions claims up to limit pending or running executions due for
// retry, excluding rows held by other workers. Each claim lasts thirty seconds and must be
// supplied when recording progress. Malformed executions are logged and omitted, retaining
// their lease delay before retry.
func (c *Client) LeaseScheduledRunExecutions(ctx context.Context, limit int) ([]LeasedScheduledRunExecution, error) {
	token := uuid.New()
	rows, err := queryMany(ctx, c.db, `
		WITH candidates AS (
		    SELECT id FROM scheduled_run_execution
		    WHERE state IN ('SCHEDULED_RUN_EXECUTION_STATE_PENDING', 'SCHEDULED_RUN_EXECUTION_STATE_RUNNING')
		      AND next_attempt_at <= statement_timestamp()
		    ORDER BY next_attempt_at, id LIMIT $1 FOR UPDATE SKIP LOCKED
		)
		UPDATE scheduled_run_execution e
		SET lease_token = $2::uuid, next_attempt_at = clock_timestamp() + interval '30 seconds'
		FROM candidates c WHERE e.id = c.id RETURNING e.id, e.scheduled_run_id, e.scheduled_time, e.manual_request_id, e.data, e.created_at, e.deadline, e.session_id, e.task_id, e.completed_at, e.state
	`, pgx.RowToStructByName[scheduledRunExecutionRow], int32(limit), token)
	if err != nil {
		return nil, fmt.Errorf("failed to lease scheduled executions: %w", err)
	}
	leases := make([]LeasedScheduledRunExecution, 0, len(rows))
	for _, row := range rows {
		execution, err := toScheduledRunExecution(row)
		if err != nil {
			// The committed lease supplies the retry delay. A bad payload must
			// not discard healthy leases or invent a terminal runtime state.
			logging.FromContext(ctx).ErrorContext(ctx, "skipping malformed scheduled execution", "execution_id", row.ID, "error", err)
			continue
		}
		leases = append(leases, LeasedScheduledRunExecution{Execution: execution, Lease: ScheduledRunExecutionLease{ExecutionID: row.ID, Token: token}})
	}
	return leases, nil
}

// ClaimScheduledRunDispatch records that a worker may issue its one A2A send.
// It requires an unexpired lease and a linked, pending execution. Once claimed,
// later workers recover the task from history; they never resend an uncertain
// dispatch. A crash before sending therefore waits for the execution deadline.
func (c *Client) ClaimScheduledRunDispatch(ctx context.Context, lease ScheduledRunExecutionLease) error {
	result, err := c.db.Exec(ctx, `
  UPDATE scheduled_run_execution SET state = 'SCHEDULED_RUN_EXECUTION_STATE_RUNNING'
  WHERE id = $1 AND lease_token = $2 AND next_attempt_at > clock_timestamp()
    AND deadline > clock_timestamp() AND session_id IS NOT NULL
    AND state = 'SCHEDULED_RUN_EXECUTION_STATE_PENDING'
 `, lease.ExecutionID, lease.Token)
	if err != nil {
		return fmt.Errorf("claim scheduled dispatch: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrConflict
	}
	return nil
}

// UpdateScheduledRunExecution records progress only under a matching, unexpired lease on a
// pending or running execution. It preserves an existing task ID, records terminal
// completion time, and releases the lease with a one-second retry delay. A lost lease or
// conflicting task ID returns ErrConflict.
func (c *Client) UpdateScheduledRunExecution(ctx context.Context, lease ScheduledRunExecutionLease, progress ScheduledRunExecutionProgress) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := queryOne(ctx, tx, `
			SELECT id, scheduled_run_id, scheduled_time, manual_request_id, data, created_at, deadline, session_id,
			    task_id, completed_at, state FROM scheduled_run_execution
			WHERE id = $1 AND lease_token = $2 AND next_attempt_at > clock_timestamp()
			  AND state IN ('SCHEDULED_RUN_EXECUTION_STATE_PENDING', 'SCHEDULED_RUN_EXECUTION_STATE_RUNNING') FOR UPDATE
		`, pgx.RowToStructByName[scheduledRunExecutionRow], lease.ExecutionID, &lease.Token)
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("ScheduledRunExecution %s lease expired or execution changed: %w", lease.ExecutionID, ErrConflict)
		}
		if err != nil {
			return fmt.Errorf("failed to get leased execution: %w", err)
		}
		execution, err := toScheduledRunExecution(row)
		if err != nil {
			return err
		}
		execution.FailureReason = progress.FailureReason
		data, err := proto.Marshal(execution)
		if err != nil {
			return err
		}
		var taskID *string
		if progress.TaskID != "" {
			taskID = &progress.TaskID
		}
		rows, err := tx.Exec(ctx, `
			UPDATE scheduled_run_execution
			SET state = $2, task_id = COALESCE(task_id, $3), data = $4,
			    completed_at = CASE WHEN $2::text IN (
			        'SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED', 'SCHEDULED_RUN_EXECUTION_STATE_FAILED',
			        'SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT'
			    ) THEN clock_timestamp() END,
			    next_attempt_at = clock_timestamp() + interval '1 second', lease_token = NULL
			WHERE id = $1 AND lease_token = $5::uuid AND next_attempt_at > clock_timestamp()
			    AND state IN ('SCHEDULED_RUN_EXECUTION_STATE_PENDING', 'SCHEDULED_RUN_EXECUTION_STATE_RUNNING')
			    AND (task_id IS NULL OR $3::text IS NULL OR task_id = $3)
            AND (state <> 'SCHEDULED_RUN_EXECUTION_STATE_RUNNING' OR $2::text <> 'SCHEDULED_RUN_EXECUTION_STATE_PENDING')
		`,
			lease.ExecutionID, progress.State.String(), taskID,
			data, lease.Token,
		)
		if err != nil {
			return fmt.Errorf("failed to update scheduled execution: %w", err)
		}
		if rows.RowsAffected() == 0 {
			return fmt.Errorf("ScheduledRunExecution %s lease expired or execution changed: %w", lease.ExecutionID, ErrConflict)
		}
		return nil
	})
}

// nextExecutionTime computes the next occurrence after now, returning nil for a paused
// schedule. Invalid configuration returns an error even when paused.
func nextExecutionTime(spec v1alpha3.ScheduledRunSpec, now time.Time) (*time.Time, error) {
	next, err := scheduling.Next(spec, now)
	if err != nil {
		return nil, err
	}
	if *spec.Paused {
		return nil, nil
	}
	return &next, nil
}

// scheduledRunSnapshot stores only reconciled inputs without duplicating indexed
// identity, ownership, or timestamps. It is private persistence, not a wire API.
type scheduledRunSnapshot struct {
	Spec            v1alpha3.ScheduledRunSpec `json:"spec"`
	ResourceVersion string                    `json:"resourceVersion"`
	AgentUID        string                    `json:"agentUID"`
}

// toScheduledRun validates durable inputs and takes identity, ownership, and
// scheduling times exclusively from indexed columns. Missing defaults are corrupt
// data: they must have been resolved when the projection was written.
func toScheduledRun(row scheduledRunRow) (*ScheduledRun, error) {
	var snapshot scheduledRunSnapshot
	if err := json.Unmarshal(row.Data, &snapshot); err != nil {
		return nil, fmt.Errorf("failed to decode schedule %s: %w", row.ID, err)
	}
	if snapshot.Spec.ExecutionTimeout == nil || snapshot.Spec.TimeZone == nil || snapshot.Spec.Paused == nil || snapshot.ResourceVersion == "" || snapshot.AgentUID == "" {
		return nil, fmt.Errorf("invalid stored schedule %s", row.ID)
	}
	if err := scheduling.Validate(snapshot.Spec); err != nil {
		return nil, fmt.Errorf("invalid stored schedule %s: %w", row.ID, err)
	}
	return &ScheduledRun{
		ID: row.ID.String(), Creator: row.Creator,
		Ref:        types.NamespacedName{Namespace: row.Namespace, Name: row.Name},
		Generation: row.Generation, Disabled: row.Disabled, ResourceVersion: snapshot.ResourceVersion,
		Spec: snapshot.Spec, AgentUID: snapshot.AgentUID,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
		NextExecutionTime: row.NextExecutionTime, DeletedAt: row.DeletedAt,
	}, nil
}

// optionalTimestamp converts a present time to a protobuf timestamp and preserves nil.
func optionalTimestamp(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(*value)
}

type dueScheduledRunRow struct {
	scheduledRunRow
	DBTime time.Time
}

type scheduledRunRow struct {
	ID                uuid.UUID
	Creator           string
	Namespace         string
	Name              string
	Generation        int64
	Disabled          bool
	Data              []byte
	CreatedAt         time.Time
	UpdatedAt         time.Time
	NextExecutionTime *time.Time
	DeletedAt         *time.Time
}

type scheduledRunExecutionRow struct {
	ID              uuid.UUID
	ScheduledRunID  uuid.UUID
	ScheduledTime   *time.Time
	ManualRequestID *string
	Data            []byte
	CreatedAt       time.Time
	Deadline        time.Time
	SessionID       *uuid.UUID
	TaskID          *string
	CompletedAt     *time.Time
	State           string
}

// advanceScheduledRun sets the next due time, or removes the schedule from the due queue
// when next is nil. It does not change the resource version or authorize access; callers coordinate
// the surrounding transaction.
func advanceScheduledRun(ctx context.Context, db dbExecutor, id uuid.UUID, next *time.Time) error {
	return execSQL(ctx, db, `
		UPDATE scheduled_run SET next_execution_time = $2 WHERE id = $1
	`, id, next)
}

// readScheduledRun reads an owned schedule, including a deletion tombstone, without
// locking it. Missing schedules and other owners return pgx.ErrNoRows.
func readScheduledRun(ctx context.Context, db dbExecutor, id uuid.UUID, creator string) (scheduledRunRow, error) {
	return queryOne(ctx, db, `
		SELECT id, creator, namespace, name, generation, disabled, data, created_at, updated_at, next_execution_time, deleted_at
		    FROM scheduled_run WHERE ($1::text = '' OR creator = $1) AND id = $2
	`, pgx.RowToStructByName[scheduledRunRow], creator, id)
}
