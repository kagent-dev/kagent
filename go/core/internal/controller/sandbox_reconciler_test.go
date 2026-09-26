package controller

import (
	"context"
	"errors"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	atev1alpha1 "github.com/agent-substrate/substrate/pkg/api/v1alpha1"
	"github.com/agent-substrate/substrate/pkg/proto/ateapipb"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	kagentfake "github.com/kagent-dev/kagent/go/api/clientset/versioned/fake"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"
)

// These fakes are also used by the informer/queue integration test.
type sandboxTestStore struct {
	mu             sync.Mutex
	desired        database.SandboxTemplateDefinition
	revision       database.SandboxRevision
	ready          bool
	retired        bool
	retireAttempts int
	retireErr      error
	recordErr      error
}

func (s *sandboxTestStore) UpsertSandboxTemplateDefinition(_ context.Context, value database.SandboxTemplateDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.desired = value
	return nil
}
func (s *sandboxTestStore) RecordSandboxRevision(_ context.Context, value database.SandboxRevision, ready bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.recordErr != nil {
		return s.recordErr
	}
	s.revision, s.ready = value, ready
	return nil
}
func (s *sandboxTestStore) RetireSandboxTemplateIdentities(context.Context, string, string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.retireAttempts++
	if s.retireErr != nil {
		return s.retireErr
	}
	s.retired = true
	return nil
}

var _ sandboxRevisionStore = (*sandboxTestStore)(nil)

type sandboxTestActors struct {
	mu        sync.Mutex
	store     *sandboxTestStore
	templates map[string]*ateapipb.ActorTemplate
	autoReady bool
}

var _ actorTemplateClient = (*sandboxTestActors)(nil)

func (*sandboxTestActors) EnsureAtespace(context.Context, string) error { return nil }
func (s *sandboxTestActors) GetActorTemplate(_ context.Context, atespace, name string) (*ateapipb.ActorTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if template := s.templates[atespace+"/"+name]; template != nil {
		return proto.CloneOf(template), nil
	}
	return nil, status.Error(codes.NotFound, "missing")
}
func (s *sandboxTestActors) CreateActorTemplate(_ context.Context, template *ateapipb.ActorTemplate) (*ateapipb.ActorTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.store.mu.Lock()
	desired := s.store.desired.DesiredRevision
	s.store.mu.Unlock()
	if len(desired) != 64 || template.Metadata.Name != "sandbox-"+desired[:40] {
		return nil, errors.New("backend mutation before pinning inputs")
	}
	template = proto.CloneOf(template)
	template.Metadata.Uid = "backend-uid"
	if s.autoReady {
		template.Status = sandboxGoldenStatus()
	}
	s.templates[template.Metadata.Atespace+"/"+template.Metadata.Name] = template
	return proto.CloneOf(template), nil
}

func sandboxGoldenStatus() *ateapipb.ActorTemplateStatus {
	return &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{GoldenTag: &ateapipb.ObjectRef{Atespace: "ate-golden", Name: "golden"}}}
}

func sandboxTestTemplate() *kagentv1alpha3.SandboxTemplate {
	return &kagentv1alpha3.SandboxTemplate{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "scratch", UID: "template-uid", Generation: 1}, Spec: kagentv1alpha3.SandboxTemplateSpec{
		Workload:  kagentv1alpha3.SandboxTemplateWorkload{Image: "tools@sha256:" + strings.Repeat("a", 64)},
		Substrate: kagentv1alpha3.HarnessSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: kagentv1alpha3.HarnessSnapshotPolicy{Location: "s3://snapshots/"}},
	}}
}

func newSandboxTestReconciler(t *testing.T, guestImage string) (*SandboxReconciler, krt.StaticCollection[*kagentv1alpha3.SandboxTemplate], krt.StaticCollection[*atev1alpha1.WorkerPool]) {
	t.Helper()
	opts := krt.NewOptionsBuilder(t.Context().Done(), "test-sandbox", nil)
	template := sandboxTestTemplate()
	pool := &atev1alpha1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: template.Namespace, Name: "default"}}
	templates := krt.NewStaticCollection(nil, []*kagentv1alpha3.SandboxTemplate{template}, opts.WithName("SandboxTemplates")...)
	pools := krt.NewStaticCollection(nil, []*atev1alpha1.WorkerPool{pool}, opts.WithName("WorkerPools")...)
	store := &sandboxTestStore{}
	actors := &sandboxTestActors{store: store, templates: map[string]*ateapipb.ActorTemplate{}}
	reconciler := &SandboxReconciler{
		collections: newSandboxCollections(Collections{SandboxTemplates: templates, WorkerPools: pools}, substrate.SandboxPolicy{GuestImage: guestImage, CPU: "1", Memory: "1Gi"}, opts),
		store:       store, actors: actors, client: kagentfake.NewSimpleClientset(template.DeepCopy()).ApiV1alpha3(),
	}
	waitFor(t, func() bool { return reconciler.collections.states.GetKey("team-a/scratch") != nil })
	return reconciler, templates, pools
}

