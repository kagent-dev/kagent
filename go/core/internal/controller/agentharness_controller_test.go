package controller

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend"
	"github.com/kagent-dev/kagent/go/core/pkg/sandboxbackend/substrate"
)

type fakeSubstrateLifecycle struct {
	state        substrate.LifecycleState
	ensureErr    error
	cleanupDone  bool
	cleanupErr   error
	ensureCalls  int
	cleanupCalls int
}

func (f *fakeSubstrateLifecycle) EnsureGeneratedTemplate(_ context.Context, _ *v1alpha2.AgentHarness) (substrate.LifecycleState, error) {
	f.ensureCalls++
	return f.state, f.ensureErr
}

func (f *fakeSubstrateLifecycle) CleanupGeneratedTemplate(_ context.Context, _ *v1alpha2.AgentHarness) (bool, error) {
	f.cleanupCalls++
	return f.cleanupDone, f.cleanupErr
}

type fakeAgentHarnessBackend struct {
	ensureCalls int
	deleteCalls int
	readyCalls  int

	ensureHandle string
	endpoint     string
	status       metav1.ConditionStatus
	reason       string
	message      string

	deleteDone bool
	deleteErr  error
	readyErr   error
}

func (f *fakeAgentHarnessBackend) Name() v1alpha2.AgentHarnessBackendType {
	return v1alpha2.AgentHarnessBackendOpenClaw
}

func (f *fakeAgentHarnessBackend) EnsureAgentHarness(context.Context, *v1alpha2.AgentHarness) (sandboxbackend.EnsureResult, error) {
	f.ensureCalls++
	id := f.ensureHandle
	if id == "" {
		id = "actor-1"
	}
	return sandboxbackend.EnsureResult{
		Handle:   sandboxbackend.Handle{ID: id},
		Endpoint: f.endpoint,
	}, nil
}

func (f *fakeAgentHarnessBackend) GetStatus(context.Context, sandboxbackend.Handle) (metav1.ConditionStatus, string, string) {
	st := f.status
	if st == "" {
		st = metav1.ConditionTrue
	}
	reason := f.reason
	if reason == "" {
		reason = "Running"
	}
	return st, reason, f.message
}

func (f *fakeAgentHarnessBackend) DeleteAgentHarness(context.Context, sandboxbackend.Handle) (bool, error) {
	f.deleteCalls++
	return f.deleteDone, f.deleteErr
}

func (f *fakeAgentHarnessBackend) OnAgentHarnessReady(context.Context, *v1alpha2.AgentHarness, sandboxbackend.Handle) error {
	f.readyCalls++
	return f.readyErr
}

func TestAgentHarnessController_SubstrateWaitsForGeneratedTemplate(t *testing.T) {
	ctx := context.Background()
	ah := newSubstrateHarness("kagent", "claw")
	controller := newAgentHarnessTestController(t, ah)
	lifecycle := &fakeSubstrateLifecycle{state: substrate.LifecycleState{ActorTemplateReady: false}}
	backend := &fakeAgentHarnessBackend{}
	controller.SubstrateLifecycle = lifecycle
	controller.OpenClawBackend = backend

	result, err := controller.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ah)})
	require.NoError(t, err)
	require.Equal(t, agentHarnessNotReadyRequeue, result.RequeueAfter)
	require.Equal(t, 1, lifecycle.ensureCalls)
	require.Zero(t, backend.ensureCalls, "actor backend must not run before ActorTemplate is ready")

	latest := getAgentHarness(t, controller.Client, ah)
	requireCondition(t, latest, v1alpha2.AgentHarnessConditionTypeAccepted, metav1.ConditionTrue, "SubstrateLifecyclePending")
	requireCondition(t, latest, v1alpha2.AgentHarnessConditionTypeActorTemplateReady, metav1.ConditionFalse, "NotReady")
	requireCondition(t, latest, v1alpha2.AgentHarnessConditionTypeActorReady, metav1.ConditionFalse, "ActorNotCreated")
	requireCondition(t, latest, v1alpha2.AgentHarnessConditionTypeReady, metav1.ConditionFalse, "ActorTemplateNotReady")
}

