package e2e_test

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	a2atype "github.com/a2aproject/a2a-go/v2/a2a"
	a2agrpc "github.com/a2aproject/a2a-go/v2/a2agrpc/v1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	adka2a "github.com/kagent-dev/kagent/go/adk/pkg/a2a"
	apia2a "github.com/kagent-dev/kagent/go/api/a2a"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	kagentenv "github.com/kagent-dev/kagent/go/core/pkg/env"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	utilrand "k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/apimachinery/pkg/util/wait"
	ctrlclient "sigs.k8s.io/controller-runtime/pkg/client"
)

var goADKHarness = testHarness{name: "kagent", runtimeLabel: "kagent"}

// A turn parked on input-required pauses the Actor on its worker's node. Once
// the pause is older than KAGENT_PAUSED_RUNTIME_TTL the controller suspends
// the Actor durably, and the reply resumes it from the external snapshot.
func TestSessionPausedRuntimeSuspendsAfterTTL(t *testing.T) {
	target := interactionTarget(t)
	withControllerEnv(t, target, map[string]string{kagentenv.SessionPausedRuntimeTTL.Name(): "20s"})

	fixture := newInteractionFixture(t, goADKHarness, target, startMockLLM(t, "mocks/invoke_golang_hitl_ask_user.json"))
	fixture.ctx = metadata.AppendToOutgoingContext(fixture.ctx, strings.ToLower(a2atype.SvcParamExtensions), adka2a.HITLExtensionURI)
	_, _, waiting := fixture.send(t, "Which database should we use for storage?")
	require.Equal(t, a2atype.TaskStateInputRequired, waiting.Status.State)
	request := adka2a.GetAskUserRequest(waiting.Status.Message)
	require.NotNil(t, request, "INPUT_REQUIRED task has no ask_user request")

	waitForActorState(t, fixture, ateapipb.ActorState_ACTOR_STATE_SUSPENDED, 2*time.Minute)

	reply := adka2a.AttachHitlExtension(a2atype.NewMessage(a2atype.MessageRoleUser, a2atype.NewTextPart("PostgreSQL")), &apia2a.AskUserResponse{
		Type: adka2a.HITLTypeAskUserResponse, ID: request.ID,
		Answers: []apia2a.AskUserAnswer{{Answer: []string{"PostgreSQL"}}},
	})
	reply.TaskID, reply.ContextID = waiting.ID, waiting.ContextID
	response, err := a2agrpc.NewGRPCTransportFromClient(fixture.client).SendMessage(fixture.ctx, nil, &a2atype.SendMessageRequest{Tenant: fixture.tenant, Message: reply})
	require.NoError(t, err, "resume after the durable suspend")
	completed, ok := response.(*a2atype.Task)
	require.True(t, ok, "resumed response = %T", response)
	require.Equal(t, a2atype.TaskStateCompleted, completed.Status.State, taskText(completed))
	require.Contains(t, taskText(completed), "Using PostgreSQL")
}

