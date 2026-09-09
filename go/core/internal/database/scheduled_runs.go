package database

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"buf.build/go/protovalidate"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/scheduledrun"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// FindScheduledRunRequest returns the schedule created by the creator/requestID pair,
// including later edits or deletion. A different request hash returns
// ErrIdempotencyConflict; an unknown request returns ErrNotFound.
func (c *Client) FindScheduledRunRequest(ctx context.Context, creator, requestID string, hash []byte) (*apiv1alpha1.ScheduledRun, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT id, creator, request_hash, data, created_at, updated_at, next_execution_time, deleted_at
		    FROM scheduled_run WHERE creator = $1 AND request_id = $2
	`, pgx.RowToStructByName[scheduledRunRow], creator, requestID)
	if err != nil {
		return nil, fmt.Errorf("failed to find schedule request: %w", notFoundOr(err))
	}
	if !bytes.Equal(row.RequestHash, hash) {
		return nil, ErrIdempotencyConflict
	}
	return toScheduledRun(row)
}

// CreateScheduledRun atomically stores a schedule with a new ID, etag, and initial due
// time. Reusing a creator/requestID returns the existing schedule if the request hash
// matches, or ErrIdempotencyConflict otherwise. Paused schedules have no due time;
// execution is left to the scheduler.
func (c *Client) CreateScheduledRun(ctx context.Context, request *apiv1alpha1.ScheduledRun, requestID string, hash []byte) (*apiv1alpha1.ScheduledRun, error) {
	schedule := proto.CloneOf(request)
	id := uuid.New()
	schedule.Id, schedule.Etag = id.String(), uuid.NewString()
	schedule.CreatedAt, schedule.UpdatedAt = nil, nil
	schedule.NextExecutionTime, schedule.DeletedAt = nil, nil
	data, err := proto.Marshal(schedule)
	if err != nil {
		return nil, fmt.Errorf("failed to encode schedule: %w", err)
	}
	var row scheduledRunRow
	err = c.withTx(ctx, func(tx pgx.Tx) error {
		row, err = queryOne(ctx, tx, `
			INSERT INTO scheduled_run (id, creator, request_id, request_hash, data)
			VALUES ($1, $2, $3, $4, $5)
			ON CONFLICT (creator, request_id) DO NOTHING RETURNING id, creator, request_hash, data, created_at, updated_at, next_execution_time, deleted_at
		`, pgx.RowToStructByName[scheduledRunRow], id, schedule.Creator, requestID, hash, data)
		if err != nil {
			return err
		}
		row.NextExecutionTime, err = nextExecutionTime(schedule.Config, row.CreatedAt)
		if err != nil || row.NextExecutionTime == nil {
			return err
		}
		// Keep the first cron time atomic with creation, using the insert's clock.
		return advanceScheduledRun(ctx, tx, row.ID, row.NextExecutionTime)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return c.FindScheduledRunRequest(ctx, schedule.Creator, requestID, hash)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create schedule: %w", err)
	}
	return toScheduledRun(row)
}

// GetScheduledRun returns a creator's schedule, including a deletion tombstone. Missing
// schedules and other owners return ErrNotFound.
func (c *Client) GetScheduledRun(ctx context.Context, id uuid.UUID, creator string) (*apiv1alpha1.ScheduledRun, error) {
	row, err := readScheduledRun(ctx, c.db, id, creator)
	if err != nil {
		return nil, fmt.Errorf("failed to get schedule: %w", notFoundOr(err))
	}
	return toScheduledRun(row)
}

// ListScheduledRuns returns the creator's undeleted schedules in ascending ID order after
// AfterID, up to Limit.
func (c *Client) ListScheduledRuns(ctx context.Context, query ScheduledRunQuery) ([]*apiv1alpha1.ScheduledRun, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT id, creator, request_hash, data, created_at, updated_at, next_execution_time, deleted_at
		    FROM scheduled_run WHERE creator = $1 AND deleted_at IS NULL
		  AND ($3::uuid IS NULL OR id > $3::uuid)
		ORDER BY id LIMIT $2
	`, pgx.RowToStructByName[scheduledRunRow], query.Creator, int32(query.Limit), query.AfterID)
	if err != nil {
		return nil, fmt.Errorf("failed to list schedules: %w", err)
	}
	result := make([]*apiv1alpha1.ScheduledRun, 0, len(rows))
	for _, row := range rows {
		schedule, err := toScheduledRun(row)
		if err != nil {
			return nil, err
		}
		result = append(result, schedule)
	}
	return result, nil
}

