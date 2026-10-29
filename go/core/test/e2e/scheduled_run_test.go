package e2e_test

import (
	"bytes"
	"context"
	"fmt"
	"io"
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
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestScheduledRunCronAndManualExecution(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		// Independent schedules can wait for the same cron tick.
		t.Parallel()
		target := interactionTarget(t)
		f := newScheduledFixture(t, harness, target, startInteractionMock(t), false, 2*time.Minute)
		var first *apiv1.ScheduledRunExecution
		require.NoError(t, wait.PollUntilContextTimeout(f.ctx, time.Second, 90*time.Second, true, func(ctx context.Context) (bool, error) {
			result, err := f.schedules.ListScheduledRunExecutions(ctx, &apiv1.ListScheduledRunExecutionsRequest{ScheduledRunId: string(f.schedule.UID)})
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
		require.NoError(t, f.kube.Get(f.ctx, ctrlclient.ObjectKeyFromObject(f.schedule), f.schedule))
		f.schedule.Spec.Paused = new(true)
		require.NoError(t, f.kube.Update(f.ctx, f.schedule))
		f.waitReconciled(t)
		first = f.waitExecution(t, first.GetId(), apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED)
		f.assertCompletedTask(t, first)
		f.assertHumanCheckpointFork(t, first)

		// A failed replacement must keep the last successful runtime available.
		require.NoError(t, f.kube.Get(f.ctx, ctrlclient.ObjectKeyFromObject(f.agent), f.agent))
		f.agent.Spec.HarnessRef.Name = "missing-harness"
		require.NoError(t, f.kube.Update(f.ctx, f.agent))
		require.NoError(t, wait.PollUntilContextTimeout(f.ctx, time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
			if err := f.kube.Get(ctx, ctrlclient.ObjectKeyFromObject(f.schedule), f.schedule); err != nil {
				return false, err
			}
			if err := f.kube.Get(ctx, ctrlclient.ObjectKeyFromObject(f.agent), f.agent); err != nil {
				return false, err
			}
			return f.agent.Status.ObservedGeneration == f.agent.Generation &&
				apimeta.IsStatusConditionFalse(f.agent.Status.Conditions, v1alpha3.AgentConditionResolvedRefs) &&
				apimeta.IsStatusConditionTrue(f.schedule.Status.Conditions, v1alpha3.ScheduledRunConditionReady), nil
		}))

		// A manual trigger works while paused; retrying its request ID is one firing.
		request := &apiv1.TriggerScheduledRunRequest{ScheduledRunId: string(f.schedule.UID), RequestId: uuid.NewString()}
		manual, err := f.schedules.TriggerScheduledRun(f.ctx, request)
		require.NoError(t, err)
		retried, err := f.schedules.TriggerScheduledRun(f.ctx, request)
		require.NoError(t, err)
		require.Equal(t, manual.GetExecution().GetId(), retried.GetExecution().GetId())
		second := f.waitExecution(t, manual.GetExecution().GetId(), apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED)
		require.NotEqual(t, first.GetSessionId(), second.GetSessionId())
		require.Equal(t, first.RuntimeRevision, second.RuntimeRevision)
		f.assertCompletedTask(t, second)

		// Continuing the conversation does not replace the original execution's task.
		conversation := &interactionFixture{ctx: f.ctx, client: f.tasks, sessions: f.sessions, sessionID: second.GetSessionId(), contextID: second.GetSessionId(), tenant: f.agent.Namespace + "/" + f.agent.Name}
		_, _, continued := conversation.send(t, "What is 2+2?")
		require.NotEqual(t, second.GetTaskId(), string(continued.ID))
		retained, err := f.schedules.GetScheduledRunExecution(f.ctx, &apiv1.GetScheduledRunExecutionRequest{ExecutionId: second.GetId()})
		require.NoError(t, err)
		require.Equal(t, second.GetTaskId(), retained.GetExecution().GetTaskId())

		err = f.kube.Delete(f.ctx, f.schedule)
		require.NoError(t, err)
		require.NoError(t, deleteIdleSession(f.ctx, f.sessions, first.GetSessionId()))
		retained, err = f.schedules.GetScheduledRunExecution(f.ctx, &apiv1.GetScheduledRunExecutionRequest{ExecutionId: first.GetId()})
		require.NoError(t, err)
		require.Equal(t, first.GetSessionId(), retained.GetExecution().GetSessionId())
		require.Equal(t, first.GetState(), retained.GetExecution().GetState())
	})
}

func TestScheduledRunTimeout(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		target := interactionTarget(t)
		modelURL, started := startBlockingInteractionMock(t)
		// Template preparation finishes before triggering; leave time for the new
		// session to reach the blocking model while exercising a real deadline.
		f := newScheduledFixture(t, harness, target, modelURL, true, 20*time.Second)
		execution := f.trigger(t)
		select {
		case <-started:
		case <-time.After(45 * time.Second):
			t.Fatal("scheduled agent did not reach the blocking model")
		}
		execution = f.waitExecution(t, execution.GetId(), apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_TIMED_OUT)
		require.NotEmpty(t, execution.GetFailureReason())
		require.False(t, execution.GetCompletedAt().AsTime().Before(execution.GetDeadline().AsTime()))
		require.Equal(t, a2atype.TaskStateCanceled, f.task(t, execution).Status.State)
		f.assertQuiescent(t, execution)
	})
}

