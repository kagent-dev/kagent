package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	kagentfake "github.com/kagent-dev/kagent/go/api/clientset/versioned/fake"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	v2translator "github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/kube/controllers"
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestUnresolvedPoolReleasesAbandonedRevision(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}
	for _, failed := range []bool{false, true} {
		name := "preparing revision"
		if failed {
			name = "failed revision"
		}
		t.Run(name, func(t *testing.T) {
			ctx := t.Context()
			dsn := dbtest.StartT(ctx, t)
			dbtest.MigrateT(t, dsn, false)
			pool, err := database.Connect(ctx, &database.PostgresConfig{URL: dsn})
			require.NoError(t, err)
			t.Cleanup(pool.Close)
			store := database.NewClient(pool)
			collections, harnesses := newPreparationTestCollections(t, "gvisor")
			initial := collections.Reconciliations.List()[0]
			templates := &fakeActorTemplates{template: proto.CloneOf(initial.DesiredActorTemplate)}
			templates.template.Metadata.Uid = "gvisor-uid"
			templates.template.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{
				GoldenTag: &ateapipb.ObjectRef{Atespace: "ate-golden", Name: "golden"},
			}}
			reconciler := &Reconciler{collections: collections, templates: templates, store: store}
			require.NoError(t, reconciler.reconcileAgent(ctx, initial.ResourceName()))

			updatedHarness := harnesses.List()[0].DeepCopy()
			updatedHarness.Spec.Substrate.WorkerPoolRef.Name = "microvm"
			harnesses.UpdateObject(updatedHarness)
			waitFor(t, func() bool {
				return collections.Reconciliations.GetKey(initial.ResourceName()).RevisionID != initial.RevisionID
			})
			preparing := collections.Reconciliations.GetKey(initial.ResourceName())
			templates.template = nil
			require.NoError(t, reconciler.reconcileAgent(ctx, initial.ResourceName()))
			if failed {
				templates.template = proto.CloneOf(templates.template)
				templates.template.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{ErrorMessage: "snapshot failed"}}
				require.NoError(t, reconciler.reconcileAgent(ctx, initial.ResourceName()))
				waitFor(t, func() bool { return collections.Reconciliations.GetKey(initial.ResourceName()).Failure != nil })
			}
			unreferenced, err := store.ListUnreferencedRuntimeRevisions(ctx)
			require.NoError(t, err)
			require.Empty(t, unreferenced)

			updatedHarness = updatedHarness.DeepCopy()
			updatedHarness.Spec.Substrate.WorkerPoolRef.Name = "missing"
			harnesses.UpdateObject(updatedHarness)
			waitFor(t, func() bool {
				state := collections.Reconciliations.GetKey(initial.ResourceName())
				return state.Failure != nil && state.Failure.Reason == "WorkerPoolNotFound"
			})
			unresolved := collections.Reconciliations.GetKey(initial.ResourceName())
			require.True(t, unresolved.RevisionID.IsZero())
			for range 2 {
				require.NoError(t, reconciler.reconcileAgent(ctx, initial.ResourceName()))
			}
			require.Empty(t, collections.AgentRuntimeObservations.List())
			require.Nil(t, unresolved.DesiredActorTemplate)
			require.Equal(t, preparing.DesiredActorTemplate.GetMetadata().GetName(), templates.template.GetMetadata().GetName(), "unresolved inputs must not create compute")

			// A delayed success for the abandoned preparation must not replace A.
			abandoned, err := store.GetRuntimeRevision(ctx, preparing.RevisionID.String())
			require.NoError(t, err)
			require.NoError(t, store.RecordRuntimeRevision(ctx, *abandoned, true))
			unreferenced, err = store.ListUnreferencedRuntimeRevisions(ctx)
			require.NoError(t, err)
			require.Len(t, unreferenced, 1)
			require.Equal(t, preparing.RevisionID.String(), unreferenced[0].Revision)
			retained, err := store.BeginRuntimeRevisionDeletion(ctx, initial.RevisionID.String())
			require.NoError(t, err)
			require.Nil(t, retained, "last-successful A must remain protected without any instances")

			instance, _, err := store.CreateAgentInstance(ctx, &apiv1alpha1.AgentInstance{
				Id: uuid.NewString(), Creator: "alice",
				Agent: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"},
			}, "last-good-instance")
			require.NoError(t, err)
			require.Equal(t, initial.RevisionID.String(), instance.GetPreparedRevision())

			require.NoError(t, NewRuntimeRevisionGC(store, templates).collect(ctx, abandoned.Revision))
			require.Nil(t, templates.template)
			_, err = store.GetRuntimeRevision(ctx, abandoned.Revision)
			require.ErrorIs(t, err, database.ErrNotFound)
			_, err = store.GetRuntimeRevision(ctx, initial.RevisionID.String())
			require.NoError(t, err)

			updatedHarness = updatedHarness.DeepCopy()
			updatedHarness.Spec.Substrate.WorkerPoolRef.Name = "microvm"
			harnesses.UpdateObject(updatedHarness)
			waitFor(t, func() bool {
				state := collections.Reconciliations.GetKey(initial.ResourceName())
				return state.Failure == nil && state.RevisionID == preparing.RevisionID
			})
			require.NoError(t, reconciler.reconcileAgent(ctx, initial.ResourceName()), "restoring valid inputs must prepare the collected revision again")
			_, err = store.GetRuntimeRevision(ctx, abandoned.Revision)
			require.NoError(t, err)
		})
	}
}