// UpdateScheduledRun replaces an owned schedule's config and etag only if the supplied
// etag matches; stale edits return ErrScheduledRunConflict and deleted schedules return
// ErrScheduledRunDeleted. The next occurrence changes only when the schedule, time zone,
// or pause setting changes, so unrelated edits cannot skip an already-due occurrence.
func (c *Client) UpdateScheduledRun(ctx context.Context, id uuid.UUID, creator, etag string, config *apiv1alpha1.ScheduledRunConfig) (*apiv1alpha1.ScheduledRun, error) {
	var result scheduledRunRow
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := getScheduledRunForUpdate(ctx, tx, id, creator)
		if err != nil {
			return err
		}
		schedule, err := toScheduledRun(row)
		if err != nil {
			return err
		}
		if row.DeletedAt != nil {
			return ErrScheduledRunDeleted
		}
		if schedule.Etag != etag {
			return ErrScheduledRunConflict
		}
		previous := schedule.Config
		schedule.Config, schedule.Etag = proto.CloneOf(config), uuid.NewString()
		data, err := proto.Marshal(schedule)
		if err != nil {
			return err
		}
		result, err = saveScheduledRun(ctx, tx, row.ID, data, row.NextExecutionTime, false)
		if err != nil {
			return err
		}
		// Prompt/name edits must not skip an occurrence already due for reservation.
		if config.Schedule != previous.Schedule || config.TimeZone != previous.TimeZone || config.Paused != previous.Paused {
			result.NextExecutionTime, err = nextExecutionTime(config, result.UpdatedAt)
			if err != nil {
				return err
			}
			return advanceScheduledRun(ctx, tx, result.ID, result.NextExecutionTime)
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update schedule: %w", err)
	}
	return toScheduledRun(result)
}

// DeleteScheduledRun stops future scheduling and returns an owned deletion tombstone while
// retaining execution history and request deduplication. Repeated deletion succeeds.
// Malformed payloads are retained for repair and return only authoritative tombstone
// metadata.
func (c *Client) DeleteScheduledRun(ctx context.Context, id uuid.UUID, creator string) (*apiv1alpha1.ScheduledRun, error) {
	var result scheduledRunRow
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := getScheduledRunForUpdate(ctx, tx, id, creator)
		if err != nil {
			return err
		}
		if row.DeletedAt != nil {
			result = row
			return nil
		}
		schedule, err := toScheduledRun(row)
		if err != nil {
			// Identity and ownership come from columns, not the damaged payload.
			result, err = saveScheduledRun(ctx, tx, row.ID, row.Data, nil, true)
			return err
		}
		schedule.Etag, schedule.NextExecutionTime = uuid.NewString(), nil
		data, err := proto.Marshal(schedule)
		if err != nil {
			return err
		}
		result, err = saveScheduledRun(ctx, tx, row.ID, data, nil, true)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to delete schedule: %w", err)
	}
	schedule, err := toScheduledRun(result)
	if err != nil {
		logging.FromContext(ctx).ErrorContext(ctx, "deleted malformed schedule", "scheduled_run_id", result.ID, "error", err)
		// Return only authoritative tombstone metadata; preserve the bytes for repair.
		return &apiv1alpha1.ScheduledRun{
			Id: result.ID.String(), Creator: result.Creator,
			CreatedAt: timestamppb.New(result.CreatedAt), UpdatedAt: timestamppb.New(result.UpdatedAt),
			DeletedAt: optionalTimestamp(result.DeletedAt),
		}, nil
	}
	return schedule, nil
}

