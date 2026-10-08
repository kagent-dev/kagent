package e2e_test

import (
	"context"
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/a2aproject/a2a-go/v2/a2a"
	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// Keep the parent sequential so controller rollouts cannot overlap other tests.
// Harness subtests run in parallel to share the idle period and expiration sweep.
// The API endpoint must survive pod replacement (NodePort or ingress).
func TestSessionIdleExpiration(t *testing.T) {
	target := interactionTarget(t)
	setSessionExpirationPolicy(t, target, "30s", "5s")
	forEachHarness(t, func(t *testing.T, harness testHarness) {
		t.Parallel()
		fixture := newInteractionFixture(t, harness, target, startInteractionMock(t))
		original, err := fixture.sessions.GetSession(fixture.ctx, &apiv1alpha1.GetSessionRequest{SessionId: fixture.sessionID})
		require.NoError(t, err)
		request := &apiv1alpha1.CreateSessionRequest{Agent: original.Session.Agent, RequestId: uuid.NewString()}
		created, err := fixture.sessions.CreateSession(fixture.ctx, request)
		require.NoError(t, err)
		id := created.Session.Id
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(fixture.ctx), time.Minute)
			defer cancel()
			require.NoError(t, deleteIdleSession(ctx, fixture.sessions, id))
		})
		conversation := *fixture
		conversation.sessionID, conversation.contextID = id, created.Session.ContextId
		_, _, task := conversation.send(t, "What is 2+2?")
		require.Equal(t, a2a.TaskStateCompleted, task.Status.State)
		assertActorSuspended(t, &conversation)
		require.Eventually(t, func() bool {
			_, err := fixture.sessions.GetSession(fixture.ctx, &apiv1alpha1.GetSessionRequest{SessionId: id})
			return status.Code(err) == codes.NotFound
		}, 2*time.Minute, time.Second, "idle session did not expire")
		actor, err := findSubstrateActor(fixture.ctx, fixture.system, request.Agent.Namespace, substrate.ActorName(id))
		require.NoError(t, err)
		require.Nil(t, actor, "expiration must remove the Substrate Actor")
		var fresh *apiv1alpha1.CreateSessionResponse
		require.Eventually(t, func() bool {
			fresh, err = fixture.sessions.CreateSession(fixture.ctx, request)
			return err == nil && fresh.Session.Id != id
		}, time.Minute, time.Second, "expiration must release the conversation request ID")
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.WithoutCancel(fixture.ctx), time.Minute)
			defer cancel()
			require.NoError(t, deleteIdleSession(ctx, fixture.sessions, fresh.Session.Id))
		})
		require.NotEqual(t, created.Session.ContextId, fresh.Session.ContextId)
	})
}

func setSessionExpirationPolicy(t *testing.T, target, ttl, pollInterval string) {
	t.Helper()
	withControllerEnv(t, target, map[string]string{
		kagentenv.SessionIdleTTL.Name():                ttl,
		kagentenv.SessionExpirationPollInterval.Name(): pollInterval,
	})
}

// withControllerEnv sets variables on the controller Deployment for the test
// and restores the original environment afterwards, waiting out both rollouts.
func withControllerEnv(t *testing.T, target string, values map[string]string) {
	t.Helper()
	kube := interactionKubeClient(t)
	require.NoError(t, appsv1.AddToScheme(kube.Scheme()))
	deployments := &appsv1.DeploymentList{}
	require.NoError(t, kube.List(t.Context(), deployments, ctrlclient.InNamespace("kagent"), ctrlclient.MatchingLabels{"app.kubernetes.io/component": "controller"}))
	require.Len(t, deployments.Items, 1)
	deployment := &deployments.Items[0]
	index := slices.IndexFunc(deployment.Spec.Template.Spec.Containers, func(container corev1.Container) bool { return container.Name == "controller" })
	require.NotEqual(t, -1, index)
	original := slices.Clone(deployment.Spec.Template.Spec.Containers[index].Env)
	updated := slices.DeleteFunc(slices.Clone(original), func(env corev1.EnvVar) bool {
		_, replaced := values[env.Name]
		return replaced
	})
	for _, name := range slices.Sorted(maps.Keys(values)) {
		updated = append(updated, corev1.EnvVar{Name: name, Value: values[name]})
	}
	key := ctrlclient.ObjectKeyFromObject(deployment)
	apply := func(env []corev1.EnvVar) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		previous := &corev1.PodList{}
		require.NoError(t, kube.List(ctx, previous, ctrlclient.InNamespace(key.Namespace), ctrlclient.MatchingLabels{"app.kubernetes.io/component": "controller"}))
		current := &appsv1.Deployment{}
		require.NoError(t, kube.Get(ctx, key, current))
		base := current.DeepCopy()
		current.Spec.Template.Spec.Containers[index].Env = env
		require.NoError(t, kube.Patch(ctx, current, ctrlclient.MergeFrom(base)))
		rolled := current.Generation != base.Generation
		require.NoError(t, wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
			observed := &appsv1.Deployment{}
			if err := kube.Get(ctx, key, observed); err != nil {
				return false, err
			}
			if observed.Status.ObservedGeneration < current.Generation || observed.Status.UpdatedReplicas != *observed.Spec.Replicas || observed.Status.AvailableReplicas != *observed.Spec.Replicas {
				return false, nil
			}
			if !rolled {
				return true, nil
			}
			// The Deployment's counters can read complete while an outgoing
			// pod still serves; once it terminates it refuses the next test's
			// connections. The rollout is over when every pod from before the
			// change is gone.
			for _, pod := range previous.Items {
				err := kube.Get(ctx, ctrlclient.ObjectKeyFromObject(&pod), &corev1.Pod{})
				if !apierrors.IsNotFound(err) {
					return false, ctrlclient.IgnoreNotFound(err)
				}
			}
			return true, nil
		}), "controller rollout did not replace its pods")
		conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		defer func() { require.NoError(t, conn.Close()) }()
		require.NoError(t, waitForControllerAPI(ctx, conn), "controller API did not become reachable after rollout")
	}
	t.Cleanup(func() { apply(original) })
	apply(updated)
}
