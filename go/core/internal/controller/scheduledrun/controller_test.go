package scheduledrun

import (
	"context"
	"errors"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/a2aproject/a2a-go/v2/a2asrv"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type controllerTestStore struct {
	controllerStore
	session *apiv1alpha1.Session
}

func (s controllerTestStore) GetSession(context.Context, string, string) (*apiv1alpha1.Session, error) {
	if s.session == nil {
		return nil, database.ErrNotFound
	}
	return s.session, nil
}

type controllerTestCleanup struct {
	controllerWorkflow
	a2asrv.RequestHandler
	task             *a2atype.Task
	err              error
	deletes, expires int
}

func (c *controllerTestCleanup) Delete(_ context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	c.deletes++
	return session, c.err
}

func (c *controllerTestCleanup) CancelTask(_ context.Context, request *a2atype.CancelTaskRequest) (*a2atype.Task, error) {
	c.expires++
	if c.err != nil {
		return nil, c.err
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

func (c *controllerTestCleanup) Suspend(_ context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	c.expires++
	return session, c.err
}

func TestControllerRecoversCleanupWithoutReplacingSession(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state apiv1alpha1.RuntimeState
	}{
		{"creating timeout", apiv1alpha1.RuntimeState_RUNTIME_STATE_CREATING},
		{"running timeout", apiv1alpha1.RuntimeState_RUNTIME_STATE_READY},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deadline := time.Now().Add(-time.Minute)
			execution := &apiv1alpha1.ScheduledRunExecution{Id: "execution", SessionId: "original", TaskId: "original-task", State: apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_PENDING, Deadline: timestamppb.New(deadline)}
			store := controllerTestStore{session: &apiv1alpha1.Session{Id: "original", State: tc.state}}
			cleanup := &controllerTestCleanup{err: errors.New("Substrate unavailable")}
			controller := NewController(store, cleanup, cleanup)
			require.Error(t, controller.reconcile(t.Context(), database.LeasedScheduledRunExecution{Execution: execution}))
			require.Equal(t, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_PENDING, execution.State)
			cleanup.err = nil
			require.NoError(t, controller.reconcile(t.Context(), database.LeasedScheduledRunExecution{Execution: execution}))
			require.Equal(t, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT, execution.State)
			require.Equal(t, "original", execution.SessionId)
			require.Equal(t, 2, cleanup.deletes+cleanup.expires)
		})
	}
	// No reserve method is supplied: a missing historical session must never
	// call it, even when the execution was still PENDING when deletion happened.
	execution := &apiv1alpha1.ScheduledRunExecution{Id: "execution", SessionId: "deleted", Deadline: timestamppb.New(time.Now().Add(time.Minute))}
	require.NoError(t, NewController(controllerTestStore{}, nil, nil).reconcile(t.Context(), database.LeasedScheduledRunExecution{Execution: execution}))
	require.Equal(t, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_FAILED, execution.State)
	require.Equal(t, "deleted", execution.SessionId)
}

func TestControllerKeepsCompletedOutcomeAfterDeadline(t *testing.T) {
	completedAt := time.Now().Add(-2 * time.Minute)
	execution := &apiv1alpha1.ScheduledRunExecution{Id: "execution", SessionId: "original", TaskId: "original-task", Deadline: timestamppb.New(completedAt.Add(time.Minute))}
	store := controllerTestStore{session: &apiv1alpha1.Session{Id: "original"}}
	gateway := &controllerTestCleanup{task: &a2atype.Task{ID: "original-task", Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted, Timestamp: &completedAt}}}
	require.NoError(t, NewController(store, nil, gateway).reconcile(t.Context(), database.LeasedScheduledRunExecution{Execution: execution}))
	require.Equal(t, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED, execution.State)
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
			execution := &apiv1alpha1.ScheduledRunExecution{Id: "execution", TaskId: string(task.ID)}
			got, err := NewController(nil, nil, gateway).executionTask(t.Context(), execution)
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
			execution := &apiv1alpha1.ScheduledRunExecution{Id: "execution"}
			got, err := NewController(nil, nil, gateway).executionTask(t.Context(), execution)
			require.ErrorIs(t, err, tc.err)
			require.Equal(t, tc.task, got)
			require.Equal(t, 2, calls)
		})
	}
}

func TestControllerDoesNotRepeatAnUncertainDispatch(t *testing.T) {
	execution := &apiv1alpha1.ScheduledRunExecution{
		Id: "execution", SessionId: "session",
		State:    apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING,
		Deadline: timestamppb.New(time.Now().Add(time.Minute)),
	}
	store := controllerTestStore{session: &apiv1alpha1.Session{Id: "session", State: apiv1alpha1.RuntimeState_RUNTIME_STATE_READY}}
	reads := 0
	gateway := taskLookupGateway{listTasks: func(*a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
		reads++
		return &a2atype.ListTasksResponse{}, nil
	}}
	// Neither dispatch nor workflow methods are supplied: even an empty history
	// after a lost response must never trigger another send.
	controller := NewController(store, nil, gateway)
	require.NoError(t, controller.reconcile(t.Context(), database.LeasedScheduledRunExecution{Execution: execution}))
	require.Equal(t, 1, reads)
	require.Empty(t, execution.TaskId)
	require.Equal(t, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING, execution.State)
}
