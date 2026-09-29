package controller

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/uuid"
	kagentfake "github.com/kagent-dev/kagent/go/api/clientset/versioned/fake"
	kagentclient "github.com/kagent-dev/kagent/go/api/clientset/versioned/typed/api/v1alpha3"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/internal/translator"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
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
			store := database.NewClient(pool, "public")
			collections, harnesses := newPreparationTestCollections(t, "gvisor")
			initial := collections.Reconciliations.List()[0]
			templates := &fakeActorTemplates{template: proto.CloneOf(initial.Target.ActorTemplate)}
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
				return collections.Reconciliations.GetKey(initial.ResourceName()).Target.RevisionID != initial.Target.RevisionID
			})
			preparing := collections.Reconciliations.GetKey(initial.ResourceName())
			templates.template = nil
			require.NoError(t, reconciler.reconcileAgent(ctx, initial.ResourceName()))
			if failed {
				templates.template = proto.CloneOf(templates.template)
				templates.template.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{ErrorMessage: "snapshot failed"}}
				require.NoError(t, reconciler.reconcileAgent(ctx, initial.ResourceName()))
				waitFor(t, func() bool {
					return collections.Reconciliations.GetKey(initial.ResourceName()).PreparationFailure != nil
				})
			}
			unreferenced, err := store.ListUnreferencedRuntimeRevisions(ctx)
			require.NoError(t, err)
			require.Empty(t, unreferenced)

			updatedHarness = updatedHarness.DeepCopy()
			updatedHarness.Spec.Substrate.WorkerPoolRef.Name = "missing"
			harnesses.UpdateObject(updatedHarness)
			waitFor(t, func() bool {
				state := collections.Reconciliations.GetKey(initial.ResourceName())
				return state.CompilationFailure != nil && state.CompilationFailure.Reason == "WorkerPoolNotFound"
			})
			unresolved := collections.Reconciliations.GetKey(initial.ResourceName())
			require.Nil(t, unresolved.Target)
			for range 2 {
				require.NoError(t, reconciler.reconcileAgent(ctx, initial.ResourceName()))
			}
			require.Empty(t, collections.AgentRuntimeObservations.List())
			require.Empty(t, unresolved.desiredRevision())
			require.Equal(t, preparing.Target.ActorTemplate.GetMetadata().GetName(), templates.template.GetMetadata().GetName(), "unresolved inputs must not create compute")

			// A delayed success for the abandoned preparation must not replace A.
			abandoned, err := store.GetRuntimeRevision(ctx, preparing.Target.RevisionID.String())
			require.NoError(t, err)
			require.NoError(t, store.RecordRuntimeRevision(ctx, *abandoned, true))
			unreferenced, err = store.ListUnreferencedRuntimeRevisions(ctx)
			require.NoError(t, err)
			require.Len(t, unreferenced, 1)
			require.Equal(t, preparing.Target.RevisionID.String(), unreferenced[0].Revision)
			retained, err := store.BeginRuntimeRevisionDeletion(ctx, initial.Target.RevisionID.String())
			require.NoError(t, err)
			require.Nil(t, retained, "last-successful A must remain protected without any sessions")

			session, _, err := store.CreateSession(ctx, &apiv1alpha1.Session{
				Id: uuid.NewString(), Creator: "alice",
				Agent: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"},
			}, "last-good-session")
			require.NoError(t, err)
			require.Equal(t, initial.Target.RevisionID.String(), session.GetPreparedRevision())

			collector, _ := newTestRuntimeRevisionGC(t, store, templates)
			require.NoError(t, collector.collect(ctx, abandoned.Revision))
			require.Nil(t, templates.template)
			_, err = store.GetRuntimeRevision(ctx, abandoned.Revision)
			require.ErrorIs(t, err, database.ErrNotFound)
			_, err = store.GetRuntimeRevision(ctx, initial.Target.RevisionID.String())
			require.NoError(t, err)

			updatedHarness = updatedHarness.DeepCopy()
			updatedHarness.Spec.Substrate.WorkerPoolRef.Name = "microvm"
			harnesses.UpdateObject(updatedHarness)
			waitFor(t, func() bool {
				state := collections.Reconciliations.GetKey(initial.ResourceName())
				return state.Target != nil && state.PreparationFailure == nil && state.Target.RevisionID == preparing.Target.RevisionID
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
		{name: "get invalid argument", templates: fakeActorTemplates{getErr: status.Error(codes.InvalidArgument, "private-endpoint")}, code: codes.InvalidArgument},
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
				return state.PreparationFailure != nil && state.PreparationFailure.Retryable
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
			require.Equal(t, initial.Target.RevisionID, observation.RevisionID)
			require.Error(t, reconciler.reconcileAgent(t.Context(), initial.ResourceName()), "publishing a failure must not prevent the next retry")

			test.templates.ensureErr, test.templates.getErr, test.templates.createErr = nil, nil, nil
			test.store.pairErr, test.store.revisionErr = nil, nil
			require.NoError(t, reconciler.reconcileAgent(t.Context(), initial.ResourceName()))
			waitFor(t, func() bool {
				state := collections.Reconciliations.GetKey(initial.ResourceName())
				return state.PreparationFailure == nil && state.ObservedActorTemplate != nil
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
					harnessStatus.LatestSuccessfulRevision == initial.Target.RevisionID.String()
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

func TestPreparationRevisionIsolation(t *testing.T) {
	collections, harnesses := newPreparationTestCollections(t, "microvm")
	initial := collections.Reconciliations.List()[0]
	templates := &fakeActorTemplates{createErr: status.Error(codes.FailedPrecondition, `SandboxConfig "microvm" not found`)}
	reconciler := &Reconciler{collections: collections, templates: templates, store: &fakeRuntimeRevisionStore{}}
	require.Error(t, reconciler.reconcileAgent(t.Context(), initial.ResourceName()))
	waitFor(t, func() bool {
		return collections.Reconciliations.GetKey(initial.ResourceName()).PreparationFailure != nil
	})

	updatedHarness := harnesses.List()[0].DeepCopy()
	updatedHarness.Spec.Workload.Args = []string{"changed"}
	harnesses.UpdateObject(updatedHarness)
	waitFor(t, func() bool {
		state := collections.Reconciliations.GetKey(initial.ResourceName())
		return state.Target.RevisionID != initial.Target.RevisionID && state.PreparationFailure == nil
	})
	templates.createErr = nil
	require.NoError(t, reconciler.reconcileAgent(t.Context(), initial.ResourceName()))
	observation := collections.AgentRuntimeObservations.GetKey(initial.ResourceName())
	require.NotEqual(t, initial.Target.RevisionID, observation.RevisionID)
	require.Nil(t, observation.Failure, "a new revision must not inherit an older preparation failure")
	harnesses.DeleteObject("team-a/byo")
	waitFor(t, func() bool {
		return collections.Reconciliations.GetKey(initial.ResourceName()).CompilationFailure != nil
	})
	require.NoError(t, reconciler.reconcileAgent(t.Context(), initial.ResourceName()))
	require.Empty(t, collections.AgentRuntimeObservations.List())
}

func TestPreparationQueueAndPollerBackoff(t *testing.T) {
	for _, test := range []struct {
		name      string
		templates fakeActorTemplates
		store     fakeRuntimeRevisionStore
	}{
		{name: "missing sandbox config", templates: fakeActorTemplates{createErr: status.Error(codes.FailedPrecondition, "missing config")}},
		{name: "revision being collected", store: fakeRuntimeRevisionStore{pairErr: database.ErrObjectDeleting}},
		{name: "persistence unavailable", store: fakeRuntimeRevisionStore{revisionErr: errors.New("database unavailable")}},
	} {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				collections, _ := newPreparationTestCollections(t, "microvm")
				initial := collections.Reconciliations.List()[0]
				statusClient := kagentfake.NewSimpleClientset(initial.Agent.DeepCopy()).ApiV1alpha3()
				backend := &preparationTestBackend{templates: &test.templates, store: &test.store}
				r := newReconciler(collections, backend, backend, statusClient)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				go r.Run(ctx.Done())
				synctest.Wait()
				backend.inspect(func() {
					require.Equal(t, 1, test.store.pairCalls, "publishing a failure must not immediately requeue it")
				})
				time.Sleep(10 * time.Second)
				synctest.Wait()
				backend.inspect(func() {
					require.Equal(t, 4, test.store.pairCalls, "only exponential backoff may retry failures")
				})
				time.Sleep(5 * time.Minute)
				synctest.Wait()
				var before int
				backend.inspect(func() {
					require.Greater(t, test.store.pairCalls, 10, "retries must survive the former attempt budget")
					before = test.store.pairCalls
				})
				time.Sleep(30 * time.Second)
				synctest.Wait()
				backend.inspect(func() {
					require.Equal(t, before+1, test.store.pairCalls, "the poller must not bypass capped backoff")
				})
				failed := collections.Reconciliations.GetKey(initial.ResourceName())
				require.Nil(t, failed.CompilationFailure)
				require.NotNil(t, failed.Target)
				require.NotNil(t, failed.PreparationFailure)

				// Recover without changing any Kubernetes input or manually queuing work.
				backend.inspect(func() {
					test.templates.createErr = nil
					test.store.pairErr, test.store.revisionErr = nil, nil
				})
				time.Sleep(30 * time.Second)
				synctest.Wait()
				require.Nil(t, collections.Reconciliations.GetKey(initial.ResourceName()).PreparationFailure)
				backend.inspect(func() {
					require.NotNil(t, test.templates.template)
					require.False(t, test.store.markedSuccessful)
					test.templates.template = proto.CloneOf(test.templates.template)
					test.templates.template.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{
						GoldenTag: &ateapipb.ObjectRef{Atespace: "ate-golden", Name: "golden"},
					}}
				})
				time.Sleep(time.Second)
				synctest.Wait()
				backend.inspect(func() {
					require.True(t, test.store.markedSuccessful, "polling must still observe pending golden snapshots")
					before = test.store.pairCalls
				})
				published, err := statusClient.Agents(initial.Agent.Namespace).Get(t.Context(), initial.Agent.Name, metav1.GetOptions{})
				require.NoError(t, err)
				require.True(t, apimeta.IsStatusConditionTrue(published.Status.Conditions, kagentv1alpha3.AgentConditionReady))
				time.Sleep(time.Minute)
				synctest.Wait()
				backend.inspect(func() {
					require.Equal(t, before, test.store.pairCalls, "a ready revision needs no more polling")
				})
				cancel()
				synctest.Wait()
				require.NoError(t, r.agents.WaitForClose(time.Second))
			})
		})
	}
}

