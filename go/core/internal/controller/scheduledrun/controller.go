package scheduledrun

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type controllerStore interface {
	ReserveScheduledRunExecutionSession(context.Context, uuid.UUID, string) (*apiv1alpha1.ScheduledRunExecution, error)
	ClaimScheduledRunDispatch(context.Context, database.ScheduledRunExecutionLease) error
	LeaseScheduledRunExecutions(context.Context, int) ([]database.LeasedScheduledRunExecution, error)
	UpdateScheduledRunExecution(context.Context, database.ScheduledRunExecutionLease, database.ScheduledRunExecutionProgress) error
	GetSession(context.Context, string, string) (*apiv1alpha1.Session, error)
}

type controllerWorkflow interface {
	Create(context.Context, *apiv1alpha1.Session) (*apiv1alpha1.Session, error)
	Suspend(context.Context, *apiv1alpha1.Session) (*apiv1alpha1.Session, error)
	Delete(context.Context, *apiv1alpha1.Session) (*apiv1alpha1.Session, error)
}

// Controller reconciles executions on every replica. SQL leases fence status
// writes; a durable dispatch claim prevents resending after an uncertain result.
type Controller struct {
	store    controllerStore
	workflow controllerWorkflow
	gateway  a2asrv.RequestHandler
}

var _ manager.LeaderElectionRunnable = (*Controller)(nil)
var _ manager.Runnable = (*Controller)(nil)

func NewController(store controllerStore, workflow controllerWorkflow, gateway a2asrv.RequestHandler) *Controller {
	return &Controller{store: store, workflow: workflow, gateway: gateway}
}

func (*Controller) NeedLeaderElection() bool { return false }

