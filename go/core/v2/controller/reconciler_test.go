package controller

import (
	"context"
	"testing"
	"time"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/v2/translator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"istio.io/istio/pkg/kube/krt"
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
	revision := &v2translator.Revision{}
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
	statuses := krt.NewStaticCollection(nil, []krt.ObjectWithStatus[*kagentv1alpha3.AgentTemplate, kagentv1alpha3.AgentTemplateStatus]{{Obj: template, Status: status}}, opts.WithName("Statuses")...)
	store := &fakeRuntimeRevisionStore{}
	templates := &fakeActorTemplates{}
	var statusWrite *kagentv1alpha3.AgentTemplate
	reconciler := &Reconciler{
		collections: Collections{
			AgentTemplates:  krt.NewStaticCollection(nil, []*kagentv1alpha3.AgentTemplate{template}, opts.WithName("AgentTemplates")...),
			ActorTemplates:  krt.NewStaticCollection[ObservedActorTemplate](nil, nil, opts.WithName("ActorTemplates")...),
			Reconciliations: reconciliations, AgentTemplateStatuses: statuses,
		},
		templates: templates, store: store,
		updateAgentTemplateStatus: func(_ context.Context, template *kagentv1alpha3.AgentTemplate) error {
			statusWrite = template
			return nil
		},
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

	created.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{GoldenSnapshot: &ateapipb.ObjectRef{Atespace: "ate-golden", Name: "golden"}}}
	if err := reconciler.reconcilePair(context.Background(), state.ResourceName()); err != nil {
		t.Fatal(err)
	}
	if store.revision == nil || !store.markedSuccessful {
		t.Fatal("ready revision was not stored and marked successful")
	}

	if err := reconciler.reconcileAgentTemplateStatus(context.Background(), "team-a/assistant"); err != nil {
		t.Fatal(err)
	}
	if statusWrite == nil || statusWrite.Status.Harnesses[0].Conditions[0].LastTransitionTime.IsZero() {
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
	pair             *dbpkg.AgentTemplateHarnessPair
	revision         *dbpkg.RuntimeRevision
	markedSuccessful bool
	retired          string
}

func (s *fakeRuntimeRevisionStore) UpsertAgentTemplateHarnessPair(_ context.Context, pair dbpkg.AgentTemplateHarnessPair) error {
	s.pair = &pair
	return nil
}

func (s *fakeRuntimeRevisionStore) UpsertRuntimeRevision(_ context.Context, revision dbpkg.RuntimeRevision) error {
	s.revision = &revision
	return nil
}

func (s *fakeRuntimeRevisionStore) MarkRuntimeRevisionSuccessful(context.Context, dbpkg.AgentTemplateHarnessPair) error {
	s.markedSuccessful = true
	return nil
}

func (s *fakeRuntimeRevisionStore) RetireAgentTemplateHarnessPair(_ context.Context, namespace, template, harness string) error {
	s.retired = namespace + "/" + template + "/" + harness
	return nil
}

func (s *fakeRuntimeRevisionStore) ListUnreferencedRuntimeRevisions(context.Context) ([]dbpkg.RuntimeRevision, error) {
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

	modelConfigs := krt.NewStaticCollection(nil, []*kagentv1alpha3.ModelConfig{modelConfig}, opts.WithName("ModelConfigs")...)
	secrets := krt.NewStaticCollection(nil, []*corev1.Secret{secret}, opts.WithName("Secrets")...)
	configMaps := krt.NewStaticCollection[*corev1.ConfigMap](nil, nil, opts.WithName("ConfigMaps")...)
	modelConfigReconciliations := newModelConfigReconciliations(modelConfigs, configMaps, secrets, opts)

	collections := Collections{
		ModelConfigs:               modelConfigs,
		Secrets:                    secrets,
		ConfigMaps:                 configMaps,
		ModelConfigReconciliations: modelConfigReconciliations,
		AgentTemplates:             krt.NewStaticCollection[*kagentv1alpha3.AgentTemplate](nil, nil, opts.WithName("AgentTemplates")...),
		Reconciliations:            krt.NewStaticCollection[PairReconciliation](nil, nil, opts.WithName("Reconciliations")...),
		AgentTemplateStatuses:      krt.NewStaticCollection[krt.ObjectWithStatus[*kagentv1alpha3.AgentTemplate, kagentv1alpha3.AgentTemplateStatus]](nil, nil, opts.WithName("AgentTemplateStatuses")...),
	}

	statusUpdates := make(chan *kagentv1alpha3.ModelConfig, 10)
	reconciler := newReconciler(
		collections,
		&fakeActorTemplates{},
		&fakeRuntimeRevisionStore{},
		func(_ context.Context, _ *kagentv1alpha3.AgentTemplate) error { return nil },
		func(_ context.Context, mc *kagentv1alpha3.ModelConfig) error {
			modelConfigs.UpdateObject(mc.DeepCopy())
			statusUpdates <- mc.DeepCopy()
			return nil
		},
	)

	go reconciler.Run(stop)

	var initialUpdate *kagentv1alpha3.ModelConfig
	select {
	case initialUpdate = <-statusUpdates:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for initial ModelConfig status update")
	}

	if initialUpdate.Status.SecretHash == "" {
		t.Fatal("expected secret hash in status update")
	}
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
	select {
	case updatedMC = <-statusUpdates:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for updated ModelConfig status after secret change")
	}

	if updatedMC.Status.SecretHash == initialHash {
		t.Fatalf("expected secret hash to change after secret update, but got initial hash %s", initialHash)
	}
}