// Keep this test sequential: restarting the controller disrupts other clients.
func TestScheduledRunControllerRestart(t *testing.T) {
	var releases []func()
	restarted := false
	defer func() {
		for _, release := range releases {
			release()
		}
	}()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		target := interactionTarget(t)
		modelURL, started, release, calls := startScheduledRecoveryModel(t)
		f := newScheduledFixture(t, harness, target, modelURL, true, 3*time.Minute)
		execution := f.trigger(t)
		select {
		case <-started:
		case <-time.After(time.Minute):
			t.Fatal("scheduled agent did not reach the model before restart")
		}
		execution = f.waitExecution(t, execution.GetId(), apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_RUNNING)
		require.NotEmpty(t, execution.GetTaskId())
		releases = append(releases, release)
		// Parallel subtests resume only after the parent returns. Keep setup
		// before this barrier so every execution is running at the shared restart.
		t.Parallel()
		require.True(t, restarted, "shared controller restart did not complete")
		recovered := f.waitExecution(t, execution.GetId(), apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED)
		require.Equal(t, execution.GetSessionId(), recovered.GetSessionId())
		require.Equal(t, execution.GetTaskId(), recovered.GetTaskId())
		require.EqualValues(t, 1, calls.Load(), "restart must not resend the original prompt")
		f.assertCompletedTask(t, recovered)
	})
	// A subtest filter may select no harnesses. Failed setup must also leave the
	// controller alone; the deferred releases still unblock prepared executions.
	if len(releases) == 0 || t.Failed() {
		return
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	kube := interactionKubeClient(t)
	pods := &corev1.PodList{}
	require.NoError(t, kube.List(ctx, pods, ctrlclient.InNamespace("kagent"), ctrlclient.MatchingLabels{"app.kubernetes.io/component": "controller"}))
	require.Len(t, pods.Items, 1, "restart test requires a single controller replica")
	old := pods.Items[0]
	require.NoError(t, kube.Delete(ctx, &old))
	require.NoError(t, wait.PollUntilContextTimeout(ctx, time.Second, 90*time.Second, true, func(ctx context.Context) (bool, error) {
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
	newControllerConn(t, interactionTarget(t))
	restarted = true
}

type scheduledFixture struct {
	creator     string
	ctx         context.Context
	schedules   apiv1.ScheduledRunServiceClient
	sessions    apiv1.SessionServiceClient
	checkpoints apiv1.CheckpointServiceClient
	system      apiv1.SystemServiceClient
	tasks       a2apb.A2AServiceClient
	schedule    *v1alpha3.ScheduledRun
	kube        ctrlclient.Client
	agent       *v1alpha3.Agent
}

func newScheduledFixture(t *testing.T, harness testHarness, target, modelURL string, paused bool, timeout time.Duration) *scheduledFixture {
	t.Helper()
	conn := newControllerConn(t, target)
	ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "e2e"), 5*time.Minute)
	t.Cleanup(cancel)
	f := &scheduledFixture{ctx: ctx, schedules: apiv1.NewScheduledRunServiceClient(conn), sessions: apiv1.NewSessionServiceClient(conn), checkpoints: apiv1.NewCheckpointServiceClient(conn), system: apiv1.NewSystemServiceClient(conn), tasks: a2apb.NewA2AServiceClient(conn), kube: interactionKubeClient(t)}
	// The scheduler's identity is scoped to the controller's installation namespace.
	f.creator = auth.NewSchedulerSession("kagent", nil).Principal().ID
	model := harness.createModel(t, f.kube, modelURL, nil)
	f.agent = &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: "kagent", Name: "scheduled-agent-" + uuid.NewString()}, Spec: v1alpha3.AgentSpec{HarnessRef: corev1.LocalObjectReference{Name: harness.name}, Template: v1alpha3.AgentTemplateSource{Inline: &v1alpha3.AgentTemplateSpec{ModelConfig: &v1alpha3.ObjectReference{Name: model.Name}, Description: "Scheduled agent E2E fixture", SystemPrompt: "Reply briefly."}}}}
	require.NoError(t, f.kube.Create(ctx, f.agent))
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := f.kube.Delete(cleanupCtx, f.agent); !apierrors.IsNotFound(err) {
			require.NoError(t, err)
		}
	})
	f.schedule = &v1alpha3.ScheduledRun{ObjectMeta: metav1.ObjectMeta{Namespace: "kagent", Name: "schedule-" + uuid.NewString()}, Spec: v1alpha3.ScheduledRunSpec{AgentRef: corev1.LocalObjectReference{Name: f.agent.Name}, Schedule: "* * * * *", TimeZone: new("UTC"), Prompt: "What is 2+2?", Paused: new(paused), ExecutionTimeout: &metav1.Duration{Duration: timeout}}}
	require.NoError(t, f.kube.Create(ctx, f.schedule))
	f.waitReconciled(t)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), 2*time.Minute)
		defer cleanupCancel()
		err := f.kube.Delete(cleanupCtx, f.schedule)
		if err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete schedule: %v", err)
		}
		executions, err := f.schedules.ListScheduledRunExecutions(cleanupCtx, &apiv1.ListScheduledRunExecutionsRequest{ScheduledRunId: string(f.schedule.UID), Page: &apiv1.PageRequest{Limit: 100}})
		if err != nil {
			t.Errorf("list executions for cleanup: %v", err)
			return
		}
		for _, execution := range executions.GetExecutions() {
			if execution.GetSessionId() == "" {
				continue
			}
			if err := deleteIdleSession(cleanupCtx, f.sessions, execution.GetSessionId()); err != nil {
				t.Errorf("delete scheduled session: %v", err)
			}
		}
	})
	return f
}

