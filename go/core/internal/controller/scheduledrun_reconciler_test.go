package controller

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type scheduleProjectionRecorder struct {
	projected *database.ScheduledRun
	disabled  bool
	retired   bool
	failure   error
}

func (s *scheduleProjectionRecorder) SyncScheduledRun(_ context.Context, run *database.ScheduledRun) (*database.ScheduledRun, error) {
	s.projected = run
	s.disabled = false
	return run, s.failure
}
func (s *scheduleProjectionRecorder) GetActiveScheduledRunID(context.Context, types.NamespacedName) (uuid.UUID, error) {
	if s.projected == nil || s.retired {
		return uuid.Nil, database.ErrNotFound
	}
	return uuid.Parse(s.projected.ID)
}
func (s *scheduleProjectionRecorder) DisableScheduledRun(context.Context, uuid.UUID, int64) error {
	s.disabled = true
	return s.failure
}
func (s *scheduleProjectionRecorder) RetireScheduledRun(context.Context, uuid.UUID) error {
	s.retired = true
	return s.failure
}

func TestScheduledRunReconciliationLifecycle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx := t.Context()
		opts := krt.NewOptionsBuilder(ctx.Done(), "schedule-test", nil)
		resource := &v1alpha3.ScheduledRun{ObjectMeta: metav1.ObjectMeta{Name: "daily", Namespace: "team", UID: types.UID(uuid.NewString()), Generation: 1}, Spec: v1alpha3.ScheduledRunSpec{AgentRef: corev1.LocalObjectReference{Name: "assistant"}, Schedule: "0 9 * * *", Prompt: "hello"}}
		agent := &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: resource.Namespace, Name: resource.Spec.AgentRef.Name, UID: types.UID(uuid.NewString())}, Spec: v1alpha3.AgentSpec{HarnessRef: corev1.LocalObjectReference{Name: "kagent"}}, Status: v1alpha3.AgentStatus{LatestSuccessfulRevision: "last-successful"}}
		resources := krt.NewStaticCollection(nil, []*v1alpha3.ScheduledRun{resource}, opts.WithName("resources")...)
		agents := krt.NewStaticCollection(nil, []*v1alpha3.Agent{agent}, opts.WithName("agents")...)
		states := newScheduledRunReconciliations(resources, agents, opts)
		synctest.Wait()
		scheme := runtime.NewScheme()
		require.NoError(t, v1alpha3.AddToScheme(scheme))
		kube := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha3.ScheduledRun{}).WithObjects(resource).Build()
		store := &scheduleProjectionRecorder{}
		reconciler := &ScheduledRunReconciler{states: states, store: store, kube: kube, reader: kube, caller: auth.NewCredentialSession(auth.Principal{ID: "scheduler"}, nil)}
		key := client.ObjectKeyFromObject(resource)
		refresh := func() {
			require.NoError(t, kube.Get(ctx, key, resource))
			resources.UpdateObject(resource.DeepCopy())
			synctest.Wait()
		}

		require.NoError(t, reconciler.reconcile(ctx, "team/daily"))
		refresh()
		require.Empty(t, resource.Finalizers)
		require.Equal(t, "scheduler", store.projected.Creator)
		require.True(t, meta.IsStatusConditionTrue(resource.Status.Conditions, v1alpha3.ScheduledRunConditionReady))
		// A failed replacement is reported by Agent status while the previous revision stays usable.
		agent.Status.Conditions = []metav1.Condition{{Type: v1alpha3.AgentConditionReady, Status: metav1.ConditionFalse, Reason: "ActorTemplateFailed", Message: "replacement failed"}}
		agents.UpdateObject(agent.DeepCopy())
		synctest.Wait()
		require.NoError(t, reconciler.reconcile(ctx, "team/daily"))
		refresh()
		require.False(t, store.disabled)
		require.True(t, meta.IsStatusConditionTrue(resource.Status.Conditions, v1alpha3.ScheduledRunConditionReady))
		require.Equal(t, string(agent.UID), store.projected.AgentUID)
		// A recreated Agent cannot borrow the previous identity's successful revision.
		agent.UID = types.UID(uuid.NewString())
		agent.Status.LatestSuccessfulRevision = ""
		agents.UpdateObject(agent.DeepCopy())
		synctest.Wait()
		require.NoError(t, reconciler.reconcile(ctx, "team/daily"))
		refresh()
		require.True(t, store.disabled)
		require.Equal(t, ScheduledRunReasonAgentNotReady, meta.FindStatusCondition(resource.Status.Conditions, v1alpha3.ScheduledRunConditionReady).Reason)
		agent.Status.LatestSuccessfulRevision = "replacement"
		agents.UpdateObject(agent.DeepCopy())
		synctest.Wait()
		require.NoError(t, reconciler.reconcile(ctx, "team/daily"))
		refresh()
		require.False(t, store.disabled)
		require.Equal(t, string(agent.UID), store.projected.AgentUID)
		agents.DeleteObject("team/assistant")
		synctest.Wait()
		require.NoError(t, reconciler.reconcile(ctx, "team/daily"))
		refresh()
		require.True(t, store.disabled)
		require.Equal(t, ScheduledRunReasonAgentNotFound, meta.FindStatusCondition(resource.Status.Conditions, v1alpha3.ScheduledRunConditionReady).Reason)
		require.Contains(t, meta.FindStatusCondition(resource.Status.Conditions, v1alpha3.ScheduledRunConditionReady).Message, "Agent does not exist")
		require.NoError(t, kube.Delete(ctx, resource))
		store.failure = errors.New("database unavailable")
		require.Error(t, reconciler.reconcile(ctx, "team/daily"))
		store.failure = nil
		require.NoError(t, reconciler.reconcile(ctx, "team/daily"))
		require.True(t, store.retired)
		require.Error(t, kube.Get(ctx, key, &v1alpha3.ScheduledRun{}))
	})
}