func syncSandboxTemplate(t *testing.T, reconciler *SandboxReconciler, templates krt.StaticCollection[*kagentv1alpha3.SandboxTemplate]) *kagentv1alpha3.SandboxTemplate {
	t.Helper()
	template, err := reconciler.client.SandboxTemplates("team-a").Get(t.Context(), "scratch", metav1.GetOptions{})
	require.NoError(t, err)
	templates.UpdateObject(template)
	waitFor(t, func() bool {
		return reflect.DeepEqual(reconciler.collections.states.GetKey("team-a/scratch").Template, template)
	})
	return template
}

func TestSandboxPreparationPinsBeforeBackendAndPublishesAfterPersistence(t *testing.T) {
	registry := httptest.NewServer(registry.New())
	t.Cleanup(registry.Close)
	guestImage := strings.TrimPrefix(registry.URL, "http://") + "/sandbox-guest:v1.2.3"
	s, templates, _ := newSandboxTestReconciler(t, guestImage)
	store := s.store.(*sandboxTestStore)
	actors := s.actors.(*sandboxTestActors)
	const key = "team-a/scratch"
	require.NoError(t, s.reconcile(t.Context(), key))
	require.Empty(t, store.desired.DesiredRevision, "finalizer must persist before preparation")
	require.Empty(t, actors.templates)
	template := syncSandboxTemplate(t, s, templates)
	require.Contains(t, template.Finalizers, sandboxPreparationFinalizer)
	require.ErrorContains(t, s.reconcile(t.Context(), key), "resolve sandbox guest image")
	require.Empty(t, actors.templates)
	require.True(t, strings.HasPrefix(store.desired.DesiredRevision, "pending:"))
	waitFor(t, func() bool { return s.collections.states.GetKey(key).Failure != nil })
	require.NoError(t, s.reconcileStatus(t.Context(), key))
	template = syncSandboxTemplate(t, s, templates)
	require.Equal(t, sandboxPreparationFailed, apimeta.FindStatusCondition(template.Status.Conditions, "Ready").Reason)

	require.NoError(t, crane.Push(empty.Image, guestImage, crane.WithContext(t.Context())))
	require.NoError(t, s.reconcile(t.Context(), key))
	waitFor(t, func() bool { return s.collections.states.GetKey(key).DesiredActorTemplate != nil })
	require.NoError(t, s.reconcile(t.Context(), key))
	state := s.collections.states.GetKey(key)
	ref := state.DesiredActorTemplate.Metadata
	observed, err := actors.GetActorTemplate(t.Context(), ref.Atespace, ref.Name)
	require.NoError(t, err)
	digest, err := empty.Image.Digest()
	require.NoError(t, err)
	require.Equal(t, strings.TrimSuffix(guestImage, ":v1.2.3")+"@"+digest.String(), observed.Volumes[1].Image.Reference)
	require.Contains(t, string(store.revision.SourceSnapshot), observed.Volumes[1].Image.Reference)
	require.NotContains(t, string(store.revision.SourceSnapshot), guestImage)
	require.Equal(t, store.desired.DesiredRevision, store.revision.Revision)
	require.False(t, store.ready)
	registry.Close() // Subsequent preparations use the already-resolved guest digest.

	actors.templates[ref.Atespace+"/"+ref.Name].Status = sandboxGoldenStatus()
	store.recordErr = errors.New("database unavailable with private details")
	require.ErrorContains(t, s.reconcile(t.Context(), key), "database unavailable")
	waitFor(t, func() bool { return s.collections.states.GetKey(key).Failure != nil })
	require.NoError(t, s.reconcileStatus(t.Context(), key))
	template = syncSandboxTemplate(t, s, templates)
	ready := apimeta.FindStatusCondition(template.Status.Conditions, "Ready")
	require.Equal(t, metav1.ConditionFalse, ready.Status)
	require.NotContains(t, ready.Message, "private details")
	store.recordErr = nil
	require.NoError(t, s.reconcile(t.Context(), key))
	waitFor(t, func() bool {
		return s.collections.states.GetKey(key).ObservedActorTemplate.GetStatus().GetGoldenSnapshotStatus().GetGoldenTag() != nil
	})
	require.True(t, store.ready)
	require.NoError(t, s.reconcileStatus(t.Context(), key))
	template = syncSandboxTemplate(t, s, templates)
	ready = apimeta.FindStatusCondition(template.Status.Conditions, "Ready")
	require.Equal(t, metav1.ConditionTrue, ready.Status)
	before := ready.LastTransitionTime
	require.NoError(t, s.reconcileStatus(t.Context(), key))
	template = syncSandboxTemplate(t, s, templates)
	require.Equal(t, before, apimeta.FindStatusCondition(template.Status.Conditions, "Ready").LastTransitionTime)
}