// A paused Actor whose worker goes away cannot be resumed or suspended. The
// next send marks the session FAILED with the reason RuntimeLost and answers
// with that message instead of waiting out Substrate's parking budget again.
func TestSessionRuntimeLostFailsTheSession(t *testing.T) {
	target := interactionTarget(t)
	kube := interactionKubeClient(t)

	pool := createIsolatedWorkerPool(t, kube)
	harness := cloneHarness(t, kube, "kagent", "runtime-lost-", func(harness *v1alpha3.Harness) {
		harness.Spec.Substrate.WorkerPoolRef.Name = pool
	})
	model := goADKHarness.createModel(t, kube, startMockLLM(t, "mocks/invoke_golang_hitl_ask_user.json"), nil)
	template := &v1alpha3.AgentTemplate{
		ObjectMeta: metav1.ObjectMeta{GenerateName: "runtime-lost-", Namespace: "kagent", Labels: goADKHarness.labels()},
		Spec: v1alpha3.AgentTemplateSpec{
			ModelConfig: &corev1.LocalObjectReference{Name: model.Name},
			Description: "Runtime loss E2E fixture", SystemPrompt: "Reply briefly.",
		},
	}
	createAndWaitInteractionTemplateForHarness(t, kube, template, harness.Name)
	fixture := newInteractionFixtureForHarnessTemplate(t, target, harness.Name, template.Name)
	fixture.ctx = metadata.AppendToOutgoingContext(fixture.ctx, strings.ToLower(a2atype.SvcParamExtensions), adka2a.HITLExtensionURI)

	_, _, waiting := fixture.send(t, "Which database should we use for storage?")
	require.Equal(t, a2atype.TaskStateInputRequired, waiting.Status.State)
	// The premise: the Actor is paused, not running, when its worker goes away.
	waitForActorState(t, fixture, ateapipb.ActorState_ACTOR_STATE_PAUSED, 2*time.Minute)

	actor, err := findSubstrateActor(fixture.ctx, fixture.system, "", substrate.ActorName(fixture.sessionID))
	require.NoError(t, err)
	require.NotNil(t, actor, "the paused session has no Actor")
	assignment := actor.GetStatus().GetWorkerAssignment()
	require.NotEmpty(t, assignment.GetWorkerPod(), "the paused Actor has no worker pod")
	require.Equal(t, pool, assignment.GetWorkerPool(), "the Actor runs outside its isolated pool")
	worker := &corev1.Pod{ObjectMeta: metav1.ObjectMeta{Namespace: assignment.GetWorkerNamespace(), Name: assignment.GetWorkerPod()}}
	require.NoError(t, kube.Delete(fixture.ctx, worker, ctrlclient.GracePeriodSeconds(0)))

	// The first send after the loss asks the workflow whether the runtime is
	// lost; Substrate needs a moment to notice the worker is gone.
	var lostErr error
	require.Eventually(t, func() bool {
		message, request := newMessageRequest(t, "Are you still there?")
		message.ContextID, request.Message.ContextId, request.Tenant = fixture.sessionID, fixture.sessionID, fixture.tenant
		_, lostErr = fixture.client.SendMessage(fixture.ctx, request)
		return lostErr != nil && strings.Contains(lostErr.Error(), "runtime lost")
	}, 4*time.Minute, 5*time.Second, "a send after the worker's loss must report the runtime lost: %v", lostErr)

	session, err := fixture.sessions.GetSession(fixture.ctx, &apiv1alpha1.GetSessionRequest{SessionId: fixture.sessionID})
	require.NoError(t, err)
	require.Equal(t, apiv1alpha1.RuntimeState_RUNTIME_STATE_FAILED, session.GetSession().GetState())
	require.Equal(t, apia2a.FailureReasonRuntimeLost, session.GetSession().GetFailure().GetReason())
	require.Equal(t, apiv1alpha1.RuntimeOperation_RUNTIME_OPERATION_NONE, session.GetSession().GetOperation(), "the failure leaves no lifecycle operation pending")

	// The transcript stays readable and the session deletable; the fixture's
	// cleanup deletes it.
	_, err = fixture.sessions.ResumeSession(fixture.ctx, &apiv1alpha1.ResumeSessionRequest{SessionId: fixture.sessionID})
	require.Error(t, err, "a lost runtime cannot be resumed")
}

// waitForActorState polls the session's Substrate Actor until it reaches the
// state or the timeout passes.
func waitForActorState(t *testing.T, fixture *interactionFixture, want ateapipb.ActorState, timeout time.Duration) {
	t.Helper()
	actorID := substrate.ActorName(fixture.sessionID)
	var last ateapipb.ActorState
	err := wait.PollUntilContextTimeout(fixture.ctx, 2*time.Second, timeout, true, func(ctx context.Context) (bool, error) {
		actor, err := findSubstrateActor(ctx, fixture.system, "", actorID)
		if err != nil {
			return false, err
		}
		if actor == nil {
			return false, nil
		}
		last = actor.GetStatus().GetState()
		return last == want, nil
	})
	require.NoError(t, err, "Actor %s did not reach %s, last state %s", actorID, want, last)
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
		current := &appsv1.Deployment{}
		require.NoError(t, kube.Get(ctx, key, current))
		base := current.DeepCopy()
		current.Spec.Template.Spec.Containers[index].Env = env
		require.NoError(t, kube.Patch(ctx, current, ctrlclient.MergeFrom(base)))
		require.NoError(t, wait.PollUntilContextCancel(ctx, time.Second, true, func(ctx context.Context) (bool, error) {
			observed := &appsv1.Deployment{}
			if err := kube.Get(ctx, key, observed); err != nil {
				return false, err
			}
			// AvailableReplicas counts the outgoing pod until the Deployment
			// scales its ReplicaSet down, so the rollout is over only when the
			// total matches too; a probe before that can land on the old pod,
			// whose termination then refuses the next test's connections.
			return observed.Status.ObservedGeneration >= current.Generation && observed.Status.UpdatedReplicas == *observed.Spec.Replicas && observed.Status.AvailableReplicas == *observed.Spec.Replicas && observed.Status.Replicas == *observed.Spec.Replicas, nil
		}))
		conn, err := grpc.NewClient(target, grpc.WithTransportCredentials(insecure.NewCredentials()))
		require.NoError(t, err)
		defer func() { require.NoError(t, conn.Close()) }()
		require.NoError(t, waitForControllerAPI(ctx, conn), "controller API did not become reachable after rollout")
	}
	t.Cleanup(func() { apply(original) })
	apply(updated)
}