func TestScheduledRunDeleteEventRetiresProjectionWithoutFinalizer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		resource := &v1alpha3.ScheduledRun{ObjectMeta: metav1.ObjectMeta{
			Name: "daily", Namespace: "team", UID: types.UID(uuid.NewString()), Generation: 1,
		}, Spec: v1alpha3.ScheduledRunSpec{AgentRef: corev1.LocalObjectReference{Name: "assistant"}, Schedule: "0 9 * * *", Prompt: "hello"}}
		opts := krt.NewOptionsBuilder(ctx.Done(), "delete-event", nil)
		states := krt.NewStaticCollection(nil, []ScheduledRunReconciliation{{Resource: resource, AgentUID: "agent-uid"}}, opts.WithName("states")...)
		scheme := runtime.NewScheme()
		require.NoError(t, v1alpha3.AddToScheme(scheme))
		kube := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha3.ScheduledRun{}).WithObjects(resource).Build()
		store := &scheduleProjectionRecorder{}
		reconciler := NewScheduledRunReconciler(states, store, kube, kube, auth.NewCredentialSession(auth.Principal{ID: "scheduler"}, nil))
		done := make(chan error, 1)
		go func() { done <- reconciler.Start(ctx) }()
		synctest.Wait()
		require.NotNil(t, store.projected)
		require.NoError(t, kube.Get(ctx, client.ObjectKeyFromObject(resource), resource))
		resource.Finalizers = nil
		require.NoError(t, kube.Update(ctx, resource))
		require.NoError(t, kube.Delete(ctx, resource))
		states.DeleteObject("team/daily")
		synctest.Wait()
		require.True(t, store.retired, "a delete event must retire the durable projection even when the finalizer was removed")
		cancel()
		require.NoError(t, <-done)
	})
}

func TestScheduledRunEqualityIgnoresStatusWrites(t *testing.T) {
	before := ScheduledRunReconciliation{Resource: &v1alpha3.ScheduledRun{ObjectMeta: metav1.ObjectMeta{Name: "daily", Namespace: "team", UID: "uid", Generation: 1}}}
	after := before
	after.Resource = before.Resource.DeepCopy()
	after.Resource.ResourceVersion = "2"
	after.Resource.Status.ObservedGeneration = 1
	require.True(t, before.Equals(after))
	after.Resource.Spec.Paused = new(true)
	require.False(t, before.Equals(after))
}
