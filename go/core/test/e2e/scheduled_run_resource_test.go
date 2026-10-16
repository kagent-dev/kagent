package e2e_test

import (
	"context"
	"testing"
	"time"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/metadata"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	schedules "github.com/kagent-dev/kagent/go/api/scheduledrun"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

// TestScheduledRunResource declares a schedule in Kubernetes and verifies the
// controller persists it under the resource's own creator, runs it, follows
// edits, and retires it on deletion.
func TestScheduledRunResource(t *testing.T) {
	t.Parallel()
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		template := createInteractionTemplate(t, harness, startInteractionMock(t))
		kube := interactionKubeClient(t)
		resource := &v1alpha3.ScheduledRun{
			ObjectMeta: metav1.ObjectMeta{GenerateName: "declared-", Namespace: "kagent"},
			Spec: v1alpha3.ScheduledRunSpec{
				AgentRef: corev1.LocalObjectReference{Name: template},
				Schedule: "* * * * *",
				Prompt:   "What is 2+2?",
				Paused:   new(true),
			},
		}
		require.NoError(t, kube.Create(t.Context(), resource))
		t.Cleanup(func() {
			if err := kube.Delete(context.Background(), resource); err != nil && !apierrors.IsNotFound(err) {
				t.Errorf("delete ScheduledRun: %v", err)
			}
		})
		waitScheduledRunReady(t, kube, resource)

		// The row is owned by the resource, not by any human principal.
		creator := schedules.KubernetesCreator(resource.Namespace, resource.Name)
		conn := newControllerConn(t, interactionTarget(t))
		ctx, cancel := context.WithTimeout(metadata.AppendToOutgoingContext(t.Context(), "x-user-id", creator), 5*time.Minute)
		t.Cleanup(cancel)
		f := &scheduledFixture{ctx: ctx, schedules: apiv1.NewScheduledRunServiceClient(conn), sessions: apiv1.NewSessionServiceClient(conn), system: apiv1.NewSystemServiceClient(conn), tasks: a2apb.NewA2AServiceClient(conn)}
		f.schedule = declaredSchedule(t, f)
		require.Equal(t, template, f.schedule.GetAgent().GetName())
		require.Equal(t, "kagent", f.schedule.GetAgent().GetNamespace())
		require.True(t, f.schedule.GetConfig().GetPaused())
		require.Equal(t, "UTC", f.schedule.GetConfig().GetTimeZone())
		require.Equal(t, 15*time.Minute, f.schedule.GetConfig().GetExecutionTimeout().AsDuration())
		require.Nil(t, f.schedule.GetNextExecutionTime())
		humanCtx := metadata.AppendToOutgoingContext(t.Context(), "x-user-id", "e2e")
		_, err := f.schedules.GetScheduledRun(humanCtx, &apiv1.GetScheduledRunRequest{ScheduledRunId: f.schedule.GetId()})
		require.Error(t, err, "a human principal must not see a declared schedule")

		execution := f.trigger(t)
		t.Cleanup(func() {
			if execution.GetSessionId() == "" {
				return
			}
			cleanupCtx, cleanupCancel := context.WithTimeout(metadata.AppendToOutgoingContext(context.Background(), "x-user-id", creator), 2*time.Minute)
			defer cleanupCancel()
			if err := deleteIdleSession(cleanupCtx, f.sessions, execution.GetSessionId()); err != nil {
				t.Errorf("delete declared session: %v", err)
			}
		})
		execution = f.waitExecution(t, execution.GetId(), apiv1.ScheduledRunExecutionState_SCHEDULED_RUN_EXECUTION_STATE_SUCCEEDED)
		f.assertCompletedTask(t, execution)

		// A spec edit converges onto the same schedule identity.
		require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKeyFromObject(resource), resource))
		resource.Spec.Prompt = "What is 3+3?"
		require.NoError(t, kube.Update(t.Context(), resource))
		waitScheduledRunReady(t, kube, resource)
		edited := declaredSchedule(t, f)
		require.Equal(t, f.schedule.GetId(), edited.GetId())
		require.Equal(t, "What is 3+3?", edited.GetConfig().GetPrompt())
		require.NotEqual(t, f.schedule.GetEtag(), edited.GetEtag())

		// Deleting the resource retires the schedule and retains its history.
		require.NoError(t, kube.Delete(t.Context(), resource))
		require.NoError(t, wait.PollUntilContextTimeout(ctx, time.Second, time.Minute, true, func(ctx context.Context) (bool, error) {
			result, err := f.schedules.ListScheduledRuns(ctx, &apiv1.ListScheduledRunsRequest{})
			if err != nil {
				return false, err
			}
			return len(result.GetScheduledRuns()) == 0, nil
		}), "declared schedule should be retired")
		tombstone, err := f.schedules.GetScheduledRun(ctx, &apiv1.GetScheduledRunRequest{ScheduledRunId: f.schedule.GetId()})
		require.NoError(t, err)
		require.NotNil(t, tombstone.GetScheduledRun().GetDeletedAt())
		retained, err := f.schedules.GetScheduledRunExecution(ctx, &apiv1.GetScheduledRunExecutionRequest{ExecutionId: execution.GetId()})
		require.NoError(t, err)
		require.Equal(t, execution.GetState(), retained.GetExecution().GetState())
	})
}

func declaredSchedule(t *testing.T, f *scheduledFixture) *apiv1.ScheduledRun {
	t.Helper()
	result, err := f.schedules.ListScheduledRuns(f.ctx, &apiv1.ListScheduledRunsRequest{})
	require.NoError(t, err)
	require.Len(t, result.GetScheduledRuns(), 1, "a resource owns exactly one live schedule")
	return result.GetScheduledRuns()[0]
}

func waitScheduledRunReady(t *testing.T, kube ctrlclient.Client, resource *v1alpha3.ScheduledRun) {
	t.Helper()
	err := wait.PollUntilContextTimeout(t.Context(), time.Second, 2*time.Minute, true, func(ctx context.Context) (bool, error) {
		if err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(resource), resource); err != nil {
			return false, err
		}
		if resource.Status.ObservedGeneration != resource.Generation {
			return false, nil
		}
		for _, condition := range resource.Status.Conditions {
			if condition.Type == v1alpha3.ScheduledRunConditionReady {
				return condition.Status == metav1.ConditionTrue, nil
			}
		}
		return false, nil
	})
	if err != nil {
		t.Fatalf("wait for ScheduledRun %s/%s: %v; last conditions: %+v", resource.Namespace, resource.Name, err, resource.Status.Conditions)
	}
	for _, condition := range resource.Status.Conditions {
		require.Equal(t, metav1.ConditionTrue, condition.Status, "%s: %s", condition.Type, condition.Message)
	}
}
