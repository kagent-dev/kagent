package scheduledrun

import (
	"context"
	"errors"
	"iter"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
	"k8s.io/apimachinery/pkg/types"

	"github.com/kagent-dev/kagent/go/api/authorization"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type controllerTestStore struct {
	controllerStore
	session *apiv1.Session
}

func (s controllerTestStore) GetSession(context.Context, string, string) (*apiv1.Session, error) {
	if s.session == nil {
		return nil, database.ErrNotFound
	}
	return s.session, nil
}

type controllerTestCleanup struct {
	controllerWorkflow
	a2asrv.RequestHandler
	task             *a2atype.Task
	canceledTask     *a2atype.Task
	err              error
	deletes, expires int
	checkContext     func(context.Context)
}

func (c *controllerTestCleanup) Delete(ctx context.Context, session *apiv1.Session) (*apiv1.Session, error) {
	if c.checkContext != nil {
		c.checkContext(ctx)
	}
	c.deletes++
	return session, c.err
}

func (c *controllerTestCleanup) CancelTask(ctx context.Context, request *a2atype.CancelTaskRequest) (*a2atype.Task, error) {
	if c.checkContext != nil {
		c.checkContext(ctx)
	}
	c.expires++
	if c.err != nil {
		return nil, c.err
	}
	if c.canceledTask != nil {
		return c.canceledTask, nil
	}
	if c.task != nil {
		return c.task, nil
	}
	now := time.Now()
	return &a2atype.Task{ID: request.ID, Status: a2atype.TaskStatus{State: a2atype.TaskStateCanceled, Timestamp: &now}}, nil
}

func (c *controllerTestCleanup) GetTask(context.Context, *a2atype.GetTaskRequest) (*a2atype.Task, error) {
	if c.task != nil {
		return c.task, nil
	}
	return nil, a2atype.ErrTaskNotFound
}

func (c *controllerTestCleanup) Suspend(ctx context.Context, session *apiv1.Session) (*apiv1.Session, error) {
	if c.checkContext != nil {
		c.checkContext(ctx)
	}
	c.expires++
	return session, c.err
}

func TestControllerRecoversCleanupWithoutReplacingSession(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state apiv1.RuntimeState
	}{
		{"creating timeout", apiv1.RuntimeState_RUNTIME_STATE_CREATING},
		{"running timeout", apiv1.RuntimeState_RUNTIME_STATE_READY},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deadline := time.Now().Add(-time.Minute)
			execution := &apiv1.ScheduledRunExecution{Id: "execution", Creator: schedulerID, SessionId: "original", TaskId: "original-task", State: apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_PENDING, Deadline: timestamppb.New(deadline)}
			store := controllerTestStore{session: &apiv1.Session{Id: "original", State: tc.state}}
			cleanup := &controllerTestCleanup{err: errors.New("Substrate unavailable")}
			controller := NewController(store, cleanup, cleanup, machineSession(), time.Second)
			require.Error(t, controller.reconcile(t.Context(), database.LeasedScheduledRunExecution{Execution: execution}))
			require.Equal(t, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_PENDING, execution.State)
			cleanup.err = nil
			require.NoError(t, controller.reconcile(t.Context(), database.LeasedScheduledRunExecution{Execution: execution}))
			require.Equal(t, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT, execution.State)
			require.Equal(t, "original", execution.SessionId)
			require.Equal(t, 2, cleanup.deletes+cleanup.expires)
		})
	}
	// No reserve method is supplied: a missing historical session must never
	// call it, even when the execution was still PENDING when deletion happened.
	execution := &apiv1.ScheduledRunExecution{Id: "execution", Creator: schedulerID, SessionId: "deleted", Deadline: timestamppb.New(time.Now().Add(time.Minute))}
	require.NoError(t, NewController(controllerTestStore{}, nil, nil, machineSession(), time.Second).reconcile(t.Context(), database.LeasedScheduledRunExecution{Execution: execution}))
	require.Equal(t, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_FAILED, execution.State)
	require.Equal(t, "deleted", execution.SessionId)
}

