package database

import (
	"time"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// ScheduledRun is the internal scheduling projection of a Kubernetes resource.
// Indexed columns own identity and timing; Spec is the reconciled CRD snapshot.
type ScheduledRun struct {
	ID      string
	Creator string
	Ref     types.NamespacedName
	// Generation is the last observed generation; Disabled marks a rejected one.
	Generation      int64
	Disabled        bool
	ResourceVersion string
	Spec            v1alpha3.ScheduledRunSpec
	// AgentUID is the reconciled target identity, fencing deletion and recreation.
	AgentUID          string
	CreatedAt         time.Time
	UpdatedAt         time.Time
	NextExecutionTime *time.Time
	DeletedAt         *time.Time
}

// ScheduledRunSource identifies the Kubernetes generation a cron reservation
// must validate before advancing the durable projection.
type ScheduledRunSource struct {
	ID         uuid.UUID
	Namespace  string
	Name       string
	Generation int64
}

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
	Execution *apiv1.ScheduledRunExecution
	Lease     ScheduledRunExecutionLease
}

// ScheduledRunExecutionProgress contains only fields a worker may change.
type ScheduledRunExecutionProgress struct {
	State         apiv1.ScheduledRunExecutionState
	TaskID        string
	FailureReason string
}
