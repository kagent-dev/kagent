package controller

import (
	"context"
	"errors"
	"testing"
	"time"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/jackc/pgx/v5/pgxpool"
	kagentfake "github.com/kagent-dev/kagent/go/api/clientset/versioned/fake"
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
	template := &kagentv1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", UID: "template-uid"}}
	harness := &kagentv1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "kagent", UID: "harness-uid"}}
	desiredActor := &ateapipb.ActorTemplate{Metadata: &ateapipb.ResourceMetadata{Atespace: "team-a", Name: "assistant-kagent-revision"}}
	revision := &v2translator.Revision{AgentCard: &a2apb.AgentCard{Name: "assistant"}}
	revision.AgentCard.ProtoReflect().SetUnknown(protowire.AppendString(protowire.AppendTag(nil, 1000, protowire.BytesType), "future"))
	revisionID, err := revision.Digest()
	if err != nil {
		t.Fatal(err)
	}
	state := PairReconciliation{
		Pair:     AgentTemplateHarnessPair{AgentTemplate: template, Harness: harness},
		Revision: revision, RevisionID: revisionID, DesiredActorTemplate: desiredActor,
	}
	reconciliations := krt.NewStaticCollection(nil, []PairReconciliation{state}, opts.WithName("Reconciliations")...)
	status := kagentv1alpha3.AgentTemplateStatus{ObservedGeneration: 1, Harnesses: []kagentv1alpha3.AgentTemplateHarnessStatus{{
		Harness: "kagent", Conditions: []metav1.Condition{{Type: kagentv1alpha3.AgentTemplateConditionReady, Status: metav1.ConditionFalse}},
	}}}
	mock := krttest.NewMock(t, []any{
		template,
		krt.ObjectWithStatus[*kagentv1alpha3.AgentTemplate, kagentv1alpha3.AgentTemplateStatus]{Obj: template, Status: status},
	})
	statuses := krttest.GetMockCollection[krt.ObjectWithStatus[*kagentv1alpha3.AgentTemplate, kagentv1alpha3.AgentTemplateStatus]](mock)
	store := &fakeRuntimeRevisionStore{}
	templates := &fakeActorTemplates{}
	statusClient := kagentfake.NewSimpleClientset(template.DeepCopy()).ApiV1alpha3()
	reconciler := &Reconciler{
		collections: Collections{
			AgentTemplates:  krttest.GetMockCollection[*kagentv1alpha3.AgentTemplate](mock),
			ActorTemplates:  krt.NewStaticCollection[ObservedActorTemplate](nil, nil, opts.WithName("ActorTemplates")...),
			Reconciliations: reconciliations, AgentTemplateStatuses: statuses,
		},
		templates: templates, store: store, status: statusClient,
	}

	if err := reconciler.reconcilePair(context.Background(), state.ResourceName()); err != nil {
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
	templates.template.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{GoldenSnapshot: &ateapipb.ExternalSnapshot{SnapshotUri: "s3://snapshots/golden"}}}
	writeErr := errors.New("database unavailable")
	for _, stage := range []string{"revision", "success"} {
		t.Run(stage, func(t *testing.T) {
			t.Cleanup(func() { store.revisionErr, store.markErr = nil, nil })
			if stage == "revision" {
				store.revisionErr = writeErr
			} else {
				store.markErr = writeErr
			}
			require.ErrorIs(t, reconciler.reconcilePair(t.Context(), state.ResourceName()), writeErr)
			observed := reconciler.collections.ActorTemplates.GetKey("team-a/assistant-kagent-revision")
			require.NotNil(t, observed)
			require.Nil(t, observed.Template.GetStatus().GetGoldenSnapshotStatus().GetGoldenSnapshot(),
				"Ready must not be published before its database writes succeed")
		})
	}
	if err := reconciler.reconcilePair(context.Background(), state.ResourceName()); err != nil {
		t.Fatal(err)
	}
	if store.revision == nil || !store.markedSuccessful {
		t.Fatal("ready revision was not stored and marked successful")
	}
	require.Empty(t, store.retired, "active pairs must be replaced atomically by the store")
	observed := reconciler.collections.ActorTemplates.GetKey("team-a/assistant-kagent-revision")
	require.NotNil(t, observed.Template.GetStatus().GetGoldenSnapshotStatus().GetGoldenSnapshot())

	if err := reconciler.reconcileAgentTemplateStatus(context.Background(), "team-a/assistant"); err != nil {
		t.Fatal(err)
	}
	statusWrite, err := statusClient.AgentTemplates(template.Namespace).Get(context.Background(), template.Name, metav1.GetOptions{})
	if err != nil || statusWrite.Status.Harnesses[0].Conditions[0].LastTransitionTime.IsZero() {
		t.Fatal("desired status was not written with a transition time")
	}

	reconciliations.DeleteObject(state.ResourceName())
	if err := reconciler.reconcilePair(context.Background(), state.ResourceName()); err != nil {
		t.Fatal(err)
	}
	if store.retired != state.ResourceName() {
		t.Fatalf("retired pair = %q, want %q", store.retired, state.ResourceName())
	}
}

