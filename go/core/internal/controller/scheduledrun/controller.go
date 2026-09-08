package scheduledrun

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/scheduledrun"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
	"google.golang.org/grpc/metadata"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type controllerStore interface {
	GetScheduledRun(context.Context, string, string) (*apiv1alpha1.ScheduledRun, error)
	ReserveScheduledRunExecutionInstance(context.Context, string, string) (*apiv1alpha1.ScheduledRunExecution, error)
	LeaseScheduledRunExecutions(context.Context, int) ([]database.LeasedScheduledRunExecution, error)
	UpdateScheduledRunExecution(context.Context, database.ScheduledRunExecutionLease, database.ScheduledRunExecutionProgress) error
	GetAgentInstance(context.Context, string, string) (*apiv1alpha1.AgentInstance, error)
}

type controllerWorkflow interface {
	Create(context.Context, *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error)
	Suspend(context.Context, *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error)
	Delete(context.Context, *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error)
}

// Controller reconciles executions on every replica. SQL leases fence status
// writes; A2A's initial-message uniqueness fences dispatch across replicas.
type Controller struct {
	store      controllerStore
	workflow   controllerWorkflow
	gateway    a2asrv.RequestHandler
	authorizer auth.Authorizer
}

var _ manager.LeaderElectionRunnable = (*Controller)(nil)
var _ manager.Runnable = (*Controller)(nil)

func NewController(store controllerStore, workflow controllerWorkflow, gateway a2asrv.RequestHandler, authorizer auth.Authorizer) *Controller {
	return &Controller{store: store, workflow: workflow, gateway: gateway, authorizer: authorizer}
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
			err := c.reconcile(attemptCtx, lease.Execution)
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
	// ponytail: each batch waits for its slowest reconciliation. We'll likely
	// need a custom work queue that leases more work as capacity becomes available,
	// without blocking on the whole batch's results.
	wg.Wait()
	return nil
}

func (c *Controller) reconcile(ctx context.Context, execution *apiv1alpha1.ScheduledRunExecution) error {
	expired := !time.Now().Before(execution.GetDeadline().AsTime())
	if execution.GetAgentInstanceId() == "" {
		if expired {
			finishExecution(execution, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT, "Execution deadline elapsed")
			return nil
		}
		if err := c.authorize(ctx, execution); err != nil {
			if serviceerrors.CodeOf(err) == serviceerrors.CodePermissionDenied {
				finishExecution(execution, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_FAILED, "Control-plane execution is not authorized")
			}
			return err
		}
		linked, err := c.store.ReserveScheduledRunExecutionInstance(ctx, execution.GetId(), execution.GetCreator())
		if err != nil {
			return err
		}
		execution.AgentInstanceId = linked.GetAgentInstanceId()
		if linked.GetState() == apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT {
			// Reservation already persisted expiry under its row lock.
			finishExecution(execution, linked.GetState(), linked.GetFailureReason())
			execution.CompletedAt = linked.GetCompletedAt()
			return nil
		}
	}
	instance, err := c.store.GetAgentInstance(ctx, execution.GetAgentInstanceId(), execution.GetCreator())
	if errors.Is(err, database.ErrNotFound) {
		finishExecution(execution, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_FAILED, "AgentInstance was deleted")
		return nil
	}
	if err != nil {
		return err
	}
	ctx = metadata.NewIncomingContext(ctx, metadata.Pairs(apia2a.AgentInstanceIDHeader, instance.GetId()))
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
		stopped, err := c.stop(ctx, execution, instance, task)
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
	if err := c.authorize(ctx, execution); err != nil {
		if serviceerrors.CodeOf(err) != serviceerrors.CodePermissionDenied {
			return err
		}
		if _, cleanupErr := c.stop(ctx, execution, instance, task); cleanupErr != nil {
			return errors.Join(err, cleanupErr)
		}
		finishExecution(execution, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_FAILED, "Control-plane execution is not authorized")
		return err
	}
	if execution.GetTaskId() != "" {
		// Attach to or recover the live ingester. It persists updates independently
		// of this observer; subsequent reconciliations read those durable updates.
		for _, err := range c.gateway.SubscribeToTask(ctx, &a2atype.SubscribeToTaskRequest{ID: a2atype.TaskID(execution.GetTaskId())}) {
			return err
		}
		return nil
	}
	dispatchCtx, cancel := context.WithDeadline(ctx, execution.GetDeadline().AsTime())
	defer cancel()
	if instance.GetState() == apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING {
		instance, err = c.workflow.Create(dispatchCtx, instance)
		if err != nil {
			return err
		}
	}
	if instance.GetState() != apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY || instance.GetOperation() != apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_UNSPECIFIED {
		return fmt.Errorf("scheduled instance %s is not ready for dispatch", instance.GetId())
	}
	message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(execution.GetPrompt()))
	message.ID = "scheduled-run/" + execution.GetId()
	events := c.gateway.SendStreamingMessage(dispatchCtx, &a2atype.SendMessageRequest{Message: message})
	// Like the MCP boundary, retain the ID assigned when the gateway accepted
	// the message, even if the runtime response was lost.
	execution.TaskId = string(message.TaskID)
	// The gateway ingests the runtime stream independently of this observer.
	// Return after acceptance so reconciliation does not wait for completion.
	for event, err := range events {
		if task, ok := event.(*a2atype.Task); ok {
			observeTask(execution, task)
		}
		return err
	}
	return nil
}