// Waiting for quiescence synchronizes worker writes with test reads, but test
// mutations also need synchronization with the next timer-driven attempt.
type preparationTestBackend struct {
	mu        sync.Mutex
	templates *fakeActorTemplates
	store     *fakeRuntimeRevisionStore
}

var _ actorTemplateClient = (*preparationTestBackend)(nil)
var _ runtimeRevisionStore = (*preparationTestBackend)(nil)

func (p *preparationTestBackend) inspect(f func()) {
	p.mu.Lock()
	defer p.mu.Unlock()
	f()
}

func (p *preparationTestBackend) EnsureAtespace(ctx context.Context, atespace string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.templates.EnsureAtespace(ctx, atespace)
}

func (p *preparationTestBackend) GetActorTemplate(ctx context.Context, atespace, name string) (*ateapipb.ActorTemplate, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.templates.GetActorTemplate(ctx, atespace, name)
}

func (p *preparationTestBackend) CreateActorTemplate(ctx context.Context, template *ateapipb.ActorTemplate) (*ateapipb.ActorTemplate, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.templates.CreateActorTemplate(ctx, template)
}

func (p *preparationTestBackend) DeleteActorTemplate(ctx context.Context, atespace, name string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.templates.DeleteActorTemplate(ctx, atespace, name)
}