func TestPreparationErrorsPublishStatusAndRecover(t *testing.T) {
	for _, test := range []struct {
		name      string
		templates fakeActorTemplates
		store     fakeRuntimeRevisionStore
		code      codes.Code
	}{
		{name: "missing sandbox config", templates: fakeActorTemplates{createErr: status.Error(codes.FailedPrecondition, `SandboxConfig "microvm" not found`)}, code: codes.FailedPrecondition},
		{name: "wrong sandbox class", templates: fakeActorTemplates{createErr: status.Error(codes.FailedPrecondition, `SandboxConfig "microvm" has class "gvisor" but sandbox_config.sandbox_class is "microvm"`)}, code: codes.FailedPrecondition},
		{name: "sensitive creation error", templates: fakeActorTemplates{createErr: status.Error(codes.FailedPrecondition, "private-backend-detail")}, code: codes.FailedPrecondition},
		{name: "get failed", templates: fakeActorTemplates{getErr: status.Error(codes.Unavailable, "private-endpoint")}, code: codes.Unavailable},
		{name: "atespace failed", templates: fakeActorTemplates{ensureErr: status.Error(codes.PermissionDenied, "private-credential")}, code: codes.PermissionDenied},
		{name: "pair store failed", store: fakeRuntimeRevisionStore{pairErr: errors.New("private-database-connection")}, code: codes.Unknown},
		{name: "revision store failed", store: fakeRuntimeRevisionStore{revisionErr: errors.New("private-database-connection")}, code: codes.Unknown},
	} {
		t.Run(test.name, func(t *testing.T) {
			collections, _ := newPreparationTestCollections(t, "microvm")
			initial := collections.Reconciliations.List()[0]
			statusClient := kagentfake.NewSimpleClientset(initial.Agent.DeepCopy()).ApiV1alpha3()
			reconciler := &Reconciler{collections: collections, templates: &test.templates, store: &test.store, status: statusClient}
			err := reconciler.reconcileAgent(t.Context(), initial.ResourceName())
			require.Error(t, err)
			require.Equal(t, test.code, status.Code(err))
			waitFor(t, func() bool {
				state := collections.Reconciliations.GetKey(initial.ResourceName())
				return state.Failure != nil && state.Failure.Retryable
			})
			waitFor(t, func() bool {
				updates := collections.AgentStatuses.List()
				if len(updates) != 1 {
					return false
				}
				ready := apimeta.FindStatusCondition(updates[0].Status.Conditions, kagentv1alpha3.AgentConditionReady)
				return ready != nil && ready.Reason == "RuntimePreparationFailed"
			})
			require.NoError(t, reconciler.reconcileAgentStatus(t.Context(), "team-a/assistant"))
			published, err := statusClient.Agents("team-a").Get(t.Context(), "assistant", metav1.GetOptions{})
			require.NoError(t, err)
			ready := apimeta.FindStatusCondition(published.Status.Conditions, kagentv1alpha3.AgentConditionReady)
			require.Equal(t, metav1.ConditionFalse, ready.Status)
			require.Equal(t, "RuntimePreparationFailed", ready.Reason)
			require.Contains(t, ready.Message, test.code.String())
			require.NotContains(t, ready.Message, "private-")
			if test.code == codes.FailedPrecondition {
				require.Contains(t, ready.Message, `SandboxConfig "microvm"`)
				require.Contains(t, ready.Message, `spec.sandboxClass="microvm"`)
			}
			observation := collections.AgentRuntimeObservations.GetKey(initial.ResourceName())
			require.Nil(t, observation.Template)
			require.Equal(t, initial.RevisionID, observation.RevisionID)
			require.Error(t, reconciler.reconcileAgent(t.Context(), initial.ResourceName()), "publishing a failure must not prevent the next retry")

			test.templates.ensureErr, test.templates.getErr, test.templates.createErr = nil, nil, nil
			test.store.pairErr, test.store.revisionErr = nil, nil
			require.NoError(t, reconciler.reconcileAgent(t.Context(), initial.ResourceName()))
			waitFor(t, func() bool {
				state := collections.Reconciliations.GetKey(initial.ResourceName())
				return state.Failure == nil && state.ObservedActorTemplate != nil
			})
			require.Nil(t, collections.AgentRuntimeObservations.GetKey(initial.ResourceName()).Failure)
			test.templates.template = proto.CloneOf(test.templates.template)
			test.templates.template.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{
				GoldenTag: &ateapipb.ObjectRef{Atespace: "ate-golden", Name: "golden"},
			}}
			require.NoError(t, reconciler.reconcileAgent(t.Context(), initial.ResourceName()))
			waitFor(t, func() bool {
				harnessStatus := collections.AgentStatuses.List()[0].Status
				return apimeta.IsStatusConditionTrue(harnessStatus.Conditions, kagentv1alpha3.AgentConditionReady) &&
					harnessStatus.LatestSuccessfulRevision == initial.RevisionID.String()
			})
		})
	}
}