// TriggerScheduledRun reserves a manual execution for an owned schedule, including when
// paused, without starting runtime work or changing the next scheduled occurrence. Reusing
// requestID for that schedule returns the same execution, even after deletion; new
// triggers on a deleted schedule return ErrScheduledRunDeleted.
func (c *Client) TriggerScheduledRun(ctx context.Context, id uuid.UUID, creator, requestID string) (*apiv1alpha1.ScheduledRunExecution, error) {
	var result scheduledRunExecutionRow
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := getScheduledRunForUpdate(ctx, tx, id, creator)
		if err != nil {
			return err
		}
		result, err = queryOne(ctx, tx, `
			SELECT id, scheduled_run_id, scheduled_time, manual_request_id, data, created_at, deadline, agent_instance_id,
			    task_id, completed_at, state FROM scheduled_run_execution WHERE
			    scheduled_run_id = $1 AND manual_request_id = $2
		`, pgx.RowToStructByName[scheduledRunExecutionRow], row.ID, &requestID)
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if row.DeletedAt != nil {
			return ErrScheduledRunDeleted
		}
		schedule, err := toScheduledRun(row)
		if err != nil {
			return err
		}
		result, err = reserveScheduledRunExecution(ctx, tx, schedule, nil, &requestID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to trigger schedule: %w", err)
	}
	return toScheduledRunExecution(result)
}

