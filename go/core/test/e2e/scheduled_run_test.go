package e2e_test

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/a2aproject/a2a-go/v2/a2apb/v1/pbconv"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

func TestScheduledRunCronAndManualExecution(t *testing.T) {
	target := interactionTarget(t)
	f := newScheduledFixture(t, target, startInteractionMock(t), false, 2*time.Minute)
	var first *apiv1alpha1.ScheduledRunExecution
	require.NoError(t, wait.PollUntilContextTimeout(f.ctx, time.Second, 90*time.Second, true, func(ctx context.Context) (bool, error) {
		result, err := f.schedules.ListScheduledRunExecutions(ctx, &apiv1alpha1.ListScheduledRunExecutionsRequest{ScheduledRunId: f.schedule.GetId()})
		if err != nil {
			return false, err
		}
		if len(result.GetExecutions()) == 0 {
			return false, nil
		}
		first = result.GetExecutions()[0]
		return true, nil
	}), "wait for cron firing")
	require.NotNil(t, first.GetScheduledTime())
	require.True(t, first.GetScheduledTime().AsTime().Equal(f.schedule.GetNextExecutionTime().AsTime()))
	paused := proto.Clone(f.schedule.GetConfig()).(*apiv1alpha1.ScheduledRunConfig)
	paused.Paused = true
	updated, err := f.schedules.UpdateScheduledRun(f.ctx, &apiv1alpha1.UpdateScheduledRunRequest{ScheduledRunId: f.schedule.GetId(), Etag: f.schedule.GetEtag(), Config: paused})
	require.NoError(t, err)
	require.Nil(t, updated.GetScheduledRun().GetNextExecutionTime())
	first = f.waitExecution(t, first.GetId(), apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED)
	f.assertCompletedTask(t, first)

	// A manual trigger works while paused; retrying its request ID is one firing.
	request := &apiv1alpha1.TriggerScheduledRunRequest{ScheduledRunId: f.schedule.GetId(), RequestId: uuid.NewString()}
	manual, err := f.schedules.TriggerScheduledRun(f.ctx, request)
	require.NoError(t, err)
	retried, err := f.schedules.TriggerScheduledRun(f.ctx, request)
	require.NoError(t, err)
	require.Equal(t, manual.GetExecution().GetId(), retried.GetExecution().GetId())
	second := f.waitExecution(t, manual.GetExecution().GetId(), apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED)
	require.NotEqual(t, first.GetAgentInstanceId(), second.GetAgentInstanceId())
	f.assertCompletedTask(t, second)

	// Continuing the conversation does not replace the original execution's task.
	conversation := &interactionFixture{ctx: f.instanceContext(second), client: f.tasks, instances: f.instances, instanceID: second.GetAgentInstanceId()}
	_, _, continued := conversation.send(t, "What is 2+2?")
	require.NotEqual(t, second.GetTaskId(), string(continued.ID))
	retained, err := f.schedules.GetScheduledRunExecution(f.ctx, &apiv1alpha1.GetScheduledRunExecutionRequest{ExecutionId: second.GetId()})
	require.NoError(t, err)
	require.Equal(t, second.GetTaskId(), retained.GetExecution().GetTaskId())

	_, err = f.schedules.DeleteScheduledRun(f.ctx, &apiv1alpha1.DeleteScheduledRunRequest{ScheduledRunId: f.schedule.GetId()})
	require.NoError(t, err)
	_, err = f.instances.DeleteAgentInstance(f.ctx, &apiv1alpha1.DeleteAgentInstanceRequest{AgentInstanceId: first.GetAgentInstanceId()})
	require.NoError(t, err)
	retained, err = f.schedules.GetScheduledRunExecution(f.ctx, &apiv1alpha1.GetScheduledRunExecutionRequest{ExecutionId: first.GetId()})
	require.NoError(t, err)
	require.Equal(t, first.GetAgentInstanceId(), retained.GetExecution().GetAgentInstanceId())
	require.Equal(t, first.GetState(), retained.GetExecution().GetState())
}

func TestScheduledRunTimeout(t *testing.T) {
	target := interactionTarget(t)
	modelURL, started := startBlockingInteractionMock(t)
	f := newScheduledFixture(t, target, modelURL, true, time.Minute)
	execution := f.trigger(t)
	select {
	case <-started:
	case <-time.After(45 * time.Second):
		t.Fatal("scheduled agent did not reach the blocking model")
	}
	execution = f.waitExecution(t, execution.GetId(), apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT)
	require.NotEmpty(t, execution.GetFailureReason())
	require.False(t, execution.GetCompletedAt().AsTime().Before(execution.GetDeadline().AsTime()))
	require.Equal(t, a2atype.TaskStateCanceled, f.task(t, execution).Status.State)
	f.assertQuiescent(t, execution)
}