func (p *preparationTestBackend) UpsertAgentDefinition(ctx context.Context, definition database.AgentDefinition) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.store.UpsertAgentDefinition(ctx, definition)
}

func (p *preparationTestBackend) RecordRuntimeRevision(ctx context.Context, revision database.RuntimeRevision, ready bool) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.store.RecordRuntimeRevision(ctx, revision, ready)
}

func (p *preparationTestBackend) RetireAgentIdentities(ctx context.Context, namespace, name string, except *database.AgentDefinition) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.store.RetireAgentIdentities(ctx, namespace, name, except)
}

func TestPreparationDesiredChangesBypassBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		collections, harnesses := newPreparationTestCollections(t, "microvm")
		initial := collections.Reconciliations.List()[0]
		templates := &fakeActorTemplates{createErr: status.Error(codes.FailedPrecondition, "missing config")}
		store := &fakeRuntimeRevisionStore{}
		r := newReconciler(collections, templates, store, kagentfake.NewSimpleClientset(initial.Agent.DeepCopy()).ApiV1alpha3())
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		go r.Run(ctx.Done())
		synctest.Wait()
		time.Sleep(10 * time.Second)
		synctest.Wait()
		require.Equal(t, 4, store.pairCalls)
		published, err := r.status.Agents(initial.Agent.Namespace).Get(t.Context(), initial.Agent.Name, metav1.GetOptions{})
		require.NoError(t, err)
		agents := collections.Agents.(krt.StaticCollection[*kagentv1alpha3.Agent])
		agents.UpdateObject(published)
		synctest.Wait()
		require.Equal(t, 4, store.pairCalls, "status feedback must not bypass backoff")
		templates.createErr = nil
		updated := harnesses.List()[0].DeepCopy()
		updated.Spec.Workload.Args = []string{"changed"}
		harnesses.UpdateObject(updated)
		synctest.Wait()
		require.Equal(t, 5, store.pairCalls, "a new desired revision must prepare immediately")
		require.NotEqual(t, initial.Target.RevisionID.String(), store.pair.DesiredRevision)
		require.Nil(t, collections.Reconciliations.GetKey(initial.ResourceName()).PreparationFailure)

		harnesses.DeleteObject("team-a/byo")
		synctest.Wait()
		require.Empty(t, store.pair.DesiredRevision, "unresolved inputs must clear the desired runtime immediately")
		require.Empty(t, collections.AgentRuntimeObservations.List())
		agents.DeleteObject(initial.ResourceName())
		synctest.Wait()
		require.Equal(t, initial.ResourceName(), store.retired, "deletion must retire the Agent immediately")
		cancel()
		synctest.Wait()
	})
}