func TestAgentHarnessController_SubstrateLifecycleErrorSetsStatus(t *testing.T) {
	ctx := context.Background()
	ah := newSubstrateHarness("kagent", "claw")
	controller := newAgentHarnessTestController(t, ah)
	lifecycle := &fakeSubstrateLifecycle{ensureErr: errors.New("workerpool missing")}
	backend := &fakeAgentHarnessBackend{}
	controller.SubstrateLifecycle = lifecycle
	controller.OpenClawBackend = backend

	_, err := controller.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ah)})
	require.ErrorContains(t, err, "workerpool missing")
	require.Equal(t, 1, lifecycle.ensureCalls)
	require.Zero(t, backend.ensureCalls)

	latest := getAgentHarness(t, controller.Client, ah)
	requireCondition(t, latest, v1alpha2.AgentHarnessConditionTypeAccepted, metav1.ConditionFalse, "SubstrateLifecycleFailed")
	requireCondition(t, latest, v1alpha2.AgentHarnessConditionTypeReady, metav1.ConditionFalse, "SubstrateLifecycleFailed")
}

func TestAgentHarnessController_SubstrateReadyCreatesActorAndRunsBootstrap(t *testing.T) {
	ctx := context.Background()
	ah := newSubstrateHarness("kagent", "claw")
	controller := newAgentHarnessTestController(t, ah)
	lifecycle := &fakeSubstrateLifecycle{state: substrate.LifecycleState{ActorTemplateReady: true}}
	backend := &fakeAgentHarnessBackend{ensureHandle: "actor-1", endpoint: "kagent gateway: /api/agentharnesses/kagent/claw/gateway/"}
	controller.SubstrateLifecycle = lifecycle
	controller.OpenClawBackend = backend

	result, err := controller.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ah)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
	require.Equal(t, 1, lifecycle.ensureCalls)
	require.Equal(t, 1, backend.ensureCalls)
	require.Equal(t, 1, backend.readyCalls)

	latest := getAgentHarness(t, controller.Client, ah)
	require.NotNil(t, latest.Status.BackendRef)
	require.Equal(t, "actor-1", latest.Status.BackendRef.ID)
	require.NotNil(t, latest.Status.Connection)
	requireCondition(t, latest, v1alpha2.AgentHarnessConditionTypeAccepted, metav1.ConditionTrue, "AgentHarnessAccepted")
	requireCondition(t, latest, v1alpha2.AgentHarnessConditionTypeActorTemplateReady, metav1.ConditionTrue, "Ready")
	requireCondition(t, latest, v1alpha2.AgentHarnessConditionTypeActorReady, metav1.ConditionTrue, "Running")
	requireCondition(t, latest, v1alpha2.AgentHarnessConditionTypeBootstrapReady, metav1.ConditionTrue, "BootstrapComplete")
	requireCondition(t, latest, v1alpha2.AgentHarnessConditionTypeReady, metav1.ConditionTrue, "Running")

	result, err = controller.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ah)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
	require.Equal(t, 1, backend.readyCalls, "bootstrap should not rerun for an already bootstrapped generation")
}

func TestAgentHarnessController_SubstrateDeleteWaitsForActorBeforeTemplateCleanup(t *testing.T) {
	ctx := context.Background()
	ah := newDeletingSubstrateHarness("kagent", "claw")
	ah.Status.BackendRef = &v1alpha2.AgentHarnessStatusRef{Backend: v1alpha2.AgentHarnessBackendOpenClaw, ID: "actor-1"}
	controller := newAgentHarnessTestController(t, ah)
	lifecycle := &fakeSubstrateLifecycle{cleanupDone: true}
	backend := &fakeAgentHarnessBackend{deleteDone: false}
	controller.SubstrateLifecycle = lifecycle
	controller.OpenClawBackend = backend

	result, err := controller.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ah)})
	require.NoError(t, err)
	require.Equal(t, agentHarnessNotReadyRequeue, result.RequeueAfter)
	require.Equal(t, 1, backend.deleteCalls)
	require.Zero(t, lifecycle.cleanupCalls, "template cleanup must wait for harness actor deletion")

	latest := getAgentHarness(t, controller.Client, ah)
	require.NotNil(t, latest.Status.BackendRef)
	requireCondition(t, latest, v1alpha2.AgentHarnessConditionTypeActorReady, metav1.ConditionFalse, "ActorDeleting")
	require.Contains(t, latest.Finalizers, agentHarnessFinalizer)
}

