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
	"sigs.k8s.io/controller-runtime/pkg/manager"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/scheduling"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/pkg/logging"
)

type controllerStore interface {
	GetSessionTaskByMessage(context.Context, string, string, string) (*a2atype.Task, error)
	RetryUnacceptedScheduledRun(context.Context, database.ScheduledRunExecutionLease) error
	ScheduledMessageMayRun(context.Context, string, string) (bool, error)
	ReserveScheduledRunExecutionSession(context.Context, uuid.UUID, string) (*apiv1.ScheduledRunExecution, error)
	ClaimScheduledRunDispatch(context.Context, database.ScheduledRunExecutionLease) error
	LeaseScheduledRunExecutions(context.Context, int) ([]database.LeasedScheduledRunExecution, error)
	UpdateScheduledRunExecution(context.Context, database.ScheduledRunExecutionLease, database.ScheduledRunExecutionProgress) error
	GetSession(context.Context, string, string) (*apiv1.Session, error)
}

type controllerWorkflow interface {
	Create(context.Context, *apiv1.Session) (*apiv1.Session, error)
	Suspend(context.Context, *apiv1.Session) (*apiv1.Session, error)
	Delete(context.Context, *apiv1.Session, bool) (*apiv1.Session, error)
}

// Controller reconciles executions on every replica. SQL leases fence status
// writes; a durable dispatch claim prevents resending after an uncertain result.
type Controller struct {
	store                 controllerStore
	workflow              controllerWorkflow
	gateway               a2asrv.RequestHandler
	pollInterval          time.Duration
	recoverCrashedSession func(context.Context, string) (*apiv1.Session, error)
}

var _ manager.LeaderElectionRunnable = (*Controller)(nil)
var _ manager.Runnable = (*Controller)(nil)

func NewController(store controllerStore, workflow controllerWorkflow, gateway a2asrv.RequestHandler, recoverCrashedSession func(context.Context, string) (*apiv1.Session, error), pollInterval time.Duration) *Controller {
	return &Controller{store: store, workflow: workflow, gateway: gateway, recoverCrashedSession: recoverCrashedSession, pollInterval: pollInterval}
}

func (*Controller) NeedLeaderElection() bool { return false }