func TestControllerKeepsCompletedOutcomeAfterDeadline(t *testing.T) {
	completedAt := time.Now().Add(-2 * time.Minute)
	execution := &apiv1.ScheduledRunExecution{Id: "execution", Creator: schedulerID, SessionId: "original", TaskId: "original-task", Deadline: timestamppb.New(completedAt.Add(time.Minute))}
	store := controllerTestStore{session: &apiv1.Session{Id: "original"}}
	gateway := &controllerTestCleanup{task: &a2atype.Task{ID: "original-task", Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted, Timestamp: &completedAt}}}
	require.NoError(t, NewController(store, nil, gateway, machineSession(), time.Second).reconcile(t.Context(), database.LeasedScheduledRunExecution{Execution: execution}))
	require.Equal(t, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED, execution.State)
}

func TestControllerDeadlineCancellationOutcome(t *testing.T) {
	deadline := time.Now().Add(-time.Minute)
	before := deadline.Add(-time.Second)
	after := deadline.Add(time.Second)
	for _, tc := range []struct {
		name      string
		state     a2atype.TaskState
		timestamp *time.Time
		want      apiv1.ScheduledRunExecutionState
	}{
		{"canceled with a slow runtime clock", a2atype.TaskStateCanceled, &before, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT},
		{"canceled at deadline", a2atype.TaskStateCanceled, &deadline, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT},
		{"canceled after deadline", a2atype.TaskStateCanceled, &after, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT},
		{"canceled without timestamp", a2atype.TaskStateCanceled, nil, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT},
		{"completed before cancellation", a2atype.TaskStateCompleted, &before, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED},
		{"completed after deadline", a2atype.TaskStateCompleted, &after, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT},
	} {
		t.Run(tc.name, func(t *testing.T) {
			execution := &apiv1.ScheduledRunExecution{Id: "execution", Creator: schedulerID, SessionId: "original", TaskId: "original-task", Deadline: timestamppb.New(deadline)}
			store := controllerTestStore{session: &apiv1.Session{Id: "original"}}
			gateway := &controllerTestCleanup{
				task:         &a2atype.Task{ID: "original-task", Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking}},
				canceledTask: &a2atype.Task{ID: "original-task", Status: a2atype.TaskStatus{State: tc.state, Timestamp: tc.timestamp}},
			}
			require.NoError(t, NewController(store, nil, gateway, machineSession(), time.Second).reconcile(t.Context(), database.LeasedScheduledRunExecution{Execution: execution}))
			require.Equal(t, 1, gateway.expires)
			require.Equal(t, tc.want, execution.State)
		})
	}
}

type taskLookupGateway struct {
	a2asrv.RequestHandler
	getTask   func(*a2atype.GetTaskRequest) (*a2atype.Task, error)
	listTasks func(*a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error)
}

func (g taskLookupGateway) GetTask(_ context.Context, request *a2atype.GetTaskRequest) (*a2atype.Task, error) {
	return g.getTask(request)
}

func (g taskLookupGateway) ListTasks(_ context.Context, request *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	return g.listTasks(request)
}

func TestExecutionTaskUsesLinkedIdentity(t *testing.T) {
	task := &a2atype.Task{ID: "original-task"}
	lookupErr := errors.New("task lookup unavailable")
	for _, tc := range []struct {
		name    string
		task    *a2atype.Task
		err     error
		wantErr error
	}{
		{name: "found", task: task},
		{name: "missing", err: a2atype.ErrTaskNotFound},
		{name: "lookup failure", err: lookupErr, wantErr: lookupErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			gateway := taskLookupGateway{getTask: func(request *a2atype.GetTaskRequest) (*a2atype.Task, error) {
				calls++
				require.Equal(t, task.ID, request.ID)
				require.NotNil(t, request.HistoryLength)
				require.Zero(t, *request.HistoryLength)
				return tc.task, tc.err
			}}
			// ListTasks is deliberately absent: linked tasks, including missing
			// ones, must never fall back to scanning for a different identity.
			execution := &apiv1.ScheduledRunExecution{Id: "execution", Creator: schedulerID, TaskId: string(task.ID)}
			got, err := NewController(nil, nil, gateway, machineSession(), time.Second).executionTask(t.Context(), execution)
			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.task, got)
			require.Equal(t, 1, calls)
			require.Equal(t, string(task.ID), execution.GetTaskId())
		})
	}
}