// Keep this test sequential: restarting the controller disrupts other clients.
func TestScheduledRunControllerRestart(t *testing.T) {
	target := interactionTarget(t)
	modelURL, started, release, calls := startScheduledRecoveryModel(t)
	f := newScheduledFixture(t, target, modelURL, true, 3*time.Minute)
	execution := f.trigger(t)
	select {
	case <-started:
	case <-time.After(time.Minute):
		t.Fatal("scheduled agent did not reach the model before restart")
	}
	execution = f.waitExecution(t, execution.GetId(), apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING)
	require.NotEmpty(t, execution.GetTaskId())
	kube := interactionKubeClient(t)
	pods := &corev1.PodList{}
	require.NoError(t, kube.List(f.ctx, pods, ctrlclient.InNamespace("kagent"), ctrlclient.MatchingLabels{"app.kubernetes.io/component": "controller"}))
	require.Len(t, pods.Items, 1, "restart test requires a single controller replica")
	old := pods.Items[0]
	require.NoError(t, kube.Delete(f.ctx, &old))
	require.NoError(t, wait.PollUntilContextTimeout(f.ctx, time.Second, 90*time.Second, true, func(ctx context.Context) (bool, error) {
		current := &corev1.PodList{}
		if err := kube.List(ctx, current, ctrlclient.InNamespace("kagent"), ctrlclient.MatchingLabels{"app.kubernetes.io/component": "controller"}); err != nil {
			return false, err
		}
		for _, pod := range current.Items {
			if pod.UID == old.UID {
				continue
			}
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					return true, nil
				}
			}
		}
		return false, nil
	}), "wait for replacement controller")
	release()
	recovered := f.waitExecution(t, execution.GetId(), apiv1alpha1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED)
	require.Equal(t, execution.GetAgentInstanceId(), recovered.GetAgentInstanceId())
	require.Equal(t, execution.GetTaskId(), recovered.GetTaskId())
	require.EqualValues(t, 1, calls.Load(), "restart must not resend the original prompt")
	f.assertCompletedTask(t, recovered)
}

type scheduledFixture struct {
	ctx       context.Context
	schedules apiv1alpha1.ScheduledRunServiceClient
	instances apiv1alpha1.AgentInstanceServiceClient
	system    apiv1alpha1.SystemServiceClient
	tasks     a2apb.A2AServiceClient
	schedule  *apiv1alpha1.ScheduledRun
}

func newScheduledFixture(t *testing.T, target, modelURL string, paused bool, timeout time.Duration) *scheduledFixture {
	t.Helper()
	template := createInteractionTemplate(t, modelURL)
	conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "e2e"), 5*time.Minute)
	t.Cleanup(cancel)
	f := &scheduledFixture{ctx: ctx, schedules: apiv1alpha1.NewScheduledRunServiceClient(conn), instances: apiv1alpha1.NewAgentInstanceServiceClient(conn), system: apiv1alpha1.NewSystemServiceClient(conn), tasks: a2apb.NewA2AServiceClient(conn)}
	created, err := f.schedules.CreateScheduledRun(ctx, &apiv1alpha1.CreateScheduledRunRequest{
		RequestId: uuid.NewString(), Harness: &apiv1alpha1.ResourceReference{Namespace: "kagent", Name: "kagent"},
		AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: "kagent", Name: template},
		Config:        &apiv1alpha1.ScheduledRunConfig{Name: t.Name(), Schedule: "* * * * *", TimeZone: "UTC", Prompt: "What is 2+2?", Paused: paused, ExecutionTimeout: durationpb.New(timeout)},
	})
	require.NoError(t, err)
	f.schedule = created.GetScheduledRun()
	require.NotNil(t, f.schedule)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), 2*time.Minute)
		defer cleanupCancel()
		_, err := f.schedules.DeleteScheduledRun(cleanupCtx, &apiv1alpha1.DeleteScheduledRunRequest{ScheduledRunId: f.schedule.GetId()})
		if err != nil {
			t.Errorf("delete schedule: %v", err)
		}
		executions, err := f.schedules.ListScheduledRunExecutions(cleanupCtx, &apiv1alpha1.ListScheduledRunExecutionsRequest{ScheduledRunId: f.schedule.GetId(), Page: &apiv1alpha1.PageRequest{Limit: 100}})
		if err != nil {
			t.Errorf("list executions for cleanup: %v", err)
			return
		}
		for _, execution := range executions.GetExecutions() {
			if execution.GetAgentInstanceId() == "" {
				continue
			}
			_, err := f.instances.DeleteAgentInstance(cleanupCtx, &apiv1alpha1.DeleteAgentInstanceRequest{AgentInstanceId: execution.GetAgentInstanceId()})
			if err != nil && status.Code(err) != codes.NotFound {
				t.Errorf("delete scheduled instance: %v", err)
			}
		}
	})
	return f
}