func (c *Controller) Start(ctx context.Context) error {
	if c.pollInterval <= 0 {
		return fmt.Errorf("scheduled run execution poll interval must be positive")
	}
	ctx = auth.AuthSessionTo(ctx, auth.ControlPlaneSession{})
	ticker := time.NewTicker(c.pollInterval)
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
			attemptCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
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
			finishExecution(execution, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT, "Execution deadline elapsed before session reservation")
			return nil
		}
		linked, err := c.store.ReserveScheduledRunExecutionSession(ctx, leased.Lease.ExecutionID, execution.GetCreator())
		if err != nil {
			return err
		}
		execution.SessionId = linked.GetSessionId()
		if linked.GetState() == apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT {
			// Reservation already persisted expiry under its row lock.
			finishExecution(execution, linked.GetState(), linked.GetFailureReason())
			execution.CompletedAt = linked.GetCompletedAt()
			return nil
		}
	}
	session, err := c.store.GetSession(ctx, execution.GetSessionId(), execution.GetCreator())
	if errors.Is(err, database.ErrNotFound) {
		finishExecution(execution, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_FAILED, "Session was deleted")
		return nil
	}
	if err != nil {
		return err
	}
	ctx = a2atype.AttachTenant(ctx, session.GetAgent().GetRef().GetNamespace()+"/"+session.GetAgent().GetRef().GetName())
	// A lost response may precede persisting the returned task ID. Recover it
	// from the original message, including at expiry when sending is forbidden.
	var task *a2atype.Task
	if execution.GetTaskId() != "" || execution.GetState() == apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING {
		task, err = c.executionTask(ctx, execution)
		if err != nil {
			return err
		}
	}
	if task != nil {
		execution.TaskId = string(task.ID)
		if task.Status.State.Terminal() {
			observeTask(execution, task)
			return nil
		}
	}
	if expired {
		return c.expire(ctx, execution, session, task)
	}
	if execution.GetTaskId() != "" || execution.GetState() == apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING {
		// The runtime persists independently. Reconciliation only needs the
		// stored outcome and never opens a stream to keep execution alive. A
		// claimed send without a task ID remains uncertain until history appears
		// or the deadline expires; sending again could duplicate native work.
		return nil
	}
	dispatchCtx, cancel := context.WithDeadline(ctx, execution.GetDeadline().AsTime())
	defer cancel()
	if session.GetState() == apiv1.RuntimeState_RUNTIME_STATE_CREATING {
		session, err = c.workflow.Create(dispatchCtx, session)
		if err != nil {
			return err
		}
	}
	if execution.ContinueConversation && database.RuntimeLost(session) {
		finishExecution(execution, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_FAILED, "Session runtime was lost. Start a new conversation.")
		return nil
	}
	if database.CrashRecoveryAdmitted(session) && execution.ContinueConversation {
		execution.FailureReason = "Session crash recovery is pending"
		if c.recoverCrashedSession != nil {
			_, err := c.recoverCrashedSession(dispatchCtx, session.Id)
			return err
		}
		return nil
	}
	if session.GetState() != apiv1.RuntimeState_RUNTIME_STATE_READY || session.GetOperation() != apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE {
		return fmt.Errorf("scheduled session %s is not ready for dispatch", session.GetId())
	}
	if err := c.store.ClaimScheduledRunDispatch(ctx, leased.Lease); err != nil {
		return err
	}
	execution.State = apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING
	message := a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart(execution.GetPrompt()))
	message.ID = scheduling.MessageIDPrefix + execution.GetId()
	message.ContextID = session.GetId()
	events := c.gateway.SendStreamingMessage(dispatchCtx, &a2atype.SendMessageRequest{Message: message})
	// The runtime assigns the task ID. Return after its first event; a lost
	// response is recovered by the original message on the next reconciliation.
	for event, err := range events {
		if event == nil && sessionsvc.IsUnacceptedSend(err) {
			if retryErr := c.store.RetryUnacceptedScheduledRun(ctx, leased.Lease); retryErr != nil {
				return retryErr
			}
			execution.State = apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_PENDING
			execution.FailureReason = err.Error()
			return nil
		}
		if event != nil && event.TaskInfo().TaskID != "" {
			execution.FailureReason = ""
			execution.TaskId = string(event.TaskInfo().TaskID)
			execution.State = apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING
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
func (c *Controller) executionTask(ctx context.Context, execution *apiv1.ScheduledRunExecution) (*a2atype.Task, error) {
	if taskID := execution.GetTaskId(); taskID != "" {
		zero := 0
		task, err := c.gateway.GetTask(ctx, &a2atype.GetTaskRequest{ID: a2atype.TaskID(taskID), HistoryLength: &zero})
		if errors.Is(err, a2atype.ErrTaskNotFound) {
			return nil, nil
		}
		return task, err
	}
	// A continuing conversation may contain years of history. Look up its
	// reserved message ID in the session-scoped event index.
	if execution.ContinueConversation {
		task, err := c.store.GetSessionTaskByMessage(ctx, execution.SessionId, "", scheduling.MessageIDPrefix+execution.Id)
		if errors.Is(err, database.ErrNotFound) {
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
				if message.ID == scheduling.MessageIDPrefix+execution.GetId() {
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

// expire settles only this firing. A shared conversation stays blocked while
// an earlier send or its cancellation could still affect the runtime.
func (c *Controller) expire(ctx context.Context, execution *apiv1.ScheduledRunExecution, session *apiv1.Session, task *a2atype.Task) error {
	if execution.ContinueConversation && task == nil && execution.GetState() == apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING {
		possible, err := c.store.ScheduledMessageMayRun(ctx, execution.SessionId, scheduling.MessageIDPrefix+execution.Id)
		if err != nil {
			return err
		}
		if possible {
			execution.FailureReason = "Waiting for the previous dispatch to settle"
			return nil
		}
	}
	stopped, err := c.stop(ctx, execution, session, task)
	if err != nil {
		if execution.ContinueConversation {
			execution.FailureReason = "Cancellation has not settled. Check the session and recover it if the runtime crashed."
			if c.recoverCrashedSession != nil {
				// A failed cancel may have used its whole attempt. Recovery
				// verifies a crash before reverting and leaves healthy work alone.
				recoveryCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer cancel()
				_, recoveryErr := c.recoverCrashedSession(recoveryCtx, session.Id)
				return errors.Join(err, recoveryErr)
			}
		}
		return err
	}
	if execution.ContinueConversation && execution.TaskId != "" && (stopped == nil ||
		stopped.Status.State == a2atype.TaskStateWorking || stopped.Status.State == a2atype.TaskStateSubmitted) {
		return nil
	}
	// Cancellation requested for this deadline is a timeout even if the
	// runtime clock trails ours. Preserve other outcomes that won the race.
	if stopped != nil && stopped.Status.State.Terminal() && stopped.Status.State != a2atype.TaskStateCanceled && stopped.Status.Timestamp != nil && !stopped.Status.Timestamp.After(execution.GetDeadline().AsTime()) {
		observeTask(execution, stopped)
		return nil
	}
	reason := "Execution deadline elapsed"
	if execution.FailureReason != "" {
		reason += ": " + execution.FailureReason
	}
	finishExecution(execution, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT, reason)
	return nil
}

func (c *Controller) stop(ctx context.Context, execution *apiv1.ScheduledRunExecution, session *apiv1.Session, task *a2atype.Task) (*a2atype.Task, error) {
	if task != nil && (task.Status.State.Terminal() || task.Status.State == a2atype.TaskStateInputRequired || task.Status.State == a2atype.TaskStateAuthRequired) {
		return task, nil
	}
	if execution.GetTaskId() != "" {
		return c.gateway.CancelTask(ctx, &a2atype.CancelTaskRequest{ID: a2atype.TaskID(execution.GetTaskId())})
	}
	if execution.ContinueConversation {
		return nil, nil
	}
	// No invocation was accepted; clean up the ordinary session lifecycle.
	if session.GetState() == apiv1.RuntimeState_RUNTIME_STATE_CREATING {
		_, err := c.workflow.Delete(ctx, session, false)
		return nil, err
	}
	_, err := c.workflow.Suspend(ctx, session)
	return nil, err
}

func observeTask(execution *apiv1.ScheduledRunExecution, task *a2atype.Task) {
	execution.TaskId = string(task.ID)
	switch {
	case task.Status.State.Terminal() && task.Status.Timestamp != nil && task.Status.Timestamp.After(execution.GetDeadline().AsTime()):
		finishExecution(execution, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT, "Execution deadline elapsed")
	case task.Status.State == a2atype.TaskStateCompleted:
		finishExecution(execution, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED, "")
	case task.Status.State.Terminal():
		finishExecution(execution, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_FAILED, "A2A task "+string(task.Status.State))
	default:
		execution.State = apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING
	}
}

func finishExecution(execution *apiv1.ScheduledRunExecution, state apiv1.ScheduledRunExecutionState, reason string) {
	execution.State, execution.FailureReason = state, reason
}