func TestExecutionTaskRecoversUnlinkedIdentity(t *testing.T) {
	task := &a2atype.Task{ID: "recovered-task", History: []*a2atype.Message{{ID: "scheduled-run/execution"}}}
	lookupErr := errors.New("history lookup unavailable")
	for _, tc := range []struct {
		name string
		task *a2atype.Task
		err  error
	}{
		{name: "found on later page", task: task},
		{name: "missing"},
		{name: "lookup failure", err: lookupErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			gateway := taskLookupGateway{listTasks: func(request *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
				calls++
				require.Equal(t, 100, request.PageSize)
				require.Nil(t, request.HistoryLength)
				if calls == 1 {
					require.Empty(t, request.PageToken)
					return &a2atype.ListTasksResponse{
						Tasks:         []*a2atype.Task{{ID: "other-task", History: []*a2atype.Message{{ID: "scheduled-run/other-execution"}}}},
						NextPageToken: "next-page",
					}, nil
				}
				require.Equal(t, "next-page", request.PageToken)
				if tc.err != nil {
					return nil, tc.err
				}
				page := &a2atype.ListTasksResponse{}
				if tc.task != nil {
					page.Tasks = []*a2atype.Task{tc.task}
				}
				return page, nil
			}}
			execution := &apiv1.ScheduledRunExecution{Id: "execution", Creator: schedulerID}
			got, err := NewController(nil, nil, gateway, machineSession(), time.Second).executionTask(t.Context(), execution)
			require.ErrorIs(t, err, tc.err)
			require.Equal(t, tc.task, got)
			require.Equal(t, 2, calls)
		})
	}
}

func TestControllerDoesNotRepeatAnUncertainDispatch(t *testing.T) {
	execution := &apiv1.ScheduledRunExecution{
		Creator: schedulerID, Id: "execution", SessionId: "session",
		State:    apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING,
		Deadline: timestamppb.New(time.Now().Add(time.Minute)),
	}
	store := controllerTestStore{session: &apiv1.Session{
		Id: "session", State: apiv1.RuntimeState_RUNTIME_STATE_READY,
		Operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE,
	}}
	reads := 0
	gateway := taskLookupGateway{listTasks: func(*a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
		reads++
		return &a2atype.ListTasksResponse{}, nil
	}}
	// Neither dispatch nor workflow methods are supplied: even an empty history
	// after a lost response must never trigger another send.
	controller := NewController(store, nil, gateway, machineSession(), time.Second)
	require.NoError(t, controller.reconcile(t.Context(), database.LeasedScheduledRunExecution{Execution: execution}))
	require.Equal(t, 1, reads)
	require.Empty(t, execution.TaskId)
	require.Equal(t, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING, execution.State)
}

// This user policy denies everything. Scheduled dispatch must not consult it:
// accepting the schedule was the authorization decision.
type controllerSessionPolicy struct{ calls []auth.Verb }

func (p *controllerSessionPolicy) Check(_ context.Context, _ auth.Principal, verb auth.Verb, _ auth.Resource) error {
	p.calls = append(p.calls, verb)
	return errors.New("operation denied")
}

func (p *controllerSessionPolicy) Scope(context.Context, auth.Principal, auth.Verb, string) (authorization.AuthorizationScope, error) {
	return authorization.AuthorizationScope{Kind: authorization.ScopeNone}, nil
}

type scopedControllerStore struct {
	*database.Client
	session  *apiv1.Session
	leased   bool
	reserved bool
}

func (s *scopedControllerStore) LeaseScheduledRunExecutions(context.Context, int) ([]database.LeasedScheduledRunExecution, error) {
	if s.leased {
		return nil, nil
	}
	s.leased = true
	return []database.LeasedScheduledRunExecution{{Execution: &apiv1.ScheduledRunExecution{Id: "execution", Creator: schedulerID, SessionId: s.session.Id, Deadline: timestamppb.New(time.Now().Add(time.Minute))}}}, nil
}
func (s *scopedControllerStore) GetSession(_ context.Context, id, owner string) (*apiv1.Session, error) {
	if id != s.session.Id || owner != schedulerID {
		return nil, database.ErrNotFound
	}
	return s.session, nil
}
func (s *scopedControllerStore) GetSessionByID(_ context.Context, id string) (*apiv1.Session, error) {
	if id != s.session.Id {
		return nil, database.ErrNotFound
	}
	return s.session, nil
}
func (s *scopedControllerStore) ListAgentTasks(context.Context, []string, string, a2atype.TaskState, *time.Time, int, *int) ([]*a2atype.Task, int, error) {
	return nil, 0, nil
}
func (s *scopedControllerStore) ReserveSessionDispatch(context.Context, string, uuid.UUID, string) error {
	s.reserved = true
	return nil
}
func (*scopedControllerStore) ClaimScheduledRunDispatch(context.Context, database.ScheduledRunExecutionLease) error {
	return nil
}
func (*scopedControllerStore) UpdateScheduledRunExecution(context.Context, database.ScheduledRunExecutionLease, database.ScheduledRunExecutionProgress) error {
	return nil
}

