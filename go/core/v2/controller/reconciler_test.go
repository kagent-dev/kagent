package controller

import (
	"context"
	"testing"

	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	v2translator "github.com/kagent-dev/kagent/go/core/v2/translator"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"istio.io/istio/pkg/kube/krt"
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
		updateStatus: func(_ context.Context, template *kagentv1alpha3.AgentTemplate) error {
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

	if err := reconciler.reconcileStatus(context.Background(), "team-a/assistant"); err != nil {
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