func TestPreparationTerminalFailures(t *testing.T) {
	for _, reason := range []string{"ActorTemplateRejected", "ActorTemplateConflict", "ActorTemplateFailed"} {
		t.Run(reason, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				collections, harnesses := newPreparationTestCollections(t, "microvm")
				initial := collections.Reconciliations.List()[0]
				templates := &fakeActorTemplates{}
				switch reason {
				case "ActorTemplateRejected":
					templates.createErr = status.Error(codes.InvalidArgument, "private-config-value")
				case "ActorTemplateConflict":
					templates.template = proto.CloneOf(initial.Target.ActorTemplate)
					templates.template.Containers[0].Args = []string{"unexpected"}
				case "ActorTemplateFailed":
					templates.template = proto.CloneOf(initial.Target.ActorTemplate)
					templates.template.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{ErrorMessage: "snapshot failed"}}
				}
				store := &fakeRuntimeRevisionStore{}
				statusClient := kagentfake.NewSimpleClientset(initial.Agent.DeepCopy()).ApiV1alpha3()
				r := newReconciler(collections, templates, store, statusClient)
				ctx, cancel := context.WithCancel(t.Context())
				defer cancel()
				go r.Run(ctx.Done())
				synctest.Wait()
				require.Equal(t, 1, store.pairCalls)
				state := collections.Reconciliations.GetKey(initial.ResourceName())
				require.Nil(t, state.CompilationFailure)
				require.NotNil(t, state.Target)
				require.Equal(t, reason, state.PreparationFailure.Reason)
				published, err := statusClient.Agents(initial.Agent.Namespace).Get(t.Context(), initial.Agent.Name, metav1.GetOptions{})
				require.NoError(t, err)
				ready := apimeta.FindStatusCondition(published.Status.Conditions, kagentv1alpha3.AgentConditionReady)
				require.Equal(t, reason, ready.Reason)
				require.NotContains(t, ready.Message, "private-config-value")
				time.Sleep(5 * time.Minute)
				synctest.Wait()
				require.Equal(t, 1, store.pairCalls, "neither the queue nor the poller may retry terminal failures")

				store.pairErr = errors.New("database unavailable")
				require.ErrorIs(t, r.reconcileAgent(t.Context(), initial.ResourceName()), store.pairErr)
				synctest.Wait()
				require.Equal(t, state.PreparationFailure, collections.Reconciliations.GetKey(initial.ResourceName()).PreparationFailure,
					"a storage failure must preserve the terminal preparation diagnostic")
				store.pairErr = nil
				templates.getErr = errors.New("Substrate must not be called after a terminal failure")
				require.NoError(t, r.reconcileAgent(t.Context(), initial.ResourceName()))
				templates.getErr = nil

				templates.createErr, templates.template = nil, nil
				updated := harnesses.List()[0].DeepCopy()
				updated.Spec.Workload.Args = []string{"changed"}
				harnesses.UpdateObject(updated)
				synctest.Wait()
				require.Equal(t, 4, store.pairCalls)
				require.NotNil(t, templates.template, "a changed revision must prepare without the old failure")
				require.Nil(t, collections.Reconciliations.GetKey(initial.ResourceName()).PreparationFailure)
				cancel()
				synctest.Wait()
			})
		})
	}
}