func TestReconcilerCollectsRetiredRevisions(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping database test in short mode")
	}
	ctx := t.Context()
	dsn := dbtest.StartT(ctx, t)
	dbtest.MigrateT(t, dsn, false)
	pool, err := pgxpool.New(ctx, dsn)
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	store := database.NewClient(pool)
	opts := krt.NewOptionsBuilder(ctx.Done(), "test", nil)
	states := krt.NewStaticCollection[PairReconciliation](nil, nil, opts.WithName("Reconciliations")...)
	templates := &fakeActorTemplates{}
	reconciler := &Reconciler{
		collections: Collections{
			ActorTemplates:  krt.NewStaticCollection[ObservedActorTemplate](nil, nil, opts.WithName("ActorTemplates")...),
			Reconciliations: states,
		},
		templates: templates, store: store,
	}
	for _, name := range []string{"first", "replacement"} {
		revision := &v2translator.Revision{
			AgentCard: &a2apb.AgentCard{Name: name}, Provenance: []byte("{}"), EgressDestinations: []string{},
		}
		id, err := revision.Digest()
		require.NoError(t, err)
		desired := &ateapipb.ActorTemplate{Metadata: &ateapipb.ResourceMetadata{
			Atespace: "team-a", Name: "assistant-" + name, Uid: name,
		}}
		templates.template = proto.CloneOf(desired)
		templates.template.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{
			GoldenSnapshot: &ateapipb.ExternalSnapshot{SnapshotUri: "s3://snapshots/" + name},
		}}
		state := PairReconciliation{
			Pair: AgentTemplateHarnessPair{
				AgentTemplate: &kagentv1alpha3.AgentTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "assistant", UID: "template-uid"}},
				Harness:       &kagentv1alpha3.Harness{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "kagent", UID: "harness-uid"}},
			},
			Revision: revision, RevisionID: id, DesiredActorTemplate: desired,
		}
		states.UpdateObject(state)
		require.NoError(t, reconciler.reconcilePair(ctx, state.ResourceName()))
		_, err = store.GetRuntimeRevision(ctx, id.String())
		require.NoError(t, err)

		states.DeleteObject(state.ResourceName())
		require.NoError(t, reconciler.reconcilePair(ctx, state.ResourceName()), "retiring a successful pair must finish GC")
		require.Nil(t, templates.template)
		require.Empty(t, reconciler.collections.ActorTemplates.List())
		_, err = store.GetRuntimeRevision(ctx, id.String())
		require.ErrorIs(t, err, database.ErrNotFound)
		// Retrying deletion must converge even after both resources are gone.
		require.NoError(t, reconciler.reconcilePair(ctx, state.ResourceName()))
	}
}

type fakeActorTemplates struct {
	template *ateapipb.ActorTemplate
}

func (f *fakeActorTemplates) EnsureAtespace(context.Context, string) error { return nil }

func (f *fakeActorTemplates) GetActorTemplate(context.Context, string, string) (*ateapipb.ActorTemplate, error) {
	if f.template == nil {
		return nil, status.Error(codes.NotFound, "not found")
	}
	return f.template, nil
}

func (f *fakeActorTemplates) CreateActorTemplate(_ context.Context, template *ateapipb.ActorTemplate) (*ateapipb.ActorTemplate, error) {
	f.template = template
	f.template.Metadata.Uid = "actor-uid"
	return f.template, nil
}

func (f *fakeActorTemplates) DeleteActorTemplate(context.Context, string, string, string) error {
	f.template = nil
	return nil
}

type fakeRuntimeRevisionStore struct {
	pair             *database.AgentTemplateHarnessPair
	revision         *database.RuntimeRevision
	markedSuccessful bool
	retired          string
	revisionErr      error
	markErr          error
}

func (s *fakeRuntimeRevisionStore) UpsertAgentTemplateHarnessPair(_ context.Context, pair database.AgentTemplateHarnessPair) error {
	s.pair = &pair
	return nil
}

func (s *fakeRuntimeRevisionStore) UpsertRuntimeRevision(_ context.Context, revision database.RuntimeRevision) error {
	if s.revisionErr != nil {
		return s.revisionErr
	}
	s.revision = &revision
	return nil
}

func (s *fakeRuntimeRevisionStore) MarkRuntimeRevisionSuccessful(context.Context, database.AgentTemplateHarnessPair) error {
	if s.markErr != nil {
		return s.markErr
	}
	s.markedSuccessful = true
	return nil
}

func (s *fakeRuntimeRevisionStore) RetireAgentTemplateHarnessPair(_ context.Context, namespace, template, harness string) error {
	s.retired = namespace + "/" + template + "/" + harness
	return nil
}

func (s *fakeRuntimeRevisionStore) ListUnreferencedRuntimeRevisions(context.Context) ([]database.RuntimeRevision, error) {
	return nil, nil
}

func (s *fakeRuntimeRevisionStore) DeleteUnreferencedRuntimeRevision(context.Context, string) error {
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
		ModelConfigs:          modelConfigs,
		Secrets:               secrets,
		ConfigMaps:            configMaps,
		ModelConfigStatuses:   modelConfigStatuses,
		ResolvedModelConfigs:  resolvedModelConfigs,
		AgentTemplates:        krttest.GetMockCollection[*kagentv1alpha3.AgentTemplate](mock),
		Reconciliations:       krttest.GetMockCollection[PairReconciliation](mock),
		AgentTemplateStatuses: krttest.GetMockCollection[krt.ObjectWithStatus[*kagentv1alpha3.AgentTemplate, kagentv1alpha3.AgentTemplateStatus]](mock),
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
