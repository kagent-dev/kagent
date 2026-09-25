package controller

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

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
	"istio.io/istio/pkg/kube/krt"
	"istio.io/istio/pkg/kube/krt/krttest"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestReconcilerPersistsPairInOrder(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	opts := krt.NewOptionsBuilder(stop, "test", nil)
	template := &kagentv1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", UID: "template-uid"}}
	desiredActor := &ateapipb.ActorTemplate{Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "assistant-kagent-revision"}}
	revision := &v2translator.Revision{AgentCard: &a2apb.AgentCard{Name: "assistant"}}
	revision.AgentCard.ProtoReflect().SetUnknown(protowire.AppendString(protowire.AppendTag(nil, 1000, protowire.BytesType), "future"))
	revisionID, err := revision.Digest()
	if err != nil {
		t.Fatal(err)
	}
	state := AgentReconciliation{
		Agent:    template,
		Revision: revision, RevisionID: revisionID, DesiredActorTemplate: desiredActor,
	}
	reconciliations := krt.NewStaticCollection(nil, []AgentReconciliation{state}, opts.WithName("Reconciliations")...)
	status := kagentv1alpha3.AgentStatus{ObservedGeneration: 1, Conditions: []metav1.Condition{{Type: kagentv1alpha3.AgentConditionReady, Status: metav1.ConditionFalse}}}
	mock := krttest.NewMock(t, []any{
		template,
		krt.ObjectWithStatus[*kagentv1alpha3.Agent, kagentv1alpha3.AgentStatus]{Obj: template, Status: status},
	})
	statuses := krttest.GetMockCollection[krt.ObjectWithStatus[*kagentv1alpha3.Agent, kagentv1alpha3.AgentStatus]](mock)
	store := &fakeRuntimeRevisionStore{}
	templates := &fakeActorTemplates{}
	statusClient := kagentfake.NewSimpleClientset(template.DeepCopy()).ApiV1alpha3()
	reconciler := &Reconciler{
		collections: Collections{
			Agents:                   krttest.GetMockCollection[*kagentv1alpha3.Agent](mock),
			AgentRuntimeObservations: krt.NewStaticCollection[AgentRuntimeObservation](nil, nil, opts.WithName("AgentRuntimeObservations")...),
			Reconciliations:          reconciliations, AgentStatuses: statuses,
		},
		templates: templates, store: store, status: statusClient,
	}

	if err := reconciler.reconcileAgent(context.Background(), state.ResourceName()); err != nil {
		t.Fatal(err)
	}
	if store.pair == nil {
		t.Fatal("pair was not stored")
	}
	created := templates.template
	if created == nil {
		t.Fatal("ActorTemplate was not created")
	}
	if store.revision == nil || store.markedSuccessful {
		t.Fatal("pending revision was not stored correctly")
	}

	require.True(t, proto.Equal(revision.AgentCard, store.revision.AgentCard))

	templates.template = proto.CloneOf(created)
	templates.template.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{GoldenTag: &ateapipb.ObjectRef{Atespace: "ate-golden", Name: "golden"}}}
	writeErr := errors.New("database unavailable")
	store.revisionErr = writeErr
	require.ErrorIs(t, reconciler.reconcileAgent(t.Context(), state.ResourceName()), writeErr)
	pending := reconciler.collections.AgentRuntimeObservations.GetKey(state.ResourceName())
	require.NotNil(t, pending)
	require.Nil(t, pending.Template.GetStatus().GetGoldenSnapshotStatus().GetGoldenTag(),
		"Ready must not be published before the database write succeeds")
	store.revisionErr = nil
	if err := reconciler.reconcileAgent(context.Background(), state.ResourceName()); err != nil {
		t.Fatal(err)
	}
	if store.revision == nil || !store.markedSuccessful {
		t.Fatal("ready revision was not stored and marked successful")
	}
	require.Empty(t, store.retired, "active pairs must be replaced atomically by the store")
	observed := reconciler.collections.AgentRuntimeObservations.GetKey(state.ResourceName())
	require.NotNil(t, observed.Template.GetStatus().GetGoldenSnapshotStatus().GetGoldenTag())

	if err := reconciler.reconcileAgentStatus(context.Background(), "team-a/assistant"); err != nil {
		t.Fatal(err)
	}
	statusWrite, err := statusClient.Agents(template.Namespace).Get(context.Background(), template.Name, metav1.GetOptions{})
	if err != nil || statusWrite.Status.Conditions[0].LastTransitionTime.IsZero() {
		t.Fatal("desired status was not written with a transition time")
	}

	// A fresh reconciler must clean up observations without remembering earlier calls.
	reconciler = &Reconciler{collections: reconciler.collections, templates: templates, store: store, status: statusClient}
	state.DesiredActorTemplate = proto.CloneOf(state.DesiredActorTemplate)
	state.DesiredActorTemplate.Metadata.Name = "assistant-next-revision"
	state.Revision = &v2translator.Revision{AgentCard: &a2apb.AgentCard{Name: "updated assistant"}}
	state.RevisionID, err = state.Revision.Digest()
	require.NoError(t, err)
	templates.template = nil
	reconciliations.UpdateObject(state)
	require.NoError(t, reconciler.reconcileAgent(t.Context(), state.ResourceName()))
	require.Equal(t, state.RevisionID, reconciler.collections.AgentRuntimeObservations.GetKey(state.ResourceName()).RevisionID, "a new revision must replace the previous observation without waiting for GC")
	require.Len(t, reconciler.collections.AgentRuntimeObservations.List(), 1)

	reconciliations.DeleteObject(state.ResourceName())
	if err := reconciler.reconcileAgent(context.Background(), state.ResourceName()); err != nil {
		t.Fatal(err)
	}
	require.Empty(t, reconciler.collections.AgentRuntimeObservations.List(), "pair retirement must release its observation without waiting for GC")
	if store.retired != state.ResourceName() {
		t.Fatalf("retired pair = %q, want %q", store.retired, state.ResourceName())
	}
}

