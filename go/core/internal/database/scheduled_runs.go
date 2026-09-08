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
	dbgen "github.com/kagent-dev/kagent/go/core/internal/database/internal/dbgen"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func (c *Client) FindScheduledRunRequest(ctx context.Context, creator, requestID string, hash []byte) (*apiv1alpha1.ScheduledRun, error) {
	row, err := c.q.FindScheduledRunRequest(ctx, dbgen.FindScheduledRunRequestParams{Creator: creator, RequestID: requestID})
	if err != nil {
		return nil, fmt.Errorf("failed to find schedule request: %w", notFoundOr(err))
	}
	if !bytes.Equal(row.RequestHash, hash) {
		return nil, ErrIdempotencyConflict
	}
	return toScheduledRun(row)
}

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
	var row dbgen.ScheduledRun
	err = c.withTx(ctx, func(q *dbgen.Queries) error {
		row, err = q.CreateScheduledRun(ctx, dbgen.CreateScheduledRunParams{
			ID: id, Creator: schedule.Creator,
			RequestID: requestID, RequestHash: hash, Data: data,
		})
		if err != nil {
			return err
		}
		row.NextExecutionTime, err = nextExecutionTime(schedule.Config, row.CreatedAt)
		if err != nil || row.NextExecutionTime == nil {
			return err
		}
		// Keep the first cron time atomic with creation, using the insert's clock.
		return q.AdvanceScheduledRun(ctx, dbgen.AdvanceScheduledRunParams{ID: row.ID, NextExecutionTime: row.NextExecutionTime})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return c.FindScheduledRunRequest(ctx, schedule.Creator, requestID, hash)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to create schedule: %w", err)
	}
	return toScheduledRun(row)
}

func (c *Client) GetScheduledRun(ctx context.Context, id uuid.UUID, creator string) (*apiv1alpha1.ScheduledRun, error) {
	row, err := c.q.GetScheduledRun(ctx, dbgen.GetScheduledRunParams{ID: id, Creator: creator})
	if err != nil {
		return nil, fmt.Errorf("failed to get schedule: %w", notFoundOr(err))
	}
	return toScheduledRun(row)
}