const unattributedGoldenCrash = "GoldenActorCrashed: golden actor crashed before its snapshot was taken"

func crashedGoldenBoot(message string) *ateapipb.ActorTemplateStatus {
	return &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{ErrorMessage: message}}
}

func readyGoldenBoot() *ateapipb.ActorTemplateStatus {
	return &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{
		GoldenTag: &ateapipb.ObjectRef{Atespace: "ate-golden", Name: "golden"},
	}}
}

// goldenBootTest runs the reconciler over the fakes inside a synctest bubble,
// so the poller's ticks and the retry deadlines share one clock.
type goldenBootTest struct {
	t            *testing.T
	collections  Collections
	harnesses    krt.StaticCollection[*kagentv1alpha3.Harness]
	backend      *preparationTestBackend
	templates    *fakeActorTemplates
	store        *fakeRuntimeRevisionStore
	reconciler   *Reconciler
	initial      AgentReconciliation
	statusClient kagentclient.ApiV1alpha3Interface
}

func startGoldenBootTest(t *testing.T) *goldenBootTest {
	t.Helper()
	collections, harnesses := newPreparationTestCollections(t, "microvm")
	initial := collections.Reconciliations.List()[0]
	g := &goldenBootTest{
		t: t, collections: collections, harnesses: harnesses, initial: initial,
		templates: &fakeActorTemplates{}, store: &fakeRuntimeRevisionStore{},
		statusClient: kagentfake.NewSimpleClientset(initial.Agent.DeepCopy()).ApiV1alpha3(),
	}
	g.backend = &preparationTestBackend{templates: g.templates, store: g.store}
	g.reconciler = newReconciler(collections, g.backend, g.backend, g.statusClient)
	ctx, cancel := context.WithCancel(t.Context())
	t.Cleanup(func() {
		cancel()
		synctest.Wait()
	})
	go g.reconciler.Run(ctx.Done())
	synctest.Wait()
	g.backend.inspect(func() {
		require.Equal(t, 1, g.templates.created)
		require.Equal(t, "actor-uid", g.store.revision.ActorTemplateUID)
		require.False(t, g.store.markedSuccessful)
	})
	return g
}

// setGoldenBoot replaces the observed template's status the way Substrate's
// template reconciler writes it, and lets the next poller tick observe it.
func (g *goldenBootTest) setGoldenBoot(status *ateapipb.ActorTemplateStatus) {
	g.t.Helper()
	g.backend.inspect(func() {
		require.NotNil(g.t, g.templates.template)
		g.templates.template = proto.CloneOf(g.templates.template)
		g.templates.template.Status = status
	})
	time.Sleep(time.Second)
	synctest.Wait()
}

func (g *goldenBootTest) observation() AgentRuntimeObservation {
	g.t.Helper()
	observed := g.collections.AgentRuntimeObservations.GetKey(g.initial.ResourceName())
	require.NotNil(g.t, observed, "the Agent must stay observed")
	return *observed
}

func (g *goldenBootTest) readyCondition() metav1.Condition {
	g.t.Helper()
	published, err := g.statusClient.Agents(g.initial.Agent.Namespace).Get(g.t.Context(), g.initial.Agent.Name, metav1.GetOptions{})
	require.NoError(g.t, err)
	ready := apimeta.FindStatusCondition(published.Status.Conditions, kagentv1alpha3.AgentConditionReady)
	require.NotNil(g.t, ready)
	return *ready
}