func TestRuntimePreparationFailureSanitizesErrors(t *testing.T) {
	for _, test := range []struct {
		name       string
		class      atev1alpha1.SandboxClass
		configName string
		wantClass  string
	}{
		{name: "default", configName: "gvisor-default", wantClass: "gvisor"},
		{name: "gvisor", class: atev1alpha1.SandboxClassGvisor, configName: "gvisor-default", wantClass: "gvisor"},
		{name: "microvm", class: atev1alpha1.SandboxClassMicroVM, configName: "microvm", wantClass: "microvm"},
	} {
		t.Run(test.name, func(t *testing.T) {
			failure := runtimePreparationFailure(status.Error(codes.FailedPrecondition, "private-backend-detail"), test.configName, test.class)
			require.Equal(t, kagentv1alpha3.AgentConditionReady, failure.Condition)
			require.True(t, failure.Retryable)
			require.Contains(t, failure.Message, `SandboxConfig "`+test.configName+`"`)
			require.Contains(t, failure.Message, `spec.sandboxClass="`+test.wantClass+`"`)
			require.NotContains(t, failure.Message, "private-backend-detail")
		})
	}
}

func TestPreparationFailurePollingAndRevisionIsolation(t *testing.T) {
	collections, harnesses := newPreparationTestCollections(t, "microvm")
	initial := collections.Reconciliations.List()[0]
	templates := &fakeActorTemplates{createErr: status.Error(codes.FailedPrecondition, `SandboxConfig "microvm" not found`)}
	reconciler := &Reconciler{collections: collections, templates: templates, store: &fakeRuntimeRevisionStore{}}
	require.Error(t, reconciler.reconcileAgent(t.Context(), initial.ResourceName()))
	waitFor(t, func() bool { return collections.Reconciliations.GetKey(initial.ResourceName()).Failure != nil })

	pollCtx, cancel := context.WithCancel(t.Context())
	defer cancel()
	queued := make(chan string, 1)
	reconciler.agents = controllers.NewQueue("test-preparation-failure", controllers.WithGenericReconciler(func(item any) error {
		select {
		case queued <- item.(string):
		case <-pollCtx.Done():
		}
		return nil
	}))
	go reconciler.agents.Run(pollCtx.Done())
	go reconciler.pollPendingTemplates(pollCtx.Done())
	t.Cleanup(func() {
		cancel()
		require.NoError(t, reconciler.agents.WaitForClose(time.Second))
	})
	for range 2 {
		select {
		case key := <-queued:
			require.Equal(t, initial.ResourceName(), key)
			require.Error(t, reconciler.reconcileAgent(t.Context(), key))
		case <-time.After(5 * time.Second):
			t.Fatal("failed preparation was not requeued after its graph event")
		}
	}
	cancel()

	updatedHarness := harnesses.List()[0].DeepCopy()
	updatedHarness.Spec.Workload.Args = []string{"changed"}
	harnesses.UpdateObject(updatedHarness)
	waitFor(t, func() bool {
		state := collections.Reconciliations.GetKey(initial.ResourceName())
		return state.RevisionID != initial.RevisionID && state.Failure == nil
	})
	templates.createErr = nil
	require.NoError(t, reconciler.reconcileAgent(t.Context(), initial.ResourceName()))
	observation := collections.AgentRuntimeObservations.GetKey(initial.ResourceName())
	require.NotEqual(t, initial.RevisionID, observation.RevisionID)
	require.Nil(t, observation.Failure, "a new revision must not inherit an older preparation failure")
	harnesses.DeleteObject("team-a/byo")
	waitFor(t, func() bool { return collections.Reconciliations.GetKey(initial.ResourceName()).Failure != nil })
	require.NoError(t, reconciler.reconcileAgent(t.Context(), initial.ResourceName()))
	require.Empty(t, collections.AgentRuntimeObservations.List())
}