func (c *Controller) Start(ctx context.Context) error {
	ctx = auth.AuthSessionTo(ctx, auth.ControlPlaneSession{})
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		if err := c.tick(ctx); err != nil && ctx.Err() == nil {
			logging.FromContext(ctx).ErrorContext(ctx, "failed to reconcile scheduled executions", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (c *Controller) tick(ctx context.Context) error {
	leases, err := c.store.LeaseScheduledRunExecutions(ctx, 8)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	for _, lease := range leases {
		wg.Go(func() {
			// Leave room in the 30-second lease for persisting the result.
			attemptCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.reconcile(attemptCtx, lease)
			cancel()
			if err != nil && ctx.Err() == nil {
				logging.FromContext(ctx).ErrorContext(ctx, "failed to advance scheduled execution", "execution_id", lease.Execution.GetId(), "error", err)
			}
			if ctx.Err() != nil || lease.Execution.GetCompletedAt() != nil {
				return
			}
			persistCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			defer cancel()
			if err := c.store.UpdateScheduledRunExecution(persistCtx, lease.Lease, database.ScheduledRunExecutionProgress{State: lease.Execution.GetState(), TaskID: lease.Execution.GetTaskId(), FailureReason: lease.Execution.GetFailureReason()}); err != nil {
				logging.FromContext(ctx).ErrorContext(ctx, "failed to persist scheduled execution", "execution_id", lease.Execution.GetId(), "error", err)
			}
		})
	}
	wg.Wait()
	return nil
}

func (c *Controller) reconcile(ctx context.Context, leased database.LeasedScheduledRunExecution) error {
	execution := leased.Execution
	expired := !time.Now().Before(execution.GetDeadline().AsTime())
	if execution.GetSessionId() == "" {
		if expired {
			finishExecution(execution, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT, "Execution deadline elapsed")
			return nil
		}
		linked, err := c.store.ReserveScheduledRunExecutionSession(ctx, leased.Lease.ExecutionID, execution.GetCreator())
		if err != nil {
			return err
		}
		execution.SessionId = linked.GetSessionId()
		if linked.GetState() == apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT {
			// Reservation already persisted expiry under its row lock.
			finishExecution(execution, linked.GetState(), linked.GetFailureReason())
			execution.CompletedAt = linked.GetCompletedAt()
			return nil
		}
	}
	session, err := c.store.GetSession(ctx, execution.GetSessionId(), execution.GetCreator())
	if errors.Is(err, database.ErrNotFound) {
		finishExecution(execution, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_FAILED, "Session was deleted")
		return nil
	}
	if err != nil {
		return err
	}
	ctx = a2atype.AttachTenant(ctx, session.GetAgent().GetNamespace()+"/"+session.GetAgent().GetName())
	// A lost response may precede persisting the returned task ID. Recover it
	// from the original message, including at expiry when sending is forbidden.
	task, err := c.executionTask(ctx, execution)
	if err != nil {
		return err
	}
	if task != nil {
		execution.TaskId = string(task.ID)
		if task.Status.State.Terminal() {
			observeTask(execution, task)
			return nil
		}
	}
	if expired {
		stopped, err := c.stop(ctx, execution, session, task)
		if err != nil {
			return err
		}
		if stopped != nil && stopped.Status.State.Terminal() && stopped.Status.Timestamp != nil && !stopped.Status.Timestamp.After(execution.GetDeadline().AsTime()) {
			observeTask(execution, stopped)
			return nil
		}
		finishExecution(execution, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT, "Execution deadline elapsed")
		return nil
	}
	if execution.GetTaskId() != "" || execution.GetState() == apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING {
		// The runtime persists independently. Reconciliation only needs the
		// stored outcome and never opens a stream to keep execution alive. A
		// claimed send without a task ID remains uncertain until history appears
		// or the deadline expires; sending again could duplicate native work.
		return nil
	}
	dispatchCtx, cancel := context.WithDeadline(ctx, execution.GetDeadline().AsTime())
	defer cancel()
	if session.GetState() == apiv1alpha1.RuntimeState_RUNTIME_STATE_CREATING {
		session, err = c.workflow.Create(dispatchCtx, session)
		if err != nil {
			return err
		}
	}
	if session.GetState() != apiv1alpha1.RuntimeState_RUNTIME_STATE_READY || session.GetOperation() != apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		return fmt.Errorf("scheduled session %s is not ready for dispatch", session.GetId())
	}
	if err := c.store.ClaimScheduledRunDispatch(ctx, leased.Lease); err != nil {
		return err
	}
	execution.State = apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING
	message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(execution.GetPrompt()))
	message.ID = "scheduled-run/" + execution.GetId()
	message.ContextID = session.GetId()
	events := c.gateway.SendStreamingMessage(dispatchCtx, &a2atype.SendMessageRequest{Message: message})
	// The runtime assigns the task ID. Return after its first event; a lost
	// response is recovered by the original message on the next reconciliation.
	for event, err := range events {
		if event != nil && event.TaskInfo().TaskID != "" {
			execution.TaskId = string(event.TaskInfo().TaskID)
			execution.State = apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING
		}
		if task, ok := event.(*a2atype.Task); ok {
			observeTask(execution, task)
		}
		return err
	}
	return nil
}

// executionTask only reads persisted history. Once linked, the stored task ID
// is the sole execution identity.
// Unlinked executions are recovered by finding their original message in history.
func (c *Controller) executionTask(ctx context.Context, execution *apiv1alpha1.ScheduledRunExecution) (*a2atype.Task, error) {
	if taskID := execution.GetTaskId(); taskID != "" {
		zero := 0
		task, err := c.gateway.GetTask(ctx, &a2atype.GetTaskRequest{ID: a2atype.TaskID(taskID), HistoryLength: &zero})
		if errors.Is(err, a2atype.ErrTaskNotFound) {
			return nil, nil
		}
		return task, err
	}
	request := &a2atype.ListTasksRequest{PageSize: 100, ContextID: execution.GetSessionId()}
	for {
		page, err := c.gateway.ListTasks(ctx, request)
		if err != nil {
			return nil, err
		}
		for _, task := range page.Tasks {
			for _, message := range task.History {
				if message.ID == "scheduled-run/"+execution.GetId() {
					return task, nil
				}
			}
		}
		if page.NextPageToken == "" {
			return nil, nil
		}
		request.PageToken = page.NextPageToken
	}
}

func (c *Controller) stop(ctx context.Context, execution *apiv1alpha1.ScheduledRunExecution, session *apiv1alpha1.Session, task *a2atype.Task) (*a2atype.Task, error) {
	if task != nil && (task.Status.State.Terminal() || task.Status.State == a2atype.TaskStateInputRequired || task.Status.State == a2atype.TaskStateAuthRequired) {
		return task, nil
	}
	if execution.GetTaskId() != "" {
		return c.gateway.CancelTask(ctx, &a2atype.CancelTaskRequest{ID: a2atype.TaskID(execution.GetTaskId())})
	}
	// No invocation was accepted; clean up the ordinary session lifecycle.
	if session.GetState() == apiv1alpha1.RuntimeState_RUNTIME_STATE_CREATING {
		_, err := c.workflow.Delete(ctx, session)
		return nil, err
	}
	_, err := c.workflow.Suspend(ctx, session)
	return nil, err
}

func observeTask(execution *apiv1alpha1.ScheduledRunExecution, task *a2atype.Task) {
	execution.TaskId = string(task.ID)
	switch {
	case task.Status.State.Terminal() && task.Status.Timestamp != nil && task.Status.Timestamp.After(execution.GetDeadline().AsTime()):
		finishExecution(execution, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT, "Execution deadline elapsed")
	case task.Status.State == a2atype.TaskStateCompleted:
		finishExecution(execution, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED, "")
	case task.Status.State.Terminal():
		finishExecution(execution, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_FAILED, "A2A task "+string(task.Status.State))
	default:
		execution.State = apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING
	}
}

func finishExecution(execution *apiv1alpha1.ScheduledRunExecution, state apiv1alpha1.ScheduledRunExecutionState, reason string) {
	execution.State, execution.FailureReason = state, reason
}