func TestGoldenBootCrashIsStartedOver(t *testing.T) {
	for name, message := range map[string]string{
		"worker replaced under the golden actor": unattributedGoldenCrash,
		"registry refused the image pull": "GoldenActorCrashed: actor ate-golden/assistant crashed: CallAteletRestore failed: " +
			"while creating \"agent\" OCI bundle: FAILED_GET_EXTERNAL_OBJECT: 503 Service Unavailable",
	} {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				g := startGoldenBootTest(t)
				g.setGoldenBoot(crashedGoldenBoot(message))
				observed := g.observation()
				require.Equal(t, 0, observed.GoldenBootRetries)
				require.Equal(t, time.Now().Add(goldenBootRetryBaseDelay), observed.RetryGoldenBootAt, "the first sight of a crash schedules its retry")
				g.backend.inspect(func() { require.Empty(t, g.templates.deleted, "a crash is not started over before its backoff") })
				ready := g.readyCondition()
				require.Equal(t, metav1.ConditionFalse, ready.Status)
				require.Equal(t, "ActorTemplateRetrying", ready.Reason)
				require.Equal(t, "golden boot 1 of 6 failed ("+message+"); starting it over", ready.Message)

				time.Sleep(goldenBootRetryBaseDelay - time.Second)
				synctest.Wait()
				g.backend.inspect(func() { require.Empty(t, g.templates.deleted) })
				require.Equal(t, observed.RetryGoldenBootAt, g.observation().RetryGoldenBootAt, "the deadline holds across observations")

				time.Sleep(time.Second)
				synctest.Wait()
				g.backend.inspect(func() {
					require.Equal(t, []string{"actor-uid"}, g.templates.deleted, "the crashed template and its golden actor are removed")
					require.Equal(t, 2, g.templates.created, "the desired template is created again")
					require.Equal(t, "actor-uid-2", g.templates.template.GetMetadata().GetUid())
					require.Nil(t, g.templates.template.GetStatus(), "the new boot starts clean")
					require.Equal(t, "actor-uid-2", g.store.revision.ActorTemplateUID, "the revision follows the new template so GC can tell them apart")
					require.False(t, g.store.markedSuccessful)
				})
				observed = g.observation()
				require.Equal(t, 1, observed.GoldenBootRetries)
				require.True(t, observed.RetryGoldenBootAt.IsZero())
				require.Equal(t, "ActorTemplatePending", g.readyCondition().Reason)

				g.setGoldenBoot(readyGoldenBoot())
				g.backend.inspect(func() { require.True(t, g.store.markedSuccessful) })
				require.Equal(t, 1, g.observation().GoldenBootRetries, "the count stays with the revision")
				ready = g.readyCondition()
				require.Equal(t, metav1.ConditionTrue, ready.Status)
				require.Equal(t, "Ready", ready.Reason)
			})
		})
	}
}

func TestGoldenBootRetryBudget(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := startGoldenBootTest(t)
		var delays []time.Duration
		for retry := range maxGoldenBootRetries {
			g.setGoldenBoot(crashedGoldenBoot(unattributedGoldenCrash))
			observed := g.observation()
			require.Equal(t, retry, observed.GoldenBootRetries)
			delays = append(delays, time.Until(observed.RetryGoldenBootAt))
			require.Equal(t, "ActorTemplateRetrying", g.readyCondition().Reason)
			time.Sleep(time.Until(observed.RetryGoldenBootAt))
			synctest.Wait()
			g.backend.inspect(func() {
				require.Len(t, g.templates.deleted, retry+1)
				require.Equal(t, retry+2, g.templates.created)
			})
		}
		require.Equal(t, []time.Duration{20 * time.Second, 40 * time.Second, 80 * time.Second, 2 * time.Minute, 2 * time.Minute}, delays, "the backoff doubles up to its cap")

		g.setGoldenBoot(crashedGoldenBoot(unattributedGoldenCrash))
		observed := g.observation()
		require.Equal(t, maxGoldenBootRetries, observed.GoldenBootRetries)
		require.True(t, observed.RetryGoldenBootAt.IsZero())
		state := g.collections.Reconciliations.GetKey(g.initial.ResourceName())
		require.Nil(t, state.GoldenBootRetry)
		require.Equal(t, "ActorTemplateFailed", state.PreparationFailure.Reason)
		require.Equal(t, "golden boot 6 of 6 failed ("+unattributedGoldenCrash+"); no retries left", state.PreparationFailure.Message)
		require.False(t, state.PreparationFailure.Retryable)
		ready := g.readyCondition()
		require.Equal(t, metav1.ConditionFalse, ready.Status)
		require.Equal(t, "ActorTemplateFailed", ready.Reason)
		var reconciles int
		g.backend.inspect(func() { reconciles = g.store.pairCalls })
		time.Sleep(time.Hour)
		synctest.Wait()
		g.backend.inspect(func() {
			require.Len(t, g.templates.deleted, maxGoldenBootRetries, "the budget is spent: the last crash stands")
			require.Equal(t, reconciles, g.store.pairCalls, "neither the queue nor the poller retries a spent budget")
		})
	})
}