func (f *scheduledFixture) trigger(t *testing.T) *apiv1.ScheduledRunExecution {
	t.Helper()
	result, err := f.schedules.TriggerScheduledRun(f.ctx, &apiv1.TriggerScheduledRunRequest{ScheduledRunId: string(f.schedule.UID), RequestId: uuid.NewString()})
	require.NoError(t, err)
	return result.GetExecution()
}

func (f *scheduledFixture) waitExecution(t *testing.T, id string, want apiv1.ScheduledRunExecutionState) *apiv1.ScheduledRunExecution {
	t.Helper()
	var execution *apiv1.ScheduledRunExecution
	err := wait.PollUntilContextTimeout(f.ctx, time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		callCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		result, err := f.schedules.GetScheduledRunExecution(callCtx, &apiv1.GetScheduledRunExecutionRequest{ExecutionId: id})
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
	require.NotEmpty(t, execution.GetSessionId())
	require.Equal(t, f.creator, execution.GetCreator())
	return execution
}

func (f *scheduledFixture) task(t *testing.T, execution *apiv1.ScheduledRunExecution) *a2atype.Task {
	t.Helper()
	request, err := pbconv.ToProtoGetTaskRequest(&a2atype.GetTaskRequest{Tenant: f.agent.Namespace + "/" + f.agent.Name, ID: a2atype.TaskID(execution.GetTaskId())})
	require.NoError(t, err)
	result, err := f.tasks.GetTask(f.ctx, request)
	require.NoError(t, err)
	task, err := pbconv.FromProtoTask(result)
	require.NoError(t, err)
	session, err := f.sessions.GetSession(f.ctx, &apiv1.GetSessionRequest{SessionId: execution.GetSessionId()})
	require.NoError(t, err)
	require.NotEmpty(t, session.GetSession().GetContextId())
	require.Equal(t, session.GetSession().GetContextId(), task.ContextID)
	require.Equal(t, string(f.agent.UID), session.Session.GetAgent().GetUid())
	require.Equal(t, f.agent.Name, session.Session.GetAgent().GetRef().GetName())
	require.Equal(t, execution.RuntimeRevision, session.Session.PreparedRevision)
	require.True(t, proto.Equal(execution.ScheduledRun, session.GetScheduledRun().Ref))
	require.Equal(t, string(f.schedule.UID), session.GetScheduledRun().Uid)
	require.Equal(t, execution.Id, session.GetScheduledRun().ExecutionId)
	return task
}

func (f *scheduledFixture) assertCompletedTask(t *testing.T, execution *apiv1.ScheduledRunExecution) {
	t.Helper()
	task := f.task(t, execution)
	require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
	require.Contains(t, taskText(task), "The answer is 4.")
	f.assertQuiescent(t, execution)
}

func (f *scheduledFixture) assertQuiescent(t *testing.T, execution *apiv1.ScheduledRunExecution) {
	t.Helper()
	result, err := f.sessions.GetSession(f.ctx, &apiv1.GetSessionRequest{SessionId: execution.GetSessionId()})
	require.NoError(t, err)
	// The public A2A authority identifies the Actor without reading internal DB state.
	actorName, rest, _ := strings.Cut(result.GetSession().GetA2AAuthority(), ".")
	atespace, _, _ := strings.Cut(rest, ".")
	require.NotEmpty(t, atespace)
	require.NotEmpty(t, actorName)
	require.NoError(t, wait.PollUntilContextTimeout(f.ctx, time.Second, 30*time.Second, true, func(ctx context.Context) (bool, error) {
		for token := ""; ; {
			page, err := f.system.ListSubstrateActors(ctx, &apiv1.ListSubstrateActorsRequest{
				Atespace: atespace,
				Page:     &apiv1.PageRequest{Limit: 100, PageToken: token},
			})
			if err != nil {
				return false, err
			}
			if page.GetAteApiError() != "" {
				return false, fmt.Errorf("Substrate actors: %s", page.GetAteApiError())
			}
			for _, actor := range page.GetActors() {
				if actor.GetMetadata().GetName() == actorName && actor.GetMetadata().GetAtespace() == atespace {
					state := actor.GetStatus().GetState()
					return state == ateapipb.ActorState_ACTOR_STATE_SUSPENDED, nil
				}
			}
			token = page.GetPage().GetNextPageToken()
			if token == "" {
				return false, nil
			}
		}
	}), "scheduled Actor %s should release its worker", actorName)
}

func startScheduledRecoveryModel(t *testing.T) (string, <-chan struct{}, func(), *atomic.Int32) {
	t.Helper()
	// This proxy runs on the host, so its upstream must use the local mock URL.
	upstream, err := url.Parse(startMockLLMServer(t, interactionMocks, "mocks/invoke_agent.json"))
	require.NoError(t, err)
	proxy := httputil.NewSingleHostReverseProxy(upstream)
	started, release := make(chan struct{}), make(chan struct{})
	var startedOnce, releaseOnce sync.Once
	calls := &atomic.Int32{}
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = r.Body.Close()
		r.Body = io.NopCloser(bytes.NewReader(body))
		// Match the complete JSON string value. Native clients also send probes
		// and title requests that embed the prompt in a longer instruction.
		if !bytes.Contains(body, []byte(`"What is 2+2?"`)) {
			proxy.ServeHTTP(w, r)
			return
		}
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

func (f *scheduledFixture) waitReconciled(t *testing.T) {
	t.Helper()
	require.NoError(t, wait.PollUntilContextTimeout(f.ctx, time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := f.kube.Get(ctx, ctrlclient.ObjectKeyFromObject(f.schedule), f.schedule); err != nil {
			return false, err
		}
		ready := apimeta.FindStatusCondition(f.schedule.Status.Conditions, v1alpha3.ScheduledRunConditionReady)
		return f.schedule.Status.ObservedGeneration == f.schedule.Generation && ready != nil && ready.Status == metav1.ConditionTrue, nil
	}))
}

func (f *scheduledFixture) assertHumanCheckpointFork(t *testing.T, execution *apiv1.ScheduledRunExecution) {
	t.Helper()
	checkpoint := createCheckpoint(t, f.ctx, f.checkpoints, &apiv1.CreateCheckpointRequest{SessionId: execution.GetSessionId(), RequestId: uuid.NewString(), ExpectedHeadTaskId: execution.GetTaskId()}).GetCheckpoint()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cancel()
		_, err := f.checkpoints.DeleteCheckpoint(ctx, &apiv1.DeleteCheckpointRequest{CheckpointId: checkpoint.Id})
		require.NoError(t, err)
	})
	forked, err := f.checkpoints.ForkSession(f.ctx, &apiv1.ForkSessionRequest{CheckpointId: checkpoint.Id, RequestId: uuid.NewString()})
	require.NoError(t, err)
	fork := forked.GetSession()
	require.NotEqual(t, f.creator, fork.GetCreator())
	require.Equal(t, "e2e", fork.GetCreator())
	require.Equal(t, apiv1.RuntimeState_RUNTIME_STATE_READY, fork.GetState())
	require.Equal(t, string(f.agent.UID), fork.GetAgent().GetUid())
	require.Equal(t, execution.RuntimeRevision, fork.PreparedRevision)
	forkView, err := f.sessions.GetSession(f.ctx, &apiv1.GetSessionRequest{SessionId: fork.Id})
	require.NoError(t, err)
	require.Nil(t, forkView.GetScheduledRun(), "a human fork is not a scheduled execution")
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", "e2e"), time.Minute)
		defer cancel()
		require.NoError(t, deleteIdleSession(ctx, f.sessions, fork.Id))
	})
	conversation := &interactionFixture{ctx: f.ctx, client: f.tasks, sessions: f.sessions, sessionID: fork.Id, contextID: fork.ContextId, tenant: f.agent.Namespace + "/" + f.agent.Name}
	_, _, task := conversation.send(t, "What is 2+2?")
	require.Equal(t, a2atype.TaskStateCompleted, task.Status.State)
}