type scopedControllerGateway struct {
	a2asrv.RequestHandler
	interactions *sessionsvc.InteractionService
	cancel       context.CancelFunc
	agent        types.NamespacedName
}

func (g scopedControllerGateway) ListTasks(ctx context.Context, request *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	return g.interactions.ListTasks(ctx, types.NamespacedName{Namespace: g.agent.Namespace, Name: g.agent.Name}, request)
}
func (g scopedControllerGateway) SendStreamingMessage(ctx context.Context, request *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		_, err := g.interactions.PrepareSend(ctx, types.NamespacedName{Namespace: g.agent.Namespace, Name: g.agent.Name}, request)
		g.cancel()
		yield(&a2atype.Task{ID: "task", Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}}, err)
	}
}
func TestControllerDispatchUsesBoundedInProcessAuthority(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	store := &scopedControllerStore{session: &apiv1.Session{Id: id, ContextId: id, Creator: schedulerID, State: apiv1.RuntimeState_RUNTIME_STATE_READY, Operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE, Agent: &apiv1.AgentProvenance{Ref: &apiv1.ResourceReference{Namespace: "team", Name: "assistant"}}, ScheduledRun: &apiv1.ScheduledRunProvenance{Ref: &apiv1.ResourceReference{Namespace: "team", Name: "daily"}}}}
	policy := &controllerSessionPolicy{}
	sessions := sessionsvc.NewService(store, runtimeauth.NewAuthorizer(policy, nil), nil)
	interactions := sessionsvc.NewInteractionService(store, nil, sessions, false)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	gateway := scopedControllerGateway{interactions: interactions, cancel: cancel, agent: types.NamespacedName{Namespace: "team", Name: "assistant"}}
	controller := NewController(store, nil, gateway, machineSession(), time.Millisecond)
	require.NoError(t, controller.Start(ctx))
	require.True(t, store.reserved)
	require.Empty(t, policy.calls, "scheduled dispatch must bypass user policy")
	// Human principals cannot acquire internal authority by copying the creator ID.
	forged := auth.AuthSessionTo(t.Context(), controllerHumanSession{})
	_, err := sessions.Get(forged, id)
	require.Error(t, err)
}

type controllerHumanSession struct{}

func (controllerHumanSession) Principal() auth.Principal {
	return auth.Principal{ID: schedulerID, Claims: map[string]any{"control_plane": true}}
}

var schedulerID = auth.NewSchedulerSession("kagent", nil).Principal().ID

func machineSession() auth.Session {
	return auth.NewSchedulerSession("kagent", staticToken("outbound"))
}

type staticToken string

func (t staticToken) Token(context.Context) (string, error) { return string(t), nil }

func (c *controllerTestCleanup) ListTasks(ctx context.Context, _ *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	if c.checkContext != nil {
		c.checkContext(ctx)
	}
	if c.task == nil {
		return &a2atype.ListTasksResponse{}, nil
	}
	return &a2atype.ListTasksResponse{Tasks: []*a2atype.Task{c.task}}, nil
}