// cloneHarness copies an installed Harness into a generated one the test owns
// and lets the caller change its spec before it is created.
func cloneHarness(t *testing.T, kube ctrlclient.Client, base, prefix string, mutate func(*v1alpha3.Harness)) *v1alpha3.Harness {
	t.Helper()
	source := &v1alpha3.Harness{}
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: base}, source), "get %s Harness", base)
	harness := &v1alpha3.Harness{
		ObjectMeta: metav1.ObjectMeta{GenerateName: prefix, Namespace: "kagent"},
		Spec:       *source.Spec.DeepCopy(),
	}
	mutate(harness)
	require.NoError(t, kube.Create(t.Context(), harness), "create %s Harness", prefix)
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), harness); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete %s Harness: %v", harness.Name, err)
		}
	})
	return harness
}

// createIsolatedWorkerPool clones the chart's WorkerPool into a one-worker pool
// the test may destroy without touching the Actors of other tests, and waits
// for its worker to be ready.
func createIsolatedWorkerPool(t *testing.T, kube ctrlclient.Client) string {
	t.Helper()
	gvk := unstructuredWorkerPoolGVK()
	source := &unstructured.Unstructured{}
	source.SetGroupVersionKind(gvk)
	require.NoError(t, kube.Get(t.Context(), ctrlclient.ObjectKey{Namespace: "kagent", Name: "kagent-default"}, source), "get the chart's WorkerPool")
	spec, _, err := unstructured.NestedMap(source.Object, "spec")
	require.NoError(t, err)
	// A worker carries its WorkerPool's labels, and a template selects its
	// workers by the pool labels the chart sets on the WorkerPool, so the
	// isolated pool needs them with its own name or no worker matches.
	name := "e2e-lost-" + utilrand.String(5)
	pool := &unstructured.Unstructured{}
	pool.SetGroupVersionKind(gvk)
	pool.SetNamespace("kagent")
	pool.SetName(name)
	pool.SetLabels(map[string]string{"kagent.dev/worker-pool-name": name, "kagent.dev/worker-pool-namespace": "kagent"})
	require.NoError(t, unstructured.SetNestedMap(pool.Object, spec, "spec"))
	require.NoError(t, unstructured.SetNestedField(pool.Object, int64(1), "spec", "replicas"))
	require.NoError(t, kube.Create(t.Context(), pool), "create the isolated WorkerPool")
	t.Cleanup(func() {
		if err := kube.Delete(context.Background(), pool); err != nil && !apierrors.IsNotFound(err) {
			t.Errorf("delete WorkerPool %s: %v", pool.GetName(), err)
		}
	})
	err = wait.PollUntilContextTimeout(t.Context(), 2*time.Second, 3*time.Minute, true, func(ctx context.Context) (bool, error) {
		pods := &corev1.PodList{}
		if err := kube.List(ctx, pods, ctrlclient.InNamespace("kagent")); err != nil {
			return false, err
		}
		for _, pod := range pods.Items {
			if !strings.HasPrefix(pod.Name, name+"-") {
				continue
			}
			for _, condition := range pod.Status.Conditions {
				if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
					return true, nil
				}
			}
		}
		return false, nil
	})
	require.NoError(t, err, "the isolated WorkerPool %s has no ready worker", name)
	return name
}

func unstructuredWorkerPoolGVK() schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: "ate.dev", Version: "v1alpha1", Kind: "WorkerPool"}
}