// An invalid template and an actor someone else paused or deleted are final at
// once: no retry repairs them.
func TestGoldenBootFailuresWithoutACrashStayFinal(t *testing.T) {
	for _, message := range []string{
		"GoldenActorInvalid: rpc error: code = InvalidArgument desc = containers[0].image: unsupported reference",
		"GoldenActorUnexpectedState: golden actor in state ACTOR_STATE_PAUSED",
	} {
		synctest.Test(t, func(t *testing.T) {
			g := startGoldenBootTest(t)
			g.setGoldenBoot(crashedGoldenBoot(message))
			time.Sleep(time.Hour)
			synctest.Wait()
			g.backend.inspect(func() {
				require.Empty(t, g.templates.deleted, "a failure no retry can repair is not started over: %s", message)
				require.Equal(t, 1, g.templates.created)
			})
			observed := g.observation()
			require.Equal(t, 0, observed.GoldenBootRetries)
			require.True(t, observed.RetryGoldenBootAt.IsZero())
			state := g.collections.Reconciliations.GetKey(g.initial.ResourceName())
			require.Nil(t, state.GoldenBootRetry)
			require.Equal(t, "ActorTemplateFailed", state.PreparationFailure.Reason)
			require.Equal(t, message, state.PreparationFailure.Message)
			require.Equal(t, "ActorTemplateFailed", g.readyCondition().Reason)
		})
	}
}

// A crashed template replaced outside the reconciler's own restart (a restart
// that failed after its delete, an operator's deletion) still counts against
// the revision's budget; a new revision starts with a fresh one.
func TestGoldenBootReplacedTemplateCountsAgainstTheRevision(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := startGoldenBootTest(t)
		g.setGoldenBoot(crashedGoldenBoot(unattributedGoldenCrash))
		g.backend.inspect(func() { g.templates.template = nil })
		time.Sleep(time.Second)
		synctest.Wait()
		g.backend.inspect(func() {
			require.Empty(t, g.templates.deleted)
			require.Equal(t, 2, g.templates.created)
		})
		observed := g.observation()
		require.Equal(t, 1, observed.GoldenBootRetries)
		require.True(t, observed.RetryGoldenBootAt.IsZero())

		g.backend.inspect(func() { g.templates.template = nil })
		updated := g.harnesses.List()[0].DeepCopy()
		updated.Spec.Workload.Args = []string{"changed"}
		g.harnesses.UpdateObject(updated)
		synctest.Wait()
		fresh := g.observation()
		require.NotEqual(t, observed.RevisionID, fresh.RevisionID)
		require.Equal(t, 0, fresh.GoldenBootRetries, "a new revision brings a fresh budget")
		g.backend.inspect(func() { require.Equal(t, 3, g.templates.created) })
	})
}

// A restart Substrate refuses keeps the crashed template, its deadline and the
// count observed, so the queue's backoff retries the restart itself.
func TestGoldenBootRestartFailureIsRetried(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		g := startGoldenBootTest(t)
		g.setGoldenBoot(crashedGoldenBoot(unattributedGoldenCrash))
		deadline := g.observation().RetryGoldenBootAt
		g.backend.inspect(func() { g.templates.deleteErr = status.Error(codes.Unavailable, "private-endpoint") })
		time.Sleep(goldenBootRetryBaseDelay)
		synctest.Wait()
		observed := g.observation()
		require.NotNil(t, observed.Template)
		require.Equal(t, deadline, observed.RetryGoldenBootAt)
		require.Equal(t, 0, observed.GoldenBootRetries)
		state := g.collections.Reconciliations.GetKey(g.initial.ResourceName())
		require.Equal(t, "RuntimePreparationFailed", state.PreparationFailure.Reason)
		require.True(t, state.PreparationFailure.Retryable)
		require.NotContains(t, state.PreparationFailure.Message, "private-endpoint")

		g.backend.inspect(func() { g.templates.deleteErr = nil })
		time.Sleep(time.Second)
		synctest.Wait()
		g.backend.inspect(func() {
			require.Equal(t, []string{"actor-uid"}, g.templates.deleted)
			require.Equal(t, 2, g.templates.created)
		})
		observed = g.observation()
		require.Nil(t, observed.Failure)
		require.Equal(t, 1, observed.GoldenBootRetries)
		require.Equal(t, "ActorTemplatePending", g.readyCondition().Reason)
	})
}