func newPreparationTestCollections(t *testing.T, workerPool string) (Collections, krt.StaticCollection[*kagentv1alpha3.Harness]) {
	t.Helper()
	opts := krt.NewOptionsBuilder(t.Context().Done(), "test-preparation", nil)
	template := &kagentv1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", UID: "template-uid"}}
	runtimeHarness := harness("team-a", "byo", nil)
	runtimeHarness.UID = "harness-uid"
	runtimeHarness.Spec.BYO = &kagentv1alpha3.BYOHarness{}
	runtimeHarness.Spec.Workload.Image = "example.com/agent@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	runtimeHarness.Spec.Workload.Command = []string{"/agent"}
	runtimeHarness.Spec.Substrate = kagentv1alpha3.HarnessSubstratePolicy{
		WorkerPoolRef: corev1.LocalObjectReference{Name: workerPool}, SnapshotPolicy: kagentv1alpha3.HarnessSnapshotPolicy{Location: "snapshots"},
	}
	harnesses := krt.NewStaticCollection(nil, []*kagentv1alpha3.Harness{runtimeHarness}, opts.WithName("Harnesses")...)
	mock := krttest.NewMock(t, []any{
		template,
		&atev1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "gvisor"}, Spec: atev1alpha1.WorkerPoolSpec{SandboxClass: atev1alpha1.SandboxClassGvisor}},
		&atev1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "microvm"}, Spec: atev1alpha1.WorkerPoolSpec{SandboxClass: atev1alpha1.SandboxClassMicroVM}},
	})
	collections := Collections{
		AgentTemplates:           krttest.GetMockCollection[*kagentv1alpha3.AgentTemplate](mock),
		Harnesses:                harnesses,
		ResolvedModelConfigs:     krttest.GetMockCollection[v2translator.ResolvedModelConfig](mock),
		RemoteMCPServers:         krttest.GetMockCollection[*kagentv1alpha3.RemoteMCPServer](mock),
		ConfigMaps:               krttest.GetMockCollection[*corev1.ConfigMap](mock),
		Secrets:                  krttest.GetMockCollection[*corev1.Secret](mock),
		WorkerPools:              krttest.GetMockCollection[*atev1alpha1.WorkerPool](mock),
		AgentRuntimeObservations: krt.NewStaticCollection[AgentRuntimeObservation](nil, nil, opts.WithName("AgentRuntimeObservations")...),
	}
	collections.Agents = krt.NewStaticCollection(nil, []*kagentv1alpha3.Agent{testAgent(template, runtimeHarness)}, opts.WithName("Agents")...)
	collections.Reconciliations = newAgentReconciliations(collections.Agents, v2translator.Collections{
		Harnesses: collections.Harnesses, AgentTemplates: collections.AgentTemplates, ResolvedModelConfigs: collections.ResolvedModelConfigs,
		RemoteMCPServers: collections.RemoteMCPServers, ConfigMaps: collections.ConfigMaps,
		Secrets: collections.Secrets, WorkerPools: collections.WorkerPools,
	}, collections.AgentRuntimeObservations, opts)
	collections.AgentStatuses = newAgentStatuses(collections.Agents, collections.Reconciliations, opts)
	waitFor(t, func() bool {
		states := collections.Reconciliations.List()
		return len(states) == 1 && states[0].Failure == nil && states[0].DesiredActorTemplate != nil
	})
	return collections, harnesses
}
