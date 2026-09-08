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
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type workerTestStore struct {
	workerStore
	instance *apiv1alpha1.AgentInstance
}

func (s workerTestStore) GetAgentInstance(context.Context, string, string) (*apiv1alpha1.AgentInstance, error) {
	if s.instance == nil {
		return nil, database.ErrNotFound
	}
	return s.instance, nil
}

type workerTestCleanup struct {
	workerWorkflow
	a2asrv.RequestHandler
	task             *a2atype.Task
	err              error
	deletes, expires int
}

func (c *workerTestCleanup) Delete(_ context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	c.deletes++
	return instance, c.err
}

func (c *workerTestCleanup) CancelTask(_ context.Context, request *a2atype.CancelTaskRequest) (*a2atype.Task, error) {
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

func (c *workerTestCleanup) ListTasks(context.Context, *a2atype.ListTasksRequest) (*a2atype.ListTasksResponse, error) {
	if c.task != nil {
		return &a2atype.ListTasksResponse{Tasks: []*a2atype.Task{c.task}}, nil
	}
	return &a2atype.ListTasksResponse{}, nil
}

func (c *workerTestCleanup) Suspend(_ context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	c.expires++
	return instance, c.err
}

type workerTestDenial struct{}

func (workerTestDenial) Check(context.Context, auth.Principal, auth.Verb, auth.Resource) error {
	return errors.New("controller forbidden")
}

func TestWorkerRecoversCleanupWithoutReplacingInstance(t *testing.T) {
	for _, tc := range []struct {
		name    string
		state   apiv1alpha1.AgentInstanceState
		expired bool
		want    apiv1alpha1.ScheduledRunExecutionState
	}{
		{"creating timeout", apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_CREATING, true, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT},
		{"running timeout", apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY, true, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT},
		{"revoked controller", apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY, false, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_FAILED},
	} {
		t.Run(tc.name, func(t *testing.T) {
			deadline := time.Now().Add(time.Minute)
			if tc.expired {
				deadline = time.Now().Add(-time.Minute)
			}
			execution := &apiv1alpha1.ScheduledRunExecution{Id: "execution", AgentInstanceId: "original", TaskId: "original-task", State: apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_PENDING, Deadline: timestamppb.New(deadline)}
			store := workerTestStore{instance: &apiv1alpha1.AgentInstance{Id: "original", State: tc.state}}
			cleanup := &workerTestCleanup{err: errors.New("Substrate unavailable")}
			worker := NewWorker(store, cleanup, cleanup, workerTestDenial{})
			require.Error(t, worker.reconcile(t.Context(), execution))
			require.Equal(t, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_PENDING, execution.State)
			cleanup.err = nil
			err := worker.reconcile(t.Context(), execution)
			if tc.expired {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, tc.want, execution.State)
			require.Equal(t, "original", execution.AgentInstanceId)
			require.Equal(t, 2, cleanup.deletes+cleanup.expires)
		})
	}
	// No reserve method is supplied: a missing historical instance must never
	// call it, even when the execution was still PENDING when deletion happened.
	execution := &apiv1alpha1.ScheduledRunExecution{Id: "execution", AgentInstanceId: "deleted", Deadline: timestamppb.New(time.Now().Add(time.Minute))}
	require.NoError(t, NewWorker(workerTestStore{}, nil, nil, nil).reconcile(t.Context(), execution))
	require.Equal(t, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_FAILED, execution.State)
	require.Equal(t, "deleted", execution.AgentInstanceId)
}

func TestWorkerKeepsCompletedOutcomeAfterDeadline(t *testing.T) {
	completedAt := time.Now().Add(-2 * time.Minute)
	execution := &apiv1alpha1.ScheduledRunExecution{Id: "execution", AgentInstanceId: "original", TaskId: "original-task", Deadline: timestamppb.New(completedAt.Add(time.Minute))}
	store := workerTestStore{instance: &apiv1alpha1.AgentInstance{Id: "original"}}
	gateway := &workerTestCleanup{task: &a2atype.Task{ID: "original-task", Status: a2atype.TaskStatus{State: a2atype.TaskStateCompleted, Timestamp: &completedAt}}}
	require.NoError(t, NewWorker(store, nil, gateway, nil).reconcile(t.Context(), execution))
	require.Equal(t, apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED, execution.State)
}