func (c *Client) ListScheduledRuns(ctx context.Context, query ScheduledRunQuery) ([]*apiv1alpha1.ScheduledRun, error) {
	rows, err := c.q.ListScheduledRuns(ctx, dbgen.ListScheduledRunsParams{Creator: query.Creator, AfterID: query.AfterID, Limit: int32(query.Limit)})
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

func (c *Client) UpdateScheduledRun(ctx context.Context, id uuid.UUID, creator, etag string, config *apiv1alpha1.ScheduledRunConfig) (*apiv1alpha1.ScheduledRun, error) {
	var result dbgen.ScheduledRun
	err := c.withTx(ctx, func(q *dbgen.Queries) error {
		row, err := getScheduledRunForUpdate(ctx, q, id, creator)
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
		result, err = q.SaveScheduledRun(ctx, dbgen.SaveScheduledRunParams{ID: row.ID, Data: data, NextExecutionTime: row.NextExecutionTime})
		if err != nil {
			return err
		}
		// Prompt/name edits must not skip an occurrence already due for reservation.
		if config.Schedule != previous.Schedule || config.TimeZone != previous.TimeZone || config.Paused != previous.Paused {
			result.NextExecutionTime, err = nextExecutionTime(config, result.UpdatedAt)
			if err != nil {
				return err
			}
			return q.AdvanceScheduledRun(ctx, dbgen.AdvanceScheduledRunParams{ID: result.ID, NextExecutionTime: result.NextExecutionTime})
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to update schedule: %w", err)
	}
	return toScheduledRun(result)
}

func (c *Client) DeleteScheduledRun(ctx context.Context, id uuid.UUID, creator string) (*apiv1alpha1.ScheduledRun, error) {
	var result dbgen.ScheduledRun
	err := c.withTx(ctx, func(q *dbgen.Queries) error {
		row, err := getScheduledRunForUpdate(ctx, q, id, creator)
		if err != nil {
			return err
		}
		if row.DeletedAt != nil {
			result = row
			return nil
		}
		schedule, err := toScheduledRun(row)
		if err != nil {
			return err
		}
		schedule.Etag, schedule.NextExecutionTime = uuid.NewString(), nil
		data, err := proto.Marshal(schedule)
		if err != nil {
			return err
		}
		result, err = q.SaveScheduledRun(ctx, dbgen.SaveScheduledRunParams{ID: row.ID, Data: data, Deleted: true})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to delete schedule: %w", err)
	}
	return toScheduledRun(result)
}

func (c *Client) TriggerScheduledRun(ctx context.Context, id uuid.UUID, creator, requestID string) (*apiv1alpha1.ScheduledRunExecution, error) {
	var result dbgen.ScheduledRunExecution
	err := c.withTx(ctx, func(q *dbgen.Queries) error {
		row, err := getScheduledRunForUpdate(ctx, q, id, creator)
		if err != nil {
			return err
		}
		result, err = q.FindManualScheduledRunExecution(ctx, dbgen.FindManualScheduledRunExecutionParams{ScheduledRunID: row.ID, ManualRequestID: &requestID})
		if err == nil {
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		if row.DeletedAt != nil {
			return ErrScheduledRunDeleted
		}
		result, err = reserveScheduledRunExecution(ctx, q, row, nil, &requestID)
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to trigger schedule: %w", err)
	}
	return toScheduledRunExecution(result)
}

// ReserveDueScheduledRuns commits each due occurrence and advances its schedule
// in the same transaction. Row locks serialize this with manual triggers/edits.
func (c *Client) ReserveDueScheduledRuns(ctx context.Context, limit int) ([]*apiv1alpha1.ScheduledRunExecution, error) {
	if limit < 1 || limit > 100 {
		return nil, fmt.Errorf("reservation limit must be between 1 and 100")
	}
	result := []*apiv1alpha1.ScheduledRunExecution{}
	err := c.withTx(ctx, func(q *dbgen.Queries) error {
		rows, err := q.GetDueScheduledRunsForUpdate(ctx, int32(limit))
		if err != nil {
			return err
		}
		for _, due := range rows {
			row, now := due.ScheduledRun, due.DbTime
			schedule, err := toScheduledRun(row)
			if err != nil {
				return err
			}
			next, err := nextExecutionTime(schedule.Config, now)
			if err != nil {
				return err
			}
			// ponytail: fixed 30s lateness allowance; configure it if deployments need longer failover tolerance.
			if now.Sub(*row.NextExecutionTime) <= 30*time.Second {
				record, err := reserveScheduledRunExecution(ctx, q, row, row.NextExecutionTime, nil)
				if err != nil {
					return err
				}
				execution, err := toScheduledRunExecution(record)
				if err != nil {
					return err
				}
				result = append(result, execution)
			}
			if err := q.AdvanceScheduledRun(ctx, dbgen.AdvanceScheduledRunParams{ID: row.ID, NextExecutionTime: next}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to reserve due executions: %w", err)
	}
	return result, nil
}

func getScheduledRunForUpdate(ctx context.Context, q *dbgen.Queries, id uuid.UUID, creator string) (dbgen.ScheduledRun, error) {
	row, err := q.GetScheduledRunForUpdate(ctx, dbgen.GetScheduledRunForUpdateParams{ID: id, Creator: creator})
	return row, notFoundOr(err)
}

func reserveScheduledRunExecution(ctx context.Context, q *dbgen.Queries, row dbgen.ScheduledRun, due *time.Time, manualRequestID *string) (dbgen.ScheduledRunExecution, error) {
	schedule, err := toScheduledRun(row)
	if err != nil {
		return dbgen.ScheduledRunExecution{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return dbgen.ScheduledRunExecution{}, fmt.Errorf("failed to generate execution ID: %w", err)
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
		return dbgen.ScheduledRunExecution{}, err
	}
	// PostgreSQL timestamps have microsecond precision; don't shorten a timeout.
	timeout := (schedule.Config.ExecutionTimeout.AsDuration() + time.Microsecond - 1) / time.Microsecond
	return q.CreateScheduledRunExecution(ctx, dbgen.CreateScheduledRunExecutionParams{
		ID: id, ScheduledRunID: row.ID, ScheduledTime: due, ManualRequestID: manualRequestID, Data: data,
		ExecutionTimeout: pgtype.Interval{Microseconds: int64(timeout), Valid: true},
	})
}

func (c *Client) GetScheduledRunExecution(ctx context.Context, id uuid.UUID, creator string) (*apiv1alpha1.ScheduledRunExecution, error) {
	row, err := c.q.GetScheduledRunExecution(ctx, dbgen.GetScheduledRunExecutionParams{Creator: creator, ID: id})
	if err != nil {
		return nil, fmt.Errorf("failed to get execution: %w", notFoundOr(err))
	}
	return toScheduledRunExecution(row)
}

func (c *Client) ListScheduledRunExecutions(ctx context.Context, query ScheduledRunExecutionQuery) ([]*apiv1alpha1.ScheduledRunExecution, error) {
	rows, err := c.q.ListScheduledRunExecutions(ctx, dbgen.ListScheduledRunExecutionsParams{
		Creator: query.Creator, ScheduledRunID: query.ScheduledRunID, AfterID: query.AfterID, Limit: int32(query.Limit),
	})
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

// ReserveScheduledRunExecutionInstance keeps instance creation and the historical
// link in one transaction. A deleted conversation never becomes a new firing.
func (c *Client) ReserveScheduledRunExecutionInstance(ctx context.Context, id uuid.UUID, creator string) (*apiv1alpha1.ScheduledRunExecution, error) {
	var result dbgen.ScheduledRunExecution
	var err error
	err = c.withTx(ctx, func(q *dbgen.Queries) error {
		result, err = q.GetScheduledRunExecutionForUpdate(ctx, dbgen.GetScheduledRunExecutionForUpdateParams{Creator: creator, ID: id})
		if err != nil {
			return notFoundOr(err)
		}
		if result.AgentInstanceID != nil || result.State != "PENDING" {
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
		expired, err := q.ExpireScheduledRunExecution(ctx, dbgen.ExpireScheduledRunExecutionParams{ID: id, Data: data})
		if err == nil {
			result = expired
			return nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return err
		}
		scheduleRow, err := q.GetScheduledRun(ctx, dbgen.GetScheduledRunParams{Creator: creator, ID: result.ScheduledRunID})
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
		instance, err := insertAgentInstance(ctx, q, &apiv1alpha1.AgentInstance{
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
		result, err = q.SetScheduledRunExecutionInstance(ctx, dbgen.SetScheduledRunExecutionInstanceParams{ID: id, AgentInstanceID: &instance.ID})
		return err
	})
	if err != nil {
		return nil, fmt.Errorf("failed to reserve execution instance: %w", err)
	}
	return toScheduledRunExecution(result)
}

func toScheduledRunExecution(row dbgen.ScheduledRunExecution) (*apiv1alpha1.ScheduledRunExecution, error) {
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
	state, ok := apiv1alpha1.ScheduledRunExecutionState_value["SCHEDULED_RUN_EXECUTION_STATE_"+row.State]
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

func (c *Client) LeaseScheduledRunExecutions(ctx context.Context, limit int) ([]LeasedScheduledRunExecution, error) {
	token := uuid.New()
	rows, err := c.q.LeaseScheduledRunExecutionsForUpdate(ctx, dbgen.LeaseScheduledRunExecutionsForUpdateParams{Limit: int32(limit), LeaseToken: token})
	if err != nil {
		return nil, fmt.Errorf("failed to lease scheduled executions: %w", err)
	}
	leases := make([]LeasedScheduledRunExecution, 0, len(rows))
	for _, row := range rows {
		execution, err := toScheduledRunExecution(row)
		if err != nil {
			return nil, err
		}
		leases = append(leases, LeasedScheduledRunExecution{Execution: execution, Lease: ScheduledRunExecutionLease{ExecutionID: row.ID, Token: token}})
	}
	return leases, nil
}

func (c *Client) UpdateScheduledRunExecution(ctx context.Context, lease ScheduledRunExecutionLease, progress ScheduledRunExecutionProgress) error {
	return c.withTx(ctx, func(q *dbgen.Queries) error {
		row, err := q.GetLeasedScheduledRunExecutionForUpdate(ctx, dbgen.GetLeasedScheduledRunExecutionForUpdateParams{ID: lease.ExecutionID, LeaseToken: &lease.Token})
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
		rows, err := q.UpdateScheduledRunExecution(ctx, dbgen.UpdateScheduledRunExecutionParams{
			ID: lease.ExecutionID, LeaseToken: lease.Token, TaskID: taskID, Data: data,
			State: strings.TrimPrefix(progress.State.String(), "SCHEDULED_RUN_EXECUTION_STATE_"),
		})
		if err != nil {
			return fmt.Errorf("failed to update scheduled execution: %w", err)
		}
		if rows == 0 {
			return ErrScheduledRunConflict
		}
		return nil
	})
}

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

func toScheduledRun(row dbgen.ScheduledRun) (*apiv1alpha1.ScheduledRun, error) {
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

func optionalTimestamp(value *time.Time) *timestamppb.Timestamp {
	if value == nil {
		return nil
	}
	return timestamppb.New(*value)
}