func (f *scheduledFixture) trigger(t *testing.T) *apiv1alpha1.ScheduledRunExecution {
	t.Helper()
	result, err := f.schedules.TriggerScheduledRun(f.ctx, &apiv1alpha1.TriggerScheduledRunRequest{ScheduledRunId: f.schedule.GetId(), RequestId: uuid.NewString()})
	require.NoError(t, err)
	return result.GetExecution()
}

func (f *scheduledFixture) waitExecution(t *testing.T, id string, want apiv1alpha1.ScheduledRunExecutionState) *apiv1alpha1.ScheduledRunExecution {
	t.Helper()
	var execution *apiv1alpha1.ScheduledRunExecution
	err := wait.PollUntilContextTimeout(f.ctx, time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		result, err := f.schedules.GetScheduledRunExecution(callCtx, &apiv1alpha1.GetScheduledRunExecutionRequest{ExecutionId: id})
		if status.Code(err) == codes.Unavailable || status.Code(err) == codes.DeadlineExceeded {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		execution = result.GetExecution()
		if execution.GetState() == want {
			return true, nil
		}
		if execution.GetCompletedAt() != nil {
			return false, fmt.Errorf("execution ended in %s: %s", execution.GetState(), execution.GetFailureReason())
		}
		return false, nil
	})
	require.NoError(t, err, "last execution: %v", execution)
	require.NotEmpty(t, execution.GetAgentInstanceId())
	require.Equal(t, "e2e", execution.GetCreator())
	return execution
}

func (f *scheduledFixture) instanceContext(execution *apiv1alpha1.ScheduledRunExecution) context.Context {
	return metadata.AppendToOutgoingContext(f.ctx, "x-kagent-agent-instance-id", execution.GetAgentInstanceId())
}

func (f *scheduledFixture) task(t *testing.T, execution *apiv1alpha1.ScheduledRunExecution) *a2atype.Task {
	t.Helper()
	request, err := pbconv.ToProtoGetTaskRequest(&a2atype.GetTaskRequest{ID: a2atype.TaskID(execution.GetTaskId())})
	require.NoError(t, err)
	result, err := f.tasks.GetTask(f.instanceContext(execution), request)
	require.NoError(t, err)
	task, err := pbconv.FromProtoTask(result)
	require.NoError(t, err)
	require.Equal(t, execution.GetAgentInstanceId(), task.ContextID)
	return task
}

func (f *scheduledFixture) assertCompletedTask(t *testing.T, execution *apiv1alpha1.ScheduledRunExecution) {
	t.Helper()
	task := f.task(t, execution)
	require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
	require.Contains(t, taskText(task), "The answer is 4.")
	f.assertQuiescent(t, execution)
}

func (f *scheduledFixture) assertQuiescent(t *testing.T, execution *apiv1alpha1.ScheduledRunExecution) {
	t.Helper()
	result, err := f.instances.GetAgentInstance(f.ctx, &apiv1alpha1.GetAgentInstanceRequest{AgentInstanceId: execution.GetAgentInstanceId()})
	require.NoError(t, err)
	// The public A2A authority identifies the Actor without reading internal DB state.
	actorName, _, _ := strings.Cut(result.GetAgentInstance().GetA2AAuthority(), ".")
	require.NotEmpty(t, actorName)
	require.NoError(t, wait.PollUntilContextTimeout(f.ctx, time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		status, err := f.system.GetSubstrateStatus(ctx, &apiv1alpha1.GetSubstrateStatusRequest{Namespace: "kagent"})
		if err != nil {
			return false, err
		}
		if status.GetAteApiError() != "" {
			return false, fmt.Errorf("Substrate status: %s", status.GetAteApiError())
		}
		for _, actor := range status.GetActors() {
			if actor.GetActorId() == actorName {
				return actor.GetStatus() == "Suspended" || actor.GetStatus() == "Paused", nil
			}
		}
		return false, nil
	}), "scheduled Actor %s should release its worker", actorName)
}

func startScheduledRecoveryModel(t *testing.T) (string, <-chan struct{}, func(), *atomic.Int32) {
	t.Helper()
	upstream, err := url.Parse(startInteractionMock(t))
	require.NoError(t, err)
	upstream.Path = ""
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	started, release := make(chan struct{}), make(chan struct{})
	var startedOnce, releaseOnce sync.Once
	calls := &atomic.Int32{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		startedOnce.Do(func() { close(started) })
		select {
		case <-r.Context().Done():
			return
		case <-release:
			proxy.ServeHTTP(w, r)
		}
	}))
	require.NoError(t, server.Listener.Close())
	server.Listener, err = net.Listen("tcp", "0.0.0.0:0")
	require.NoError(t, err)
	server.Start()
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() { unblock(); server.Close() })
	return reachableModelURL(t, server.URL), started, unblock, calls
}