func TestRuntimeRevisionGCCollectsRetiredRevisions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}
	for _, test := range []struct {
		name               string
		deletedBeforeError bool
		finalizeFailure    bool
	}{
		{name: "compute deletion failed"},
		{name: "compute deletion succeeded but response lost", deletedBeforeError: true},
		{name: "compute deletion succeeded but database finalization failed", finalizeFailure: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := t.Context()
			dsn := dbtest.StartT(ctx, t)
			dbtest.MigrateT(t, dsn, false)
			pool, err := database.Connect(ctx, &database.PostgresConfig{URL: dsn})
			require.NoError(t, err)
			t.Cleanup(pool.Close)
			store := database.NewClient(pool)
			opts := krt.NewOptionsBuilder(ctx.Done(), "test", nil)
			states := krt.NewStaticCollection[AgentReconciliation](nil, nil, opts.WithName("Reconciliations")...)
			templates := &fakeActorTemplates{}
			reconciler := &Reconciler{
				collections: Collections{
					AgentRuntimeObservations: krt.NewStaticCollection[AgentRuntimeObservation](nil, nil, opts.WithName("AgentRuntimeObservations")...),
					Reconciliations:          states,
				},
				templates: templates, store: store,
			}
			revision := &v2translator.Revision{
				AgentCard: &a2apb.AgentCard{Name: "assistant"}, Provenance: []byte("{}"), EgressDestinations: []string{},
			}
			id, err := revision.Digest()
			require.NoError(t, err)
			desired := &ateapipb.ActorTemplate{Metadata: &ateapipb.ResourceMetadata{
				Atespace: "team-a", Name: "assistant-revision", Uid: "actor-uid",
			}}
			templates.template = proto.CloneOf(desired)
			templates.template.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{
				GoldenTag: &ateapipb.ObjectRef{Atespace: "ate-golden", Name: "golden"},
			}}
			state := AgentReconciliation{
				Agent:    &kagentv1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", UID: "agent-uid"}},
				Revision: revision, RevisionID: id, DesiredActorTemplate: desired,
			}
			states.UpdateObject(state)
			require.NoError(t, reconciler.reconcileAgent(ctx, state.ResourceName()))

			// Compile failures preserve the current UID's last successful runtime.
			state.Revision = nil
			states.UpdateObject(state)
			require.NoError(t, reconciler.reconcileAgent(ctx, state.ResourceName()))
			require.Empty(t, reconciler.collections.AgentRuntimeObservations.List(), "invalid preparation must release its observation before GC")
			request := &apiv1alpha1.AgentInstance{
				Id: uuid.NewString(), Creator: "alice",
				Agent: &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"},
			}
			instance, _, err := store.CreateAgentInstance(ctx, request, "instance")
			require.NoError(t, err)
			require.Equal(t, id.String(), instance.GetPreparedRevision())
			operation, err := store.BeginAgentInstanceOperation(ctx, instance.Id, apiv1alpha1.AgentInstanceOperation_AGENT_INSTANCE_OPERATION_DELETE)
			require.NoError(t, err)
			executor := uuid.New()
			claimed, err := store.ClaimAgentInstanceOperation(ctx, instance.Id, operation.ID, executor)
			require.NoError(t, err)
			require.True(t, claimed)
			_, err = store.FinishAgentInstanceOperation(ctx, instance.Id, operation.ID, executor, "", "", "")
			require.NoError(t, err)
			require.NotNil(t, templates.template)

			// An invalid replacement UID still revokes the old identity. Failed or
			// ambiguous network deletion must leave the claim available to retry.
			state.Agent = state.Agent.DeepCopy()
			state.Agent.UID = "replacement-uid"
			states.UpdateObject(state)
			deleteErr := errors.New("deletion interrupted")
			gcStore := &failingFinalizationStore{Client: store}
			if test.finalizeFailure {
				gcStore.finalizeErr = deleteErr
			} else {
				templates.deleteErr, templates.deletedBeforeError = deleteErr, test.deletedBeforeError
			}
			require.NoError(t, reconciler.reconcileAgent(ctx, state.ResourceName()), "GC failures must not fail pair reconciliation")
			collector := NewRuntimeRevisionGC(gcStore, templates)
			require.ErrorIs(t, collector.collect(ctx, id.String()), deleteErr)
			if test.finalizeFailure || test.deletedBeforeError {
				require.Nil(t, templates.template)
			}
			pending, err := store.ListUnreferencedRuntimeRevisions(ctx)
			require.NoError(t, err)
			require.Len(t, pending, 1, "failed cleanup must remain discoverable after restart")
			_, err = store.GetRuntimeRevision(ctx, id.String())
			require.NoError(t, err)
			_, _, err = store.CreateAgentInstance(ctx, request, "replacement-instance")
			require.ErrorIs(t, err, database.ErrNotFound)
			templates.deleteErr = nil
			restarted := NewRuntimeRevisionGC(database.NewClient(pool), templates)
			restarted.sweep(ctx)
			require.Nil(t, templates.template)
			require.Empty(t, reconciler.collections.AgentRuntimeObservations.List())
			_, err = store.GetRuntimeRevision(ctx, id.String())
			require.ErrorIs(t, err, database.ErrNotFound)
			restarted.sweep(ctx)
		})
	}
}

