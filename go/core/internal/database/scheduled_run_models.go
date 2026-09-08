package database

import (
	"errors"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
)

var ErrScheduledRunConflict = errors.New("ScheduledRun changed since it was read")
var ErrScheduledRunDeleted = errors.New("ScheduledRun was deleted")
var ErrScheduledRunTargetNotReady = errors.New("ScheduledRun target has no ready prepared revision")

type ScheduledRunQuery struct {
	Creator string
	AfterID *uuid.UUID
	Limit   int
}

type ScheduledRunExecutionQuery struct {
	ScheduledRunQuery
	ScheduledRunID uuid.UUID
}

// ScheduledRunExecutionLease fences status updates from a previous worker.
// Leases expire after 30 seconds; network work must finish within that lease.
type ScheduledRunExecutionLease struct {
	ExecutionID uuid.UUID
	Token       uuid.UUID
}

// LeasedScheduledRunExecution keeps the immutable lease identity separate from the snapshot.
type LeasedScheduledRunExecution struct {
	Execution *apiv1alpha1.ScheduledRunExecution
	Lease     ScheduledRunExecutionLease
}

// ScheduledRunExecutionProgress contains only fields a worker may change.
type ScheduledRunExecutionProgress struct {
	State         apiv1alpha1.ScheduledRunExecutionState
	TaskID        string
	FailureReason string
}