func TestInvalidActorTemplateHasNoCompiledTarget(t *testing.T) {
	collections, harnesses := newPreparationTestCollections(t, "microvm")
	initial := collections.Reconciliations.List()[0]
	updated := harnesses.List()[0].DeepCopy()
	updated.Spec.Env = []kagentv1alpha3.RuntimeEnvVar{{Name: "EXTRA", Value: strings.Repeat("x", 32769)}}
	harnesses.UpdateObject(updated)
	waitFor(t, func() bool {
		return collections.Reconciliations.GetKey(initial.ResourceName()).CompilationFailure != nil
	})
	state := collections.Reconciliations.GetKey(initial.ResourceName())
	require.Equal(t, "ActorTemplateInvalid", state.CompilationFailure.Reason)
	require.Nil(t, state.Target, "a revision and digest must not be published when ActorTemplate construction fails")
	require.Nil(t, state.PreparationFailure)
	store := &fakeRuntimeRevisionStore{}
	templates := &fakeActorTemplates{getErr: errors.New("Substrate must not be called for an invalid target")}
	r := &Reconciler{collections: collections, templates: templates, store: store}
	require.NoError(t, r.reconcileAgent(t.Context(), initial.ResourceName()))
	require.Empty(t, store.pair.DesiredRevision)
	require.Nil(t, templates.template)
	require.Nil(t, store.revision)
	agentStatus := statusForAgent(*state, 1, initial.Target.RevisionID.String())
	require.Empty(t, agentStatus.DesiredRevision)
	require.Equal(t, initial.Target.RevisionID.String(), agentStatus.LatestSuccessfulRevision)
	compatible := apimeta.FindStatusCondition(agentStatus.Conditions, kagentv1alpha3.AgentConditionCompatible)
	require.Equal(t, "ActorTemplateInvalid", compatible.Reason)
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
	runtimeHarness.Spec.Substrate = kagentv1alpha3.RuntimeSubstratePolicy{
		WorkerPoolRef: corev1.LocalObjectReference{Name: workerPool}, SnapshotPolicy: kagentv1alpha3.RuntimeSnapshotPolicy{Location: "snapshots"},
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
		ResolvedModelConfigs:     krttest.GetMockCollection[translator.ResolvedModelConfig](mock),
		RemoteMCPServers:         krttest.GetMockCollection[*kagentv1alpha3.RemoteMCPServer](mock),
		ConfigMaps:               krttest.GetMockCollection[*corev1.ConfigMap](mock),
		Secrets:                  krttest.GetMockCollection[*corev1.Secret](mock),
		WorkerPools:              krttest.GetMockCollection[*atev1alpha1.WorkerPool](mock),
		AgentRuntimeObservations: krt.NewStaticCollection[AgentRuntimeObservation](nil, nil, opts.WithName("AgentRuntimeObservations")...),
		ModelConfigStatuses:      krttest.GetMockCollection[krt.ObjectWithStatus[*kagentv1alpha3.ModelConfig, kagentv1alpha3.ModelConfigStatus]](mock),
	}
	collections.Agents = krt.NewStaticCollection(nil, []*kagentv1alpha3.Agent{testAgent(template, runtimeHarness)}, opts.WithName("Agents")...)
	collections.Reconciliations = newAgentReconciliations(collections.Agents, translator.Collections{
		Harnesses: collections.Harnesses, AgentTemplates: collections.AgentTemplates, ResolvedModelConfigs: collections.ResolvedModelConfigs,
		RemoteMCPServers: collections.RemoteMCPServers, ConfigMaps: collections.ConfigMaps,
		Secrets: collections.Secrets, WorkerPools: collections.WorkerPools,
	}, collections.AgentRuntimeObservations, opts)
	collections.AgentStatuses = newAgentStatuses(collections.Agents, collections.Reconciliations, opts)
	waitFor(t, func() bool {
		states := collections.Reconciliations.List()
		return len(states) == 1 && states[0].CompilationFailure == nil && states[0].Target != nil
	})
	return collections, harnesses
}