type failingFinalizationStore struct {
	*database.Client
	finalizeErr error
}

func (s *failingFinalizationStore) DeleteRuntimeRevision(ctx context.Context, revision, uid string) error {
	if s.finalizeErr != nil {
		return s.finalizeErr
	}
	return s.Client.DeleteRuntimeRevision(ctx, revision, uid)
}

type fakeActorTemplates struct {
	template           *ateapipb.ActorTemplate
	ensureErr          error
	getErr             error
	createErr          error
	deleteErr          error
	deletedBeforeError bool
}

func (f *fakeActorTemplates) EnsureAtespace(context.Context, string) error { return f.ensureErr }

func (f *fakeActorTemplates) GetActorTemplate(context.Context, string, string) (*ateapipb.ActorTemplate, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if f.template == nil {
		return nil, status.Error(codes.NotFound, "not found")
	}
	return f.template, nil
}

func (f *fakeActorTemplates) CreateActorTemplate(_ context.Context, template *ateapipb.ActorTemplate) (*ateapipb.ActorTemplate, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.template = proto.CloneOf(template)
	f.template.Metadata.Uid = "actor-uid"
	return f.template, nil
}

func (f *fakeActorTemplates) DeleteActorTemplate(context.Context, string, string) error {
	if f.deleteErr != nil {
		if f.deletedBeforeError {
			f.template = nil
		}
		return f.deleteErr
	}
	f.template = nil
	return nil
}

type fakeRuntimeRevisionStore struct {
	pair             *database.AgentDefinition
	revision         *database.RuntimeRevision
	markedSuccessful bool
	retired          string
	revisionErr      error
	pairErr          error
}

func (s *fakeRuntimeRevisionStore) UpsertAgentDefinition(_ context.Context, pair database.AgentDefinition) error {
	s.pair = &pair
	return s.pairErr
}

func (s *fakeRuntimeRevisionStore) RecordRuntimeRevision(_ context.Context, revision database.RuntimeRevision, ready bool) error {
	if s.revisionErr != nil {
		return s.revisionErr
	}
	s.revision = &revision
	s.markedSuccessful = ready
	return nil
}

func (s *fakeRuntimeRevisionStore) RetireAgentIdentities(_ context.Context, namespace, template string, except *database.AgentDefinition) error {
	if except == nil {
		s.retired = namespace + "/" + template
	}
	return nil
}