func TestAgentHarnessController_SubstrateDeleteWaitsForGeneratedTemplateCleanup(t *testing.T) {
	ctx := context.Background()
	ah := newDeletingSubstrateHarness("kagent", "claw")
	ah.Status.BackendRef = &v1alpha2.AgentHarnessStatusRef{Backend: v1alpha2.AgentHarnessBackendOpenClaw, ID: "actor-1"}
	controller := newAgentHarnessTestController(t, ah)
	lifecycle := &fakeSubstrateLifecycle{cleanupDone: false}
	backend := &fakeAgentHarnessBackend{deleteDone: true}
	controller.SubstrateLifecycle = lifecycle
	controller.OpenClawBackend = backend

	result, err := controller.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ah)})
	require.NoError(t, err)
	require.Equal(t, agentHarnessNotReadyRequeue, result.RequeueAfter)
	require.Equal(t, 1, backend.deleteCalls)
	require.Equal(t, 1, lifecycle.cleanupCalls)

	latest := getAgentHarness(t, controller.Client, ah)
	require.Nil(t, latest.Status.BackendRef)
	requireCondition(t, latest, v1alpha2.AgentHarnessConditionTypeActorTemplateReady, metav1.ConditionFalse, "GoldenActorDeleting")
	require.Contains(t, latest.Finalizers, agentHarnessFinalizer)
}

func TestAgentHarnessController_SubstrateDeleteRemovesFinalizerAfterCleanup(t *testing.T) {
	ctx := context.Background()
	ah := newDeletingSubstrateHarness("kagent", "claw")
	ah.Status.BackendRef = &v1alpha2.AgentHarnessStatusRef{Backend: v1alpha2.AgentHarnessBackendOpenClaw, ID: "actor-1"}
	controller := newAgentHarnessTestController(t, ah)
	lifecycle := &fakeSubstrateLifecycle{cleanupDone: true}
	backend := &fakeAgentHarnessBackend{deleteDone: true}
	controller.SubstrateLifecycle = lifecycle
	controller.OpenClawBackend = backend

	result, err := controller.Reconcile(ctx, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(ah)})
	require.NoError(t, err)
	require.Equal(t, ctrl.Result{}, result)
	require.Equal(t, 1, backend.deleteCalls)
	require.Equal(t, 1, lifecycle.cleanupCalls)

	var latest v1alpha2.AgentHarness
	err = controller.Client.Get(ctx, client.ObjectKeyFromObject(ah), &latest)
	require.True(t, apierrors.IsNotFound(err), "fake client should complete deletion after finalizer removal")
}

func newAgentHarnessTestController(t *testing.T, objects ...client.Object) *SubstrateAgentHarnessController {
	t.Helper()
	scheme := runtime.NewScheme()
	utilruntime.Must(v1alpha2.AddToScheme(scheme))
	kube := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(objects...).
		WithStatusSubresource(&v1alpha2.AgentHarness{}).
		Build()
	return &SubstrateAgentHarnessController{Client: kube}
}

func newSubstrateHarness(namespace, name string) *v1alpha2.AgentHarness {
	return &v1alpha2.AgentHarness{
		TypeMeta: metav1.TypeMeta{APIVersion: v1alpha2.GroupVersion.String(), Kind: "AgentHarness"},
		ObjectMeta: metav1.ObjectMeta{
			Name:       name,
			Namespace:  namespace,
			Generation: 1,
			Finalizers: []string{agentHarnessFinalizer},
		},
		Spec: v1alpha2.AgentHarnessSpec{
			Runtime: v1alpha2.AgentHarnessRuntimeSubstrate,
			Backend: v1alpha2.AgentHarnessBackendOpenClaw,
			Substrate: &v1alpha2.AgentHarnessSubstrateSpec{
				GatewayToken: "token",
			},
		},
	}
}

func newDeletingSubstrateHarness(namespace, name string) *v1alpha2.AgentHarness {
	ah := newSubstrateHarness(namespace, name)
	now := metav1.Now()
	ah.DeletionTimestamp = &now
	return ah
}

func getAgentHarness(t *testing.T, kube client.Client, ah *v1alpha2.AgentHarness) *v1alpha2.AgentHarness {
	t.Helper()
	var latest v1alpha2.AgentHarness
	err := kube.Get(context.Background(), client.ObjectKeyFromObject(ah), &latest)
	if apierrors.IsNotFound(err) {
		t.Fatalf("AgentHarness %s unexpectedly not found", client.ObjectKeyFromObject(ah))
	}
	require.NoError(t, err)
	return &latest
}

func requireCondition(t *testing.T, ah *v1alpha2.AgentHarness, conditionType string, status metav1.ConditionStatus, reason string) {
	t.Helper()
	condition := meta.FindStatusCondition(ah.Status.Conditions, conditionType)
	require.NotNil(t, condition, "missing condition %s", conditionType)
	require.Equal(t, status, condition.Status, "condition %s status", conditionType)
	require.Equal(t, reason, condition.Reason, "condition %s reason", conditionType)
}