func TestControllerCleansUpAfterSchedulerIdentityChanges(t *testing.T) {
	for _, name := range []string{"provisioning", "ready", "active task", "lost task response"} {
		t.Run(name, func(t *testing.T) {
			execution := &apiv1.ScheduledRunExecution{Id: "execution", Creator: "old-scheduler", SessionId: "original", State: apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING, Deadline: timestamppb.New(time.Now().Add(time.Hour))}
			session := &apiv1.Session{Id: "original", Creator: execution.Creator, State: apiv1.RuntimeState_RUNTIME_STATE_READY}
			cleanup := &controllerTestCleanup{err: errors.New("cleanup unavailable"), checkContext: func(ctx context.Context) {
				caller, ok := auth.AuthSessionFrom(ctx)
				require.True(t, ok)
				require.IsType(t, auth.ControlPlaneSession{}, caller)
				require.Empty(t, caller.Principal().ID, "cleanup must not impersonate the previous scheduler")
			}}
			switch name {
			case "provisioning":
				session.State = apiv1.RuntimeState_RUNTIME_STATE_CREATING
			case "active task", "lost task response":
				cleanup.task = &a2atype.Task{ID: "accepted-task", Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking}, History: []*a2atype.Message{{ID: "scheduled-run/execution"}}}
				cleanup.canceledTask = &a2atype.Task{ID: "accepted-task", Status: a2atype.TaskStatus{State: a2atype.TaskStateCanceled}}
				if name == "active task" {
					execution.TaskId = string(cleanup.task.ID)
				}
			}
			controller := NewController(controllerTestStore{session: session}, cleanup, cleanup, machineSession(), time.Second)
			leased := database.LeasedScheduledRunExecution{Execution: execution}
			require.ErrorContains(t, controller.reconcile(t.Context(), leased), "cleanup unavailable")
			require.Equal(t, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING, execution.State)
			require.Empty(t, execution.FailureReason)
			cleanup.err = nil
			require.NoError(t, controller.reconcile(t.Context(), leased))
			require.Equal(t, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_FAILED, execution.State)
			require.Contains(t, execution.FailureReason, "Scheduler identity changed")
			require.Equal(t, 2, cleanup.deletes+cleanup.expires)
		})
	}
}

type cleanupOnlyStore struct {
	controllerTestStore
	execution *apiv1.ScheduledRunExecution
	progress  database.ScheduledRunExecutionProgress
	persisted bool
	cancel    context.CancelFunc
}

func (s *cleanupOnlyStore) LeaseScheduledRunExecutions(context.Context, int) ([]database.LeasedScheduledRunExecution, error) {
	return []database.LeasedScheduledRunExecution{{Execution: s.execution}}, nil
}

func (s *cleanupOnlyStore) UpdateScheduledRunExecution(_ context.Context, _ database.ScheduledRunExecutionLease, progress database.ScheduledRunExecutionProgress) error {
	s.progress, s.persisted = progress, true
	s.cancel()
	return nil
}

func TestControllerStartWithoutCredentialsReconcilesAcceptedExecutions(t *testing.T) {
	for _, name := range []string{"pending", "running", "completed"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			execution := &apiv1.ScheduledRunExecution{Id: "execution", Creator: schedulerID, SessionId: "session", State: apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_PENDING, Deadline: timestamppb.New(time.Now().Add(time.Hour))}
			cleanup := &controllerTestCleanup{checkContext: func(ctx context.Context) {
				caller, ok := auth.AuthSessionFrom(ctx)
				require.True(t, ok)
				require.IsType(t, auth.ControlPlaneSession{}, caller)
			}}
			want := apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_FAILED
			if name != "pending" {
				execution.State = apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING
				execution.TaskId = "task"
				cleanup.task = &a2atype.Task{ID: "task", Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking}}
				cleanup.canceledTask = &a2atype.Task{ID: "task", Status: a2atype.TaskStatus{State: a2atype.TaskStateCanceled}}
				if name == "completed" {
					cleanup.task.Status.State = a2atype.TaskStateCompleted
					want = apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED
				}
			}
			store := &cleanupOnlyStore{controllerTestStore: controllerTestStore{session: &apiv1.Session{Id: execution.SessionId, State: apiv1.RuntimeState_RUNTIME_STATE_CREATING}}, execution: execution, cancel: cancel}
			// No dispatch or creation methods are supplied: cleanup-only mode
			// must not call them, even for an accepted but unsent execution.
			require.NoError(t, NewController(store, cleanup, cleanup, nil, time.Millisecond).Start(ctx))
			require.True(t, store.persisted, "the worker must persist an outcome before it stops")
			require.Equal(t, want, store.progress.State)
			if name == "completed" {
				require.Zero(t, cleanup.deletes+cleanup.expires)
			} else {
				require.Equal(t, 1, cleanup.deletes+cleanup.expires)
				require.Contains(t, store.progress.FailureReason, "Scheduler identity changed or is unavailable")
			}
		})
	}
}
