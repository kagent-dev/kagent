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

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
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
}

func (c *controllerTestCleanup) Delete(_ context.Context, session *apiv1.Session) (*apiv1.Session, error) {
	c.deletes++
	return session, c.err
}

func (c *controllerTestCleanup) CancelTask(_ context.Context, request *a2atype.CancelTaskRequest) (*a2atype.Task, error) {
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

func (c *controllerTestCleanup) Suspend(_ context.Context, session *apiv1.Session) (*apiv1.Session, error) {
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
			execution := &apiv1.ScheduledRunExecution{Id: "execution", SessionId: "original", TaskId: "original-task", State: apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_PENDING, Deadline: timestamppb.New(deadline)}
			store := controllerTestStore{session: &apiv1.Session{Id: "original", State: tc.state}}
			cleanup := &controllerTestCleanup{err: errors.New("Substrate unavailable")}
			controller := NewController(store, cleanup, cleanup, time.Second)
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
	execution := &apiv1.ScheduledRunExecution{Id: "execution", SessionId: "deleted", Deadline: timestamppb.New(time.Now().Add(time.Minute))}
	require.NoError(t, NewController(controllerTestStore{}, nil, nil, time.Second).reconcile(t.Context(), database.LeasedScheduledRunExecution{Execution: execution}))
	require.Equal(t, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_FAILED, execution.State)
	require.Equal(t, "deleted", execution.SessionId)
}

func TestControllerKeepsCompletedOutcomeAfterDeadline(t *testing.T) {
	completedAt := time.Now().Add(-2 * time.Minute)
	execution := &apiv1.ScheduledRunExecution{Id: "execution", SessionId: "original", TaskId: "original-task", Deadline: timestamppb.New(completedAt.Add(time.Minute))}
	store := controllerTestStore{session: &apiv1.Session{Id: "original"}}
	gateway := &controllerTestCleanup{task: &a2atype.Task{ID: "original-task", Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted, Timestamp: &completedAt}}}
	require.NoError(t, NewController(store, nil, gateway, time.Second).reconcile(t.Context(), database.LeasedScheduledRunExecution{Execution: execution}))
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
			execution := &apiv1.ScheduledRunExecution{Id: "execution", SessionId: "original", TaskId: "original-task", Deadline: timestamppb.New(deadline)}
			store := controllerTestStore{session: &apiv1.Session{Id: "original"}}
			gateway := &controllerTestCleanup{
				task:         &a2atype.Task{ID: "original-task", Status: a2atype.TaskStatus{State: a2atype.TaskStateWorking}},
				canceledTask: &a2atype.Task{ID: "original-task", Status: a2atype.TaskStatus{State: tc.state, Timestamp: tc.timestamp}},
			}
			require.NoError(t, NewController(store, nil, gateway, time.Second).reconcile(t.Context(), database.LeasedScheduledRunExecution{Execution: execution}))
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
			execution := &apiv1.ScheduledRunExecution{Id: "execution", TaskId: string(task.ID)}
			got, err := NewController(nil, nil, gateway, time.Second).executionTask(t.Context(), execution)
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
			execution := &apiv1.ScheduledRunExecution{Id: "execution"}
			got, err := NewController(nil, nil, gateway, time.Second).executionTask(t.Context(), execution)
			require.ErrorIs(t, err, tc.err)
			require.Equal(t, tc.task, got)
			require.Equal(t, 2, calls)
		})
	}
}

func TestControllerDoesNotRepeatAnUncertainDispatch(t *testing.T) {
	execution := &apiv1.ScheduledRunExecution{
		Id: "execution", SessionId: "session",
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
	controller := NewController(store, nil, gateway, time.Second)
	require.NoError(t, controller.reconcile(t.Context(), database.LeasedScheduledRunExecution{Execution: execution}))
	require.Equal(t, 1, reads)
	require.Empty(t, execution.TaskId)
	require.Equal(t, apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING, execution.State)
}

// This policy grants only the Session operations exercised by durable dispatch.
// It recognizes the in-process session type, never a human claim or stored owner.
type controllerSessionPolicy struct{ calls []auth.Verb }

func (p *controllerSessionPolicy) Check(ctx context.Context, principal auth.Principal, verb auth.Verb, resource auth.Resource) error {
	identity, _ := auth.AuthSessionFrom(ctx)
	if _, ok := identity.(auth.ControlPlaneSession); !ok {
		return errors.New("internal identity required")
	}
	if principal.ID != "" || len(principal.Claims) != 0 || resource.Type != "Session" || (verb != auth.VerbGet && verb != auth.VerbCreate && verb != auth.VerbUpdate) {
		return errors.New("operation denied")
	}
	p.calls = append(p.calls, verb)
	return nil
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
	return []database.LeasedScheduledRunExecution{{Execution: &apiv1.ScheduledRunExecution{Id: "execution", Creator: "owner", SessionId: s.session.Id, Deadline: timestamppb.New(time.Now().Add(time.Minute))}}}, nil
}
func (s *scopedControllerStore) GetSession(_ context.Context, id, owner string) (*apiv1.Session, error) {
	if id != s.session.Id || owner != "owner" {
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
	return g.interactions.ListTasks(ctx, g.agent, request)
}
func (g scopedControllerGateway) SendStreamingMessage(ctx context.Context, request *a2atype.SendMessageRequest) iter.Seq2[a2atype.Event, error] {
	return func(yield func(a2atype.Event, error) bool) {
		_, err := g.interactions.PrepareSend(ctx, g.agent, request)
		g.cancel()
		yield(&a2atype.Task{ID: "task", Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted}}, err)
	}
}
func TestControllerDispatchUsesBoundedInProcessAuthority(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	store := &scopedControllerStore{session: &apiv1.Session{Id: id, ContextId: id, Creator: "owner", Agent: &apiv1.ResourceReference{Namespace: "team", Name: "assistant"}, State: apiv1.RuntimeState_RUNTIME_STATE_READY, Operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE}}
	policy := &controllerSessionPolicy{}
	sessions := sessionsvc.NewService(store, policy, nil)
	interactions := sessionsvc.NewInteractionService(store, nil, sessions, false)
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	gateway := scopedControllerGateway{interactions: interactions, cancel: cancel, agent: types.NamespacedName{Namespace: "team", Name: "assistant"}}
	controller := NewController(store, nil, gateway, time.Millisecond)
	require.NoError(t, controller.Start(ctx))
	require.True(t, store.reserved)
	require.Equal(t, []auth.Verb{auth.VerbGet, auth.VerbCreate}, policy.calls)
	// Human principals cannot acquire internal authority by copying the creator ID.
	forged := auth.AuthSessionTo(t.Context(), controllerHumanSession{})
	_, err := sessions.Get(forged, id)
	require.Error(t, err)
}

type controllerHumanSession struct{}

func (controllerHumanSession) Principal() auth.Principal {
	return auth.Principal{ID: "owner", Claims: map[string]any{"control_plane": true}}
}