func TestReconcilerUpdatesModelConfigStatusOnSecretHashChange(t *testing.T) {
	stop := make(chan struct{})
	t.Cleanup(func() { close(stop) })
	opts := krt.NewOptionsBuilder(stop, "test", nil)

	modelConfig := &kagentv1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "model", Generation: 1},
		Spec: kagentv1alpha3.ModelConfigSpec{
			Model:        "gpt-5",
			Provider:     kagentv1alpha3.ModelProviderOpenAI,
			APIKeySecret: "credentials",
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "credentials"},
		Data:       map[string][]byte{"key": []byte("initial-secret")},
	}

	mock := krttest.NewMock(t, []any{modelConfig})
	modelConfigs := krttest.GetMockCollection[*kagentv1alpha3.ModelConfig](mock)
	secrets := krt.NewStaticCollection(nil, []*corev1.Secret{secret}, opts.WithName("Secrets")...)
	configMaps := krttest.GetMockCollection[*corev1.ConfigMap](mock)
	modelConfigStatuses, resolvedModelConfigs := newModelConfigReconciliations(modelConfigs, configMaps, secrets, opts)

	collections := Collections{
		ModelConfigs:         modelConfigs,
		Secrets:              secrets,
		ConfigMaps:           configMaps,
		ModelConfigStatuses:  modelConfigStatuses,
		ResolvedModelConfigs: resolvedModelConfigs,
		Agents:               krttest.GetMockCollection[*kagentv1alpha3.Agent](mock),
		Reconciliations:      krttest.GetMockCollection[AgentReconciliation](mock),
		AgentStatuses:        krttest.GetMockCollection[krt.ObjectWithStatus[*kagentv1alpha3.Agent, kagentv1alpha3.AgentStatus]](mock),
	}

	statusClient := kagentfake.NewSimpleClientset(modelConfig.DeepCopy()).ApiV1alpha3()
	reconciler := newReconciler(
		collections,
		&fakeActorTemplates{},
		&fakeRuntimeRevisionStore{},
		statusClient,
	)

	go reconciler.Run(stop)

	var initialUpdate *kagentv1alpha3.ModelConfig
	var err error
	require.Eventually(t, func() bool {
		initialUpdate, err = statusClient.ModelConfigs(modelConfig.Namespace).Get(context.Background(), modelConfig.Name, metav1.GetOptions{})
		return err == nil && initialUpdate.Status.SecretHash != ""
	}, 3*time.Second, 10*time.Millisecond)

	if len(initialUpdate.Status.Conditions) != 2 {
		t.Fatalf("expected 2 conditions in status, got: %+v", initialUpdate.Status.Conditions)
	}
	if initialUpdate.Status.Conditions[0].LastTransitionTime.IsZero() || initialUpdate.Status.Conditions[1].LastTransitionTime.IsZero() {
		t.Fatal("expected LastTransitionTime to be set on ModelConfig conditions")
	}

	initialHash := initialUpdate.Status.SecretHash

	secrets.UpdateObject(&corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "credentials"},
		Data:       map[string][]byte{"key": []byte("updated-secret")},
	})

	var updatedMC *kagentv1alpha3.ModelConfig
	require.Eventually(t, func() bool {
		updatedMC, err = statusClient.ModelConfigs(modelConfig.Namespace).Get(context.Background(), modelConfig.Name, metav1.GetOptions{})
		return err == nil && updatedMC.Status.SecretHash != initialHash
	}, 3*time.Second, 10*time.Millisecond)
}

func TestReconciliationQueueRetriesWithBackoff(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		var attempts atomic.Int32
		queue := newReconciliationQueue("test-retries", func(any) error {
			attempts.Add(1)
			return errors.New("database unavailable")
		})
		go queue.Run(ctx.Done())
		queue.Add("team-a/assistant/kagent")
		synctest.Wait()
		require.EqualValues(t, 1, attempts.Load())
		time.Sleep(time.Second - time.Nanosecond)
		synctest.Wait()
		require.EqualValues(t, 1, attempts.Load(), "errors must back off before retrying")
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		require.EqualValues(t, 2, attempts.Load())
		time.Sleep(3 * time.Minute)
		synctest.Wait()
		require.EqualValues(t, 10, attempts.Load(), "persistent failures must exhaust a finite budget")
		time.Sleep(time.Minute)
		synctest.Wait()
		require.EqualValues(t, 10, attempts.Load())
		queue.Add("team-a/assistant/kagent")
		synctest.Wait()
		require.EqualValues(t, 11, attempts.Load(), "new graph events must still enqueue work")
		cancel()
		require.NoError(t, queue.WaitForClose(time.Second))
	})
}