// executionTask only reads persisted history; the live subscription ingests
// running work. Once linked, the stored task ID is the sole execution identity.
// ponytail: scan the instance's task list; add protocol filtering if long-lived
// scheduled conversations make pagination costly.
func (c *Controller) executionTask(ctx context.Context, execution *apiv1alpha1.ScheduledRunExecution) (*a2atype.Task, error) {
	request := &a2atype.ListTasksRequest{PageSize: 100}
	if execution.GetTaskId() != "" {
		zero := 0
		request.HistoryLength = &zero
	}
	for {
		page, err := c.gateway.ListTasks(ctx, request)
		if err != nil {
			return nil, err
		}
		for _, task := range page.Tasks {
			if string(task.ID) == execution.GetTaskId() {
				return task, nil
			}
			if execution.GetTaskId() != "" {
				continue
			}
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

func (c *Controller) authorize(ctx context.Context, execution *apiv1alpha1.ScheduledRunExecution) error {
	principal := auth.ControlPlaneSession{}.Principal()
	if err := c.authorizer.Check(ctx, principal, auth.VerbCreate,
		auth.Resource{Type: "ScheduledRunExecution", Name: execution.GetId()}); err != nil {
		return serviceerrors.NewPermissionDenied("Not authorized to execute schedule", err)
	}
	schedule, err := c.store.GetScheduledRun(ctx, execution.GetScheduledRunId(), execution.GetCreator())
	if err != nil {
		return err
	}
	return scheduledrun.AuthorizeTarget(ctx, c.authorizer, principal, schedule)
}

func (c *Controller) stop(ctx context.Context, execution *apiv1alpha1.ScheduledRunExecution, instance *apiv1alpha1.AgentInstance, task *a2atype.Task) (*a2atype.Task, error) {
	if task != nil && (task.Status.State.Terminal() || task.Status.State == a2atype.TaskStateInputRequired || task.Status.State == a2atype.TaskStateAuthRequired) {
		return task, nil
	}
	if execution.GetTaskId() != "" {
		return c.gateway.CancelTask(ctx, &a2atype.CancelTaskRequest{ID: a2atype.TaskID(execution.GetTaskId())})
	}
	// No invocation was accepted; clean up the ordinary instance lifecycle.
	if instance.GetState() == apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING {
		_, err := c.workflow.Delete(ctx, instance)
		return nil, err
	}
	_, err := c.workflow.Suspend(ctx, instance)
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