func TestSandboxCollectionsTrackDependenciesAndRejectStaleReadiness(t *testing.T) {
	s, templates, pools := newSandboxTestReconciler(t, "guest@sha256:"+strings.Repeat("b", 64))
	const key = "team-a/scratch"
	require.NoError(t, s.reconcile(t.Context(), key))
	syncSandboxTemplate(t, s, templates)
	require.NoError(t, s.reconcile(t.Context(), key))
	waitFor(t, func() bool { return s.collections.states.GetKey(key).RevisionID != "" })
	original := s.collections.states.GetKey(key)
	observed := proto.CloneOf(original.DesiredActorTemplate)
	observed.Status = sandboxGoldenStatus()
	s.collections.observations.UpdateObject(sandboxRuntimeObservation{Key: key, RevisionID: original.RevisionID, Template: observed})
	waitFor(t, func() bool { return s.collections.states.GetKey(key).ObservedActorTemplate != nil })

	pool := (*pools.GetKey("team-a/default")).DeepCopy()
	pool.Spec.SandboxClass = atev1alpha1.SandboxClassMicroVM
	pools.UpdateObject(pool)
	waitFor(t, func() bool { return s.collections.states.GetKey(key).RevisionID != original.RevisionID })
	changed := s.collections.states.GetKey(key)
	require.NotEmpty(t, changed.RevisionID)
	require.Nil(t, changed.ObservedActorTemplate)
	require.False(t, proto.Equal(original.DesiredActorTemplate.SandboxConfig, changed.DesiredActorTemplate.SandboxConfig))
	pools.DeleteObject("team-a/default")
	waitFor(t, func() bool { return s.collections.states.GetKey(key).Failure != nil })
	missing := s.collections.states.GetKey(key)
	require.Equal(t, "WorkerPoolNotFound", missing.Failure.Reason)
	require.Empty(t, missing.RevisionID)
	require.Nil(t, missing.ObservedActorTemplate)
	require.NoError(t, s.reconcile(t.Context(), key))
	require.True(t, strings.HasPrefix(s.store.(*sandboxTestStore).desired.DesiredRevision, "pending:"))
	pools.UpdateObject(pool)
	waitFor(t, func() bool { return s.collections.states.GetKey(key).RevisionID == changed.RevisionID })
	require.Nil(t, s.collections.states.GetKey(key).Failure)

	// Editing the workload invalidates readiness even when the UID is unchanged.
	edited := (*templates.GetKey(key)).DeepCopy()
	edited.Generation++
	edited.Spec.Workload.Image = "tools@sha256:" + strings.Repeat("c", 64)
	templates.UpdateObject(edited)
	waitFor(t, func() bool { return s.collections.states.GetKey(key).Template.Generation == edited.Generation })
	require.NotEqual(t, changed.RevisionID, s.collections.states.GetKey(key).RevisionID)
	require.Nil(t, s.collections.states.GetKey(key).ObservedActorTemplate)

	// A recreated template cannot inherit the previous UID's readiness.
	template := (*templates.GetKey(key)).DeepCopy()
	template.UID = "replacement-uid"
	templates.UpdateObject(template)
	waitFor(t, func() bool { return s.collections.states.GetKey(key).Template.UID == template.UID })
	require.NotEqual(t, changed.RevisionID, s.collections.states.GetKey(key).RevisionID)
	require.Nil(t, s.collections.states.GetKey(key).ObservedActorTemplate)
}