// ReserveDueScheduledRuns atomically reserves due occurrences and advances their
// schedules, skipping rows held by other workers. Limit must be between 1 and 100.
// Occurrences over thirty seconds late are skipped; malformed schedules are removed from
// the due queue with their payloads retained for repair. Execution workers claim the
// stored reservations separately.
func (c *Client) ReserveDueScheduledRuns(ctx context.Context, limit int) error {
	if limit < 1 || limit > 100 {
		return fmt.Errorf("reservation limit must be between 1 and 100")
	}
	err := c.withTx(ctx, func(tx pgx.Tx) error {
		rows, err := queryMany(ctx, tx, `
			SELECT scheduled_run.id, scheduled_run.creator, scheduled_run.request_hash,
			    scheduled_run.data, scheduled_run.created_at, scheduled_run.updated_at, scheduled_run.next_execution_time,
			    scheduled_run.deleted_at, statement_timestamp()::timestamptz AS db_time FROM scheduled_run
			WHERE deleted_at IS NULL AND next_execution_time <= statement_timestamp()
			ORDER BY next_execution_time, id LIMIT $1 FOR UPDATE SKIP LOCKED
		`, pgx.RowToStructByName[dueScheduledRunRow], int32(limit))
		if err != nil {
			return err
		}
		for _, due := range rows {
			row, now := due.scheduledRunRow, due.DBTime
			schedule, err := toScheduledRun(row)
			var next *time.Time
			if err == nil {
				next, err = nextExecutionTime(schedule.Config, now)
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
			// ponytail: fixed 30s lateness allowance; configure it if deployments need longer failover tolerance.
			if now.Sub(*row.NextExecutionTime) <= 30*time.Second {
				_, err := reserveScheduledRunExecution(ctx, tx, schedule, row.NextExecutionTime, nil)
				if err != nil {
					return err
				}
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
		SELECT id, creator, request_hash, data, created_at, updated_at, next_execution_time, deleted_at
		    FROM scheduled_run WHERE creator = $1 AND id = $2 FOR UPDATE
	`, pgx.RowToStructByName[scheduledRunRow], creator, id)
	return row, notFoundOr(err)
}

// reserveScheduledRunExecution records a pending execution with the current prompt and
// timeout, using either a scheduled time or manual request ID. Callers hold the schedule
// lock and handle deduplication in the same transaction; this function does not launch
// runtime work.
func reserveScheduledRunExecution(ctx context.Context, db dbExecutor, schedule *apiv1alpha1.ScheduledRun, due *time.Time, manualRequestID *string) (scheduledRunExecutionRow, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return scheduledRunExecutionRow{}, fmt.Errorf("failed to generate execution ID: %w", err)
	}
	execution := &apiv1alpha1.ScheduledRunExecution{
		Id: id.String(), ScheduledRunId: schedule.Id, Creator: schedule.Creator,
		Prompt: schedule.Config.Prompt,
		State:  apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_PENDING,
	}
	if due != nil {
		execution.Trigger = &apiv1alpha1.ScheduledRunExecution_ScheduledTime{ScheduledTime: timestamppb.New(*due)}
	} else if manualRequestID != nil {
		execution.Trigger = &apiv1alpha1.ScheduledRunExecution_ManualRequestId{ManualRequestId: *manualRequestID}
	}
	data, err := proto.Marshal(execution)
	if err != nil {
		return scheduledRunExecutionRow{}, err
	}
	// PostgreSQL timestamps have microsecond precision; don't shorten a timeout.
	timeout := (schedule.Config.ExecutionTimeout.AsDuration() + time.Microsecond - 1) / time.Microsecond
	return queryOne(ctx, db, `
		INSERT INTO scheduled_run_execution (id, scheduled_run_id, scheduled_time, manual_request_id, data, deadline)
		VALUES ($1, $2, $3, $4, $5,
		    statement_timestamp() + $6::interval) RETURNING id, scheduled_run_id, scheduled_time, manual_request_id, data, created_at, deadline, agent_instance_id, task_id, completed_at, state
	`,
		pgx.RowToStructByName[scheduledRunExecutionRow], id, schedule.Id, due, manualRequestID, data,
		pgtype.Interval{Microseconds: int64(timeout), Valid: true},
	)
}

// GetScheduledRunExecution returns an execution only for its schedule's creator, including
// after schedule deletion. Missing executions and other owners return ErrNotFound.
func (c *Client) GetScheduledRunExecution(ctx context.Context, id uuid.UUID, creator string) (*apiv1alpha1.ScheduledRunExecution, error) {
	row, err := queryOne(ctx, c.db, `
		SELECT e.id, e.scheduled_run_id, e.scheduled_time, e.manual_request_id, e.data, e.created_at, e.deadline,
		    e.agent_instance_id, e.task_id, e.completed_at, e.state FROM
		    scheduled_run_execution e JOIN scheduled_run s ON s.id = e.scheduled_run_id
		WHERE s.creator = $1 AND e.id = $2
	`, pgx.RowToStructByName[scheduledRunExecutionRow], creator, id)
	if err != nil {
		return nil, fmt.Errorf("failed to get execution: %w", notFoundOr(err))
	}
	return toScheduledRunExecution(row)
}

// ListScheduledRunExecutions returns a creator's execution history for one schedule in
// descending ID order after AfterID, up to Limit. History remains available after schedule
// deletion.
func (c *Client) ListScheduledRunExecutions(ctx context.Context, query ScheduledRunExecutionQuery) ([]*apiv1alpha1.ScheduledRunExecution, error) {
	rows, err := queryMany(ctx, c.db, `
		SELECT e.id, e.scheduled_run_id, e.scheduled_time, e.manual_request_id, e.data, e.created_at, e.deadline,
		    e.agent_instance_id, e.task_id, e.completed_at, e.state FROM
		    scheduled_run_execution e JOIN scheduled_run s ON s.id = e.scheduled_run_id
		WHERE s.creator = $1 AND e.scheduled_run_id = $2
		  AND ($4::uuid IS NULL OR e.id < $4::uuid)
		ORDER BY e.id DESC LIMIT $3
	`,
		pgx.RowToStructByName[scheduledRunExecutionRow], query.Creator, query.ScheduledRunID, int32(query.Limit),
		query.AfterID,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list executions: %w", err)
	}
	result := make([]*apiv1alpha1.ScheduledRunExecution, 0, len(rows))
	for _, row := range rows {
		execution, err := toScheduledRunExecution(row)
		if err != nil {
			return nil, err
		}
		result = append(result, execution)
	}
	return result, nil
}

// ReserveScheduledRunExecutionInstance atomically reserves an owned execution's instance
// and saves its historical link. Existing links and non-pending executions are returned
// unchanged, even if the linked instance was deleted. An elapsed deadline marks the
// execution timed out; an unprepared target returns ErrScheduledRunTargetNotReady for
// retry. Callers provision the runtime separately.
func (c *Client) ReserveScheduledRunExecutionInstance(ctx context.Context, id uuid.UUID, creator string) (*apiv1alpha1.ScheduledRunExecution, error) {
	var result scheduledRunExecutionRow
	var err error
	err = c.withTx(ctx, func(tx pgx.Tx) error {
		result, err = queryOne(ctx, tx, `
			SELECT e.id, e.scheduled_run_id, e.scheduled_time, e.manual_request_id, e.data, e.created_at, e.deadline,
			    e.agent_instance_id, e.task_id, e.completed_at, e.state FROM
			    scheduled_run_execution e JOIN scheduled_run s ON s.id = e.scheduled_run_id
			WHERE s.creator = $1 AND e.id = $2 FOR UPDATE OF e
		`, pgx.RowToStructByName[scheduledRunExecutionRow], creator, id)
		if err != nil {
			return notFoundOr(err)
		}
		if result.AgentInstanceID != nil || result.State != "SCHEDULED_RUN_EXECUTION_STATE_PENDING" {
			return nil
		}
		execution, err := toScheduledRunExecution(result)
		if err != nil {
			return err
		}
		execution.FailureReason = "Execution deadline elapsed"
		data, err := proto.Marshal(execution)
		if err != nil {
			return err
		}
		completedAt, err := queryOne(ctx, tx, `
			UPDATE scheduled_run_execution SET state = 'SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT', completed_at = clock_timestamp(), data = $2
			WHERE id = $1 AND deadline <= clock_timestamp() RETURNING completed_at
		`, pgx.RowTo[time.Time], id, data)
		if err == nil {
			result.State = "SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT"
			result.CompletedAt, result.Data = &completedAt, data
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		scheduleRow, err := readScheduledRun(ctx, tx, result.ScheduledRunID, creator)
		if err != nil {
			return err
		}
		schedule, err := toScheduledRun(scheduleRow)
		if err != nil {
			return err
		}
		instanceID, err := uuid.NewV7()
		if err != nil {
			return err
		}
		instance, err := insertAgentInstance(ctx, tx, &apiv1alpha1.AgentInstance{
			Id: instanceID.String(), Creator: creator,
			Harness:       proto.CloneOf(schedule.Harness),
			AgentTemplate: proto.CloneOf(schedule.AgentTemplate),
		}, "scheduled-run/"+id.String())
		if errors.Is(err, ErrNotFound) {
			return ErrScheduledRunTargetNotReady
		}
		if err != nil {
			return err
		}
		rows, err := tx.Exec(ctx, `
			UPDATE scheduled_run_execution SET agent_instance_id = $2 WHERE id = $1
		`, id, instance.ID)
		if err != nil {
			return err
		}
		if rows.RowsAffected() != 1 {
			return pgx.ErrNoRows
		}
		result.AgentInstanceID = &instance.ID
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to reserve execution instance: %w", err)
	}
	return toScheduledRunExecution(result)
}

// toScheduledRunExecution decodes an execution and validates its payload, state, and
// trigger. Indexed columns supply identity, timestamps, state, and instance/task links.
func toScheduledRunExecution(row scheduledRunExecutionRow) (*apiv1alpha1.ScheduledRunExecution, error) {
	execution := &apiv1alpha1.ScheduledRunExecution{}
	if err := proto.Unmarshal(row.Data, execution); err != nil {
		return nil, fmt.Errorf("failed to decode execution %s: %w", row.ID, err)
	}
	execution.CreatedAt, execution.Deadline = timestamppb.New(row.CreatedAt), timestamppb.New(row.Deadline)
	if execution.GetCreator() == "" || strings.TrimSpace(execution.GetPrompt()) == "" || len(execution.GetPrompt()) > 32768 ||
		execution.CreatedAt.CheckValid() != nil || execution.Deadline.CheckValid() != nil ||
		!execution.Deadline.AsTime().After(execution.CreatedAt.AsTime()) {
		return nil, fmt.Errorf("invalid execution payload %s", row.ID)
	}
	state, ok := apiv1alpha1.ScheduledRunExecutionState_value[row.State]
	if !ok {
		return nil, fmt.Errorf("invalid execution state %q", row.State)
	}
	execution.Id, execution.ScheduledRunId = row.ID.String(), row.ScheduledRunID.String()
	execution.State, execution.CompletedAt = apiv1alpha1.ScheduledRunExecutionState(state), optionalTimestamp(row.CompletedAt)
	execution.AgentInstanceId, execution.TaskId = "", ""
	if row.AgentInstanceID != nil {
		execution.AgentInstanceId = row.AgentInstanceID.String()
	}
	if row.TaskID != nil {
		execution.TaskId = *row.TaskID
	}
	if row.ScheduledTime != nil {
		execution.Trigger = &apiv1alpha1.ScheduledRunExecution_ScheduledTime{ScheduledTime: timestamppb.New(*row.ScheduledTime)}
	} else if row.ManualRequestID != nil {
		execution.Trigger = &apiv1alpha1.ScheduledRunExecution_ManualRequestId{ManualRequestId: *row.ManualRequestID}
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
		FROM candidates c WHERE e.id = c.id RETURNING e.id, e.scheduled_run_id, e.scheduled_time, e.manual_request_id, e.data, e.created_at, e.deadline, e.agent_instance_id, e.task_id, e.completed_at, e.state
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

// UpdateScheduledRunExecution records progress only under a matching, unexpired lease on a
// pending or running execution. It preserves an existing task ID, records terminal
// completion time, and releases the lease with a one-second retry delay. A lost lease or
// conflicting task ID returns ErrScheduledRunConflict.
func (c *Client) UpdateScheduledRunExecution(ctx context.Context, lease ScheduledRunExecutionLease, progress ScheduledRunExecutionProgress) error {
	return c.withTx(ctx, func(tx pgx.Tx) error {
		row, err := queryOne(ctx, tx, `
			SELECT id, scheduled_run_id, scheduled_time, manual_request_id, data, created_at, deadline, agent_instance_id,
			    task_id, completed_at, state FROM scheduled_run_execution
			WHERE id = $1 AND lease_token = $2 AND next_attempt_at > clock_timestamp()
			  AND state IN ('SCHEDULED_RUN_EXECUTION_STATE_PENDING', 'SCHEDULED_RUN_EXECUTION_STATE_RUNNING') FOR UPDATE
		`, pgx.RowToStructByName[scheduledRunExecutionRow], lease.ExecutionID, &lease.Token)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrScheduledRunConflict
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
		`,
			lease.ExecutionID, progress.State.String(), taskID,
			data, lease.Token,
		)
		if err != nil {
			return fmt.Errorf("failed to update scheduled execution: %w", err)
		}
		if rows.RowsAffected() == 0 {
			return ErrScheduledRunConflict
		}
		return nil
	})
}

// nextExecutionTime computes the next occurrence after now, returning nil for a paused
// schedule. Invalid configuration returns an error even when paused.
func nextExecutionTime(config *apiv1alpha1.ScheduledRunConfig, now time.Time) (*time.Time, error) {
	next, err := scheduledrun.Next(config, now)
	if err != nil {
		return nil, err
	}
	if config.Paused {
		return nil, nil
	}
	return &next, nil
}

// toScheduledRun decodes and validates a schedule's payload and config, taking identity,
// ownership, timestamps, next occurrence, and deletion status from indexed columns.
func toScheduledRun(row scheduledRunRow) (*apiv1alpha1.ScheduledRun, error) {
	schedule := &apiv1alpha1.ScheduledRun{}
	if err := proto.Unmarshal(row.Data, schedule); err != nil {
		return nil, fmt.Errorf("failed to decode schedule %s: %w", row.ID, err)
	}
	schedule.CreatedAt, schedule.UpdatedAt = timestamppb.New(row.CreatedAt), timestamppb.New(row.UpdatedAt)
	if schedule.GetConfig() == nil || schedule.Config.ExecutionTimeout == nil ||
		schedule.CreatedAt.CheckValid() != nil || schedule.UpdatedAt.CheckValid() != nil ||
		schedule.Etag == "" || schedule.GetHarness().GetNamespace() == "" || schedule.GetHarness().GetName() == "" || schedule.GetAgentTemplate().GetName() == "" ||
		schedule.GetHarness().GetNamespace() != schedule.GetAgentTemplate().GetNamespace() {
		return nil, fmt.Errorf("invalid schedule payload %s", row.ID)
	}
	if err := protovalidate.Validate(schedule.Config); err != nil {
		return nil, fmt.Errorf("invalid schedule config %s: %w", row.ID, err)
	}
	schedule.Id, schedule.Creator = row.ID.String(), row.Creator
	schedule.NextExecutionTime, schedule.DeletedAt = optionalTimestamp(row.NextExecutionTime), optionalTimestamp(row.DeletedAt)
	return schedule, nil
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
	RequestHash       []byte
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
	AgentInstanceID *uuid.UUID
	TaskID          *string
	CompletedAt     *time.Time
	State           string
}

// advanceScheduledRun sets the next due time, or removes the schedule from the due queue
// when next is nil. It does not change the etag or authorize access; callers coordinate
// the surrounding transaction.
func advanceScheduledRun(ctx context.Context, db dbExecutor, id uuid.UUID, next *time.Time) error {
	return execSQL(ctx, db, `
		UPDATE scheduled_run SET next_execution_time = $2 WHERE id = $1
	`, id, next)
}

// saveScheduledRun replaces a schedule's payload, next occurrence, and deletion status and
// refreshes its update time. Callers lock the row and enforce ownership and etag checks in
// their transaction.
func saveScheduledRun(ctx context.Context, db dbExecutor, id uuid.UUID, data []byte, next *time.Time, deleted bool) (scheduledRunRow, error) {
	return queryOne(ctx, db, `
		UPDATE scheduled_run SET data = $2, next_execution_time = $3,
		    updated_at = statement_timestamp(),
		    deleted_at = CASE WHEN $4::boolean THEN statement_timestamp() END
		WHERE id = $1 RETURNING id, creator, request_hash, data, created_at, updated_at, next_execution_time, deleted_at
	`, pgx.RowToStructByName[scheduledRunRow], id, data, next, deleted)
}

// readScheduledRun reads an owned schedule, including a deletion tombstone, without
// locking it. Missing schedules and other owners return pgx.ErrNoRows.
func readScheduledRun(ctx context.Context, db dbExecutor, id uuid.UUID, creator string) (scheduledRunRow, error) {
	return queryOne(ctx, db, `
		SELECT id, creator, request_hash, data, created_at, updated_at, next_execution_time, deleted_at
		    FROM scheduled_run WHERE creator = $1 AND id = $2
	`, pgx.RowToStructByName[scheduledRunRow], creator, id)
}