func TestSandboxPreparationRetriesGoldenFailureAndRejectsImmutableConflict(t *testing.T) {
	s, templates, _ := newSandboxTestReconciler(t, "guest@sha256:"+strings.Repeat("b", 64))
	const key = "team-a/scratch"
	require.NoError(t, s.reconcile(t.Context(), key))
	syncSandboxTemplate(t, s, templates)
	require.NoError(t, s.reconcile(t.Context(), key))
	waitFor(t, func() bool { return s.collections.states.GetKey(key).RevisionID != "" })
	require.NoError(t, s.reconcile(t.Context(), key))
	ref := s.collections.states.GetKey(key).DesiredActorTemplate.Metadata
	observed := s.actors.(*sandboxTestActors).templates[ref.Atespace+"/"+ref.Name]
	observed.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{ErrorMessage: "snapshot backend unavailable"}}
	require.ErrorContains(t, s.reconcile(t.Context(), key), "golden snapshot failed")
	waitFor(t, func() bool { return s.collections.states.GetKey(key).Failure != nil })
	require.True(t, s.collections.states.GetKey(key).canPrepare())
	observed.Status = sandboxGoldenStatus()
	require.NoError(t, s.reconcile(t.Context(), key))
	waitFor(t, func() bool { return s.collections.states.GetKey(key).Failure == nil })
	observed.Containers[0].Image = "unexpected"
	require.ErrorContains(t, s.reconcile(t.Context(), key), "immutable inputs disagree")
	waitFor(t, func() bool { return s.collections.states.GetKey(key).Failure != nil })
	require.False(t, s.collections.states.GetKey(key).canPrepare())
	require.Nil(t, s.collections.states.GetKey(key).ObservedActorTemplate)
}

func TestResolveGuestImageDigestNeedsNoRegistry(t *testing.T) {
	image := "unreachable.invalid/guest@sha256:" + strings.Repeat("b", 64)
	got, err := resolveGuestImage(t.Context(), image)
	require.NoError(t, err)
	require.Equal(t, image, got)
}

func TestResolveGuestImageRejectsMalformedReference(t *testing.T) {
	_, err := resolveGuestImage(t.Context(), "invalid image")
	require.ErrorContains(t, err, "parse sandbox guest image")
}

// A ready runtime may outlive an exhausted status queue. Recovery must not need
// another Kubernetes edit or backend readiness change to publish Ready.
func TestSandboxPendingStatusRecoversWithoutGraphEvent(t *testing.T) {
	s, templates, _ := newSandboxTestReconciler(t, "guest@sha256:"+strings.Repeat("b", 64))
	const key = "team-a/scratch"
	require.NoError(t, s.reconcile(t.Context(), key))
	syncSandboxTemplate(t, s, templates)
	require.NoError(t, s.reconcile(t.Context(), key))
	waitFor(t, func() bool { return s.collections.states.GetKey(key).RevisionID != "" })
	s.actors.(*sandboxTestActors).autoReady = true
	require.NoError(t, s.reconcile(t.Context(), key))
	waitFor(t, func() bool { return s.collections.states.GetKey(key).ObservedActorTemplate != nil })
	client := kagentfake.NewSimpleClientset(s.collections.states.GetKey(key).Template.DeepCopy())
	failed := false
	client.PrependReactor("update", "sandboxtemplates", func(action ktesting.Action) (bool, runtime.Object, error) {
		if action.GetSubresource() == "status" && !failed {
			failed = true
			return true, nil, errors.New("API unavailable")
		}
		return false, nil, nil
	})
	s.client = client.ApiV1alpha3()
	require.ErrorContains(t, s.reconcileStatus(t.Context(), key), "API unavailable")
	ctx, cancel := context.WithCancel(t.Context())
	statuses := newReconciliationQueue("test-sandbox-status", func(item any) error { return s.reconcileStatus(ctx, item.(string)) })
	preparations := newReconciliationQueue("test-sandbox-preparation", func(any) error { return nil })
	var workers sync.WaitGroup
	workers.Go(func() { s.pollPending(ctx, preparations, statuses) })
	workers.Go(func() { statuses.Run(ctx.Done()) })
	t.Cleanup(func() { cancel(); workers.Wait(); preparations.ShutDownEarly() })
	require.Eventually(t, func() bool {
		current, err := s.client.SandboxTemplates("team-a").Get(t.Context(), "scratch", metav1.GetOptions{})
		return err == nil && apimeta.IsStatusConditionTrue(current.Status.Conditions, "Ready")
	}, 5*time.Second, 10*time.Millisecond)
}
