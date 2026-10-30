package controller

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	ateapipb "go.ate.dev/substrate/api/ateapi/v1"
	atev1 "go.ate.dev/substrate/api/infra/k8s/ate/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ktesting "k8s.io/client-go/testing"

	kagentfake "github.com/kagent-dev/kagent/go/api/clientset/versioned/fake"
	kagentv1alpha3 "github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
)

// These fakes are also used by the informer/queue integration test.
type sandboxTestStore struct {
	egressTargetStoreStub
	upsertErr      error
	mu             sync.Mutex
	desired        database.SandboxTemplateDefinition
	revision       database.SandboxRevision
	ready          bool
	retired        bool
	retireAttempts int
	retireErr      error
	recordErr      error
	recordCalls    int
	golden         fakeGoldenRetries
}

func (s *sandboxTestStore) UpsertSandboxTemplateDefinition(_ context.Context, value database.SandboxTemplateDefinition) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.desired = value
	return s.upsertErr
}
func (s *sandboxTestStore) RecordSandboxRevision(_ context.Context, value database.SandboxRevision, options database.RecordRevisionOptions) (database.GoldenRetry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recordCalls++
	if s.recordErr != nil {
		return database.GoldenRetry{}, s.recordErr
	}
	s.revision = value
	if options.Ready {
		s.ready = true
	}
	return s.golden.recordGolden(value.ActorTemplateUID, options), nil
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
	actorEgressStub
	mu           sync.Mutex
	store        *sandboxTestStore
	templates    map[string]*ateapipb.ActorTemplate
	autoReady    bool
	failOnCreate bool
	queuedUIDs   []string
	deletedUIDs  []string
	deleteErr    error
	gets         int
}

func (s *sandboxTestActors) inspect(read func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	read()
}

var _ actorTemplateClient = (*sandboxTestActors)(nil)

func (*sandboxTestActors) EnsureAtespace(context.Context, string) error { return nil }
func (s *sandboxTestActors) GetActorTemplate(_ context.Context, atespace, name string) (*ateapipb.ActorTemplate, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.gets++
	if template := s.templates[atespace+"/"+name]; template != nil {
		return proto.CloneOf(template), nil
	}
	return nil, status.Error(codes.NotFound, "missing")
}
func (s *sandboxTestActors) DeleteActorTemplate(_ context.Context, atespace, name, uid string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.deletedUIDs = append(s.deletedUIDs, uid)
	if s.deleteErr != nil {
		return s.deleteErr
	}
	if template := s.templates[atespace+"/"+name]; template != nil && template.Metadata.Uid == uid {
		delete(s.templates, atespace+"/"+name)
	}
	return nil
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
	if len(s.queuedUIDs) > 0 {
		template.Metadata.Uid, s.queuedUIDs = s.queuedUIDs[0], s.queuedUIDs[1:]
	}
	switch {
	case s.autoReady:
		template.Status = sandboxGoldenStatus()
	case s.failOnCreate:
		template.Status = goldenFailed(template).Status
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
		Substrate: kagentv1alpha3.RuntimeSubstratePolicy{WorkerPoolRef: corev1.LocalObjectReference{Name: "default"}, SnapshotPolicy: kagentv1alpha3.RuntimeSnapshotPolicy{Location: "s3://snapshots/"}},
	}}
}

func newSandboxTestReconciler(t *testing.T, guestImage string) (*SandboxReconciler, krt.StaticCollection[*kagentv1alpha3.SandboxTemplate], krt.StaticCollection[*atev1.WorkerPool]) {
	t.Helper()
	opts := krt.NewOptionsBuilder(t.Context().Done(), "test-sandbox", nil)
	template := sandboxTestTemplate()
	pool := &atev1.WorkerPool{ObjectMeta: metav1.ObjectMeta{Namespace: template.Namespace, Name: "default"}}
	templates := krt.NewStaticCollection(nil, []*kagentv1alpha3.SandboxTemplate{template}, opts.WithName("SandboxTemplates")...)
	pools := krt.NewStaticCollection(nil, []*atev1.WorkerPool{pool}, opts.WithName("WorkerPools")...)
	store := &sandboxTestStore{}
	actors := &sandboxTestActors{store: store, templates: map[string]*ateapipb.ActorTemplate{}}
	reconciler := &SandboxReconciler{
		collections: newSandboxCollections(Collections{SandboxTemplates: templates, WorkerPools: pools}, substrate.SandboxPolicy{GuestImage: guestImage, CPU: "1", Memory: "1Gi"}, opts),
		store:       store, actors: actors, client: kagentfake.NewSimpleClientset(template.DeepCopy()).ApiV1alpha3(),
		golden: defaultGoldenRetries,
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

func TestSandboxPreparationPublishesAfterPersistence(t *testing.T) {
	guestImage := "unreachable.invalid/guest@sha256:" + strings.Repeat("b", 64)
	s, templates, _ := newSandboxTestReconciler(t, guestImage)
	store := s.store.(*sandboxTestStore)
	actors := s.actors.(*sandboxTestActors)
	const key = "team-a/scratch"
	require.NoError(t, s.reconcile(t.Context(), key))
	require.Empty(t, store.desired.DesiredRevision, "finalizer must persist before preparation")
	require.Empty(t, actors.templates)
	template := syncSandboxTemplate(t, s, templates)
	require.Contains(t, template.Finalizers, sandboxPreparationFinalizer)
	require.NoError(t, s.reconcile(t.Context(), key))
	state := s.collections.states.GetKey(key)
	ref := state.DesiredActorTemplate.Metadata
	observed, err := actors.GetActorTemplate(t.Context(), ref.Atespace, ref.Name)
	require.NoError(t, err)
	require.Equal(t, guestImage, observed.Volumes[1].Image.Reference)
	require.Contains(t, string(store.revision.SourceSnapshot), observed.Volumes[1].Image.Reference)
	require.Equal(t, store.desired.DesiredRevision, store.revision.Revision)
	require.False(t, store.ready)
	require.NoError(t, s.reconcileStatus(t.Context(), key))
	template = syncSandboxTemplate(t, s, templates)
	preparing := apimeta.FindStatusCondition(template.Status.Conditions, "Ready")
	require.Equal(t, metav1.ConditionFalse, preparing.Status)
	require.Equal(t, "Preparing", preparing.Reason)
	require.Equal(t, "Waiting for the prepared sandbox snapshot", preparing.Message)

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
	waitFor(t, func() bool { return sandboxPrepared(s.collections.states.GetKey(key)) })
	require.True(t, store.ready)
	require.NoError(t, s.reconcileStatus(t.Context(), key))
	template = syncSandboxTemplate(t, s, templates)
	ready = apimeta.FindStatusCondition(template.Status.Conditions, "Ready")
	require.Equal(t, metav1.ConditionTrue, ready.Status)
	require.Equal(t, "Prepared", ready.Reason)
	require.Equal(t, "Sandbox runtime is prepared", ready.Message)
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
	ref := original.DesiredActorTemplate.Metadata
	s.actors.(*sandboxTestActors).templates[ref.Atespace+"/"+ref.Name].Status = sandboxGoldenStatus()
	require.NoError(t, s.reconcile(t.Context(), key))
	waitFor(t, func() bool { return sandboxPrepared(s.collections.states.GetKey(key)) })

	pool := (*pools.GetKey("team-a/default")).DeepCopy()
	pool.Spec.SandboxClasses = []atev1.WorkerPoolSandboxClass{{Name: atev1.SandboxClassMicroVM}}
	pools.UpdateObject(pool)
	waitFor(t, func() bool { return s.collections.states.GetKey(key).RevisionID != original.RevisionID })
	changed := s.collections.states.GetKey(key)
	require.NotEmpty(t, changed.RevisionID)
	require.False(t, sandboxPrepared(changed))
	pools.DeleteObject("team-a/default")
	waitFor(t, func() bool { return s.collections.states.GetKey(key).Failure != nil })
	missing := s.collections.states.GetKey(key)
	require.Equal(t, "WorkerPoolNotFound", missing.Failure.Reason)
	require.Empty(t, missing.RevisionID)
	require.False(t, sandboxPrepared(missing))
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
	require.False(t, sandboxPrepared(s.collections.states.GetKey(key)))

	// A recreated template cannot inherit the previous UID's readiness.
	template := (*templates.GetKey(key)).DeepCopy()
	template.UID = "replacement-uid"
	templates.UpdateObject(template)
	waitFor(t, func() bool { return s.collections.states.GetKey(key).Template.UID == template.UID })
	require.NotEqual(t, changed.RevisionID, s.collections.states.GetKey(key).RevisionID)
	require.False(t, sandboxPrepared(s.collections.states.GetKey(key)))
}

func TestSandboxPreparationRetriesUnprefixedGoldenError(t *testing.T) {
	s, templates, _ := newSandboxTestReconciler(t, "guest@sha256:"+strings.Repeat("b", 64))
	const key = "team-a/scratch"
	require.NoError(t, s.reconcile(t.Context(), key))
	syncSandboxTemplate(t, s, templates)
	require.NoError(t, s.reconcile(t.Context(), key))
	ref := s.collections.states.GetKey(key).DesiredActorTemplate.Metadata
	observed := s.actors.(*sandboxTestActors).templates[ref.Atespace+"/"+ref.Name]
	observed.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{ErrorMessage: "snapshot backend unavailable"}}
	require.NoError(t, s.reconcile(t.Context(), key))
	waitFor(t, func() bool { return s.collections.states.GetKey(key).Failure != nil })
	failure := s.collections.states.GetKey(key).Failure
	require.Equal(t, "ActorTemplateRetrying", failure.Reason)
	require.Contains(t, failure.Message, "golden build failed (unknown), retry 1 at")
	require.NotContains(t, failure.Message, "snapshot backend unavailable")
}

func TestSandboxPreparationRejectsImmutableConflict(t *testing.T) {
	s, templates, _ := newSandboxTestReconciler(t, "guest@sha256:"+strings.Repeat("b", 64))
	const key = "team-a/scratch"
	require.NoError(t, s.reconcile(t.Context(), key))
	syncSandboxTemplate(t, s, templates)
	require.NoError(t, s.reconcile(t.Context(), key))
	ref := s.collections.states.GetKey(key).DesiredActorTemplate.Metadata
	observed := s.actors.(*sandboxTestActors).templates[ref.Atespace+"/"+ref.Name]
	observed.Status = sandboxGoldenStatus()
	require.NoError(t, s.reconcile(t.Context(), key))
	waitFor(t, func() bool { return s.collections.states.GetKey(key).Failure == nil })
	observed.Containers[0].Image = "unexpected"
	require.ErrorContains(t, s.reconcile(t.Context(), key), "immutable inputs disagree")
	waitFor(t, func() bool { return s.collections.states.GetKey(key).Failure != nil })
	require.False(t, s.collections.states.GetKey(key).canPrepare())
	require.False(t, sandboxPrepared(s.collections.states.GetKey(key)))
}

var sandboxRetryAt = regexp.MustCompile(`^golden build failed \((\w+)\), retry (\d+) at (\S+)$`)

func startSandboxGoldenFailure(t *testing.T, uids ...string) (*SandboxReconciler, *sandboxTestActors, func() *metav1.Condition, context.CancelFunc) {
	t.Helper()
	s, templates, _ := newSandboxTestReconciler(t, "guest@sha256:"+strings.Repeat("b", 64))
	require.NoError(t, s.reconcile(t.Context(), "team-a/scratch"))
	syncSandboxTemplate(t, s, templates)
	actors := s.actors.(*sandboxTestActors)
	actors.inspect(func() { actors.queuedUIDs, actors.failOnCreate = uids, true })
	ctx, cancel := context.WithCancel(t.Context())
	go func() { assert.NoError(t, s.Start(ctx)) }()
	ready := func() *metav1.Condition {
		t.Helper()
		published, err := s.client.SandboxTemplates("team-a").Get(t.Context(), "scratch", metav1.GetOptions{})
		require.NoError(t, err)
		return apimeta.FindStatusCondition(published.Status.Conditions, "Ready")
	}
	synctest.Wait()
	return s, actors, ready, cancel
}

func retryAt(t *testing.T, condition *metav1.Condition, retry int, earliest, latest time.Time) time.Time {
	t.Helper()
	require.Equal(t, metav1.ConditionFalse, condition.Status)
	require.Equal(t, "ActorTemplateRetrying", condition.Reason)
	match := sandboxRetryAt.FindStringSubmatch(condition.Message)
	require.NotNil(t, match, condition.Message)
	require.Equal(t, "GoldenActorInvalid", match[1], "status names the failure reason without Substrate's text")
	require.Equal(t, fmt.Sprint(retry), match[2])
	at, err := time.Parse(time.RFC3339, match[3])
	require.NoError(t, err)
	require.WithinRange(t, at, earliest.Truncate(time.Second), latest, "the backoff must follow the retry policy with at most 20% jitter")
	return at
}

func TestSandboxPreparationRetriesGoldenFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		begin := time.Now()
		s, actors, ready, cancel := startSandboxGoldenFailure(t, "uid-1", "uid-2", "uid-3")
		defer cancel()
		store := s.store.(*sandboxTestStore)
		first := retryAt(t, ready(), 1, begin.Add(15*time.Second), begin.Add(18*time.Second))

		var gets, records int
		actors.inspect(func() { gets, actors.failOnCreate = actors.gets, false })
		store.mu.Lock()
		records = store.recordCalls
		store.mu.Unlock()
		time.Sleep(5 * time.Second)
		synctest.Wait()
		actors.inspect(func() {
			require.Equal(t, gets, actors.gets, "an armed retry is not polled")
			require.Empty(t, actors.deletedUIDs)
		})
		store.mu.Lock()
		require.Equal(t, records, store.recordCalls, "an armed retry writes nothing")
		store.mu.Unlock()

		time.Sleep(time.Until(first) - time.Second)
		synctest.Wait()
		actors.inspect(func() { require.Empty(t, actors.deletedUIDs, "the failed template stays until its backoff ends") })
		time.Sleep(3 * time.Second)
		synctest.Wait()
		actors.inspect(func() {
			require.Equal(t, []string{"uid-1"}, actors.deletedUIDs, "the delete must name the failed template's UID")
			require.Len(t, actors.templates, 1)
			for _, template := range actors.templates {
				require.Equal(t, "uid-2", template.Metadata.Uid, "the next pass recreates the template under the same name")
			}
			gets = actors.gets
		})
		condition := ready()
		require.Equal(t, "Preparing", condition.Reason)
		require.Equal(t, "rebuilding after golden failure, retry 1; Waiting for the prepared sandbox snapshot", condition.Message)
		time.Sleep(2 * time.Second)
		synctest.Wait()
		actors.inspect(func() { require.Greater(t, actors.gets, gets, "a pending rebuild is polled") })

		actors.inspect(func() {
			for _, template := range actors.templates {
				template.Status = goldenFailed(template).Status
			}
		})
		time.Sleep(2 * time.Second)
		synctest.Wait()
		now := time.Now()
		second := retryAt(t, ready(), 2, now.Add(time.Minute-2*time.Second), now.Add(72*time.Second))
		time.Sleep(time.Until(second) + 2*time.Second)
		synctest.Wait()
		actors.inspect(func() {
			require.Equal(t, []string{"uid-1", "uid-2"}, actors.deletedUIDs)
			for _, template := range actors.templates {
				require.Equal(t, "uid-3", template.Metadata.Uid)
				template.Status = sandboxGoldenStatus()
			}
		})
		time.Sleep(2 * time.Second)
		synctest.Wait()
		condition = ready()
		require.Equal(t, metav1.ConditionTrue, condition.Status)
		require.Equal(t, "Prepared", condition.Reason)
		store.mu.Lock()
		require.True(t, store.ready)
		require.Zero(t, store.golden.goldenAttempts, "a ready revision resets its retries")
		store.mu.Unlock()
		cancel()
		synctest.Wait()
	})
}

func TestSandboxGoldenRetryKeepsRetryingOnDeleteError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		begin := time.Now()
		_, actors, ready, cancel := startSandboxGoldenFailure(t, "uid-1", "uid-2")
		defer cancel()
		actors.inspect(func() { actors.deleteErr = status.Error(codes.Unavailable, "delete failed") })
		at := retryAt(t, ready(), 1, begin.Add(15*time.Second), begin.Add(18*time.Second))
		time.Sleep(time.Until(at) + 2*time.Second)
		synctest.Wait()
		condition := ready()
		require.Equal(t, "ActorTemplateRetrying", condition.Reason, "a failed delete must not hide the golden failure")
		require.Equal(t, "golden build failed (GoldenActorInvalid), retry 1 waits to delete the failed template (Unavailable)", condition.Message)

		var deletes int
		actors.inspect(func() { deletes = len(actors.deletedUIDs) })
		time.Sleep(30 * time.Second)
		synctest.Wait()
		actors.inspect(func() {
			retried := len(actors.deletedUIDs) - deletes
			require.Positive(t, retried, "the delete must be requeued")
			require.Less(t, retried, 10, "a failed delete backs off instead of retrying every poll")
			actors.deleteErr, actors.failOnCreate = nil, false
		})
		time.Sleep(31 * time.Second)
		synctest.Wait()
		actors.inspect(func() {
			for _, template := range actors.templates {
				require.Equal(t, "uid-2", template.Metadata.Uid)
			}
		})
		condition = ready()
		require.Equal(t, "Preparing", condition.Reason)
		require.Equal(t, "rebuilding after golden failure, retry 1; Waiting for the prepared sandbox snapshot", condition.Message)
		cancel()
		synctest.Wait()
	})
}

// A ready runtime may outlive an exhausted status queue. Recovery must not need
// another Kubernetes edit or backend readiness change to publish Ready.
func TestSandboxPendingStatusRecoversWithoutGraphEvent(t *testing.T) {
	s, templates, _ := newSandboxTestReconciler(t, "guest@sha256:"+strings.Repeat("b", 64))
	const key = "team-a/scratch"
	require.NoError(t, s.reconcile(t.Context(), key))
	syncSandboxTemplate(t, s, templates)
	s.actors.(*sandboxTestActors).autoReady = true
	require.NoError(t, s.reconcile(t.Context(), key))
	waitFor(t, func() bool { return sandboxPrepared(s.collections.states.GetKey(key)) })
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

func TestSandboxPreparationPollFollowsGoldenState(t *testing.T) {
	s, templates, _ := newSandboxTestReconciler(t, "guest@sha256:"+strings.Repeat("b", 64))
	const key = "team-a/scratch"
	require.NoError(t, s.reconcile(t.Context(), key))
	syncSandboxTemplate(t, s, templates)
	polls := func() bool { return needsPreparationPoll(*s.collections.states.GetKey(key), time.Now()) }
	require.True(t, polls(), "a preparable template is polled before its first observation")

	require.NoError(t, s.reconcile(t.Context(), key))
	ref := s.collections.states.GetKey(key).DesiredActorTemplate.Metadata
	observed := s.actors.(*sandboxTestActors).templates[ref.Atespace+"/"+ref.Name]
	observed.Status = sandboxGoldenStatus()
	require.NoError(t, s.reconcile(t.Context(), key))
	waitFor(t, func() bool { return !polls() })

	observed.Status = nil
	require.NoError(t, s.reconcile(t.Context(), key))
	waitFor(t, polls)

	observed.Status = &ateapipb.ActorTemplateStatus{GoldenSnapshotStatus: &ateapipb.GoldenSnapshotStatus{ErrorMessage: "snapshot backend unavailable"}}
	require.NoError(t, s.reconcile(t.Context(), key))
	waitFor(t, func() bool { return s.collections.states.GetKey(key).Failure != nil })
	require.False(t, polls(), "a golden retry waiting for its backoff is not polled")
}

func sandboxPrepared(state *sandboxReconciliation) bool {
	return apimeta.IsStatusConditionTrue(sandboxStatusWithTransitionTimes(*state).Conditions, "Ready")
}

func TestSandboxPreparationPollSkipsOnlyWaitingGoldenRetries(t *testing.T) {
	now := time.Now()
	waiting := goldenProgress{phase: goldenRetrying, message: "waiting", nextCheck: now.Add(time.Minute)}
	due := goldenProgress{phase: goldenRetrying, message: "due", nextCheck: now.Add(-time.Second)}
	prepareFailed := &ReconciliationFailure{Reason: sandboxPreparationFailed, Message: sandboxPreparationFailureMessage, Retryable: true}
	deleted := metav1.NewTime(now)
	finalized := metav1.ObjectMeta{Finalizers: []string{sandboxPreparationFinalizer}}
	for _, test := range []struct {
		name     string
		template metav1.ObjectMeta
		golden   goldenProgress
		failure  *ReconciliationFailure
		want     bool
	}{
		{name: "waiting retry", template: finalized, golden: waiting},
		{name: "due retry", template: finalized, golden: due, want: true},
		{name: "missing finalizer", template: metav1.ObjectMeta{}, golden: waiting, want: true},
		{name: "deleting during a waiting retry", template: metav1.ObjectMeta{DeletionTimestamp: &deleted, Finalizers: []string{sandboxPreparationFinalizer}}, golden: waiting, want: true},
		{name: "not prepared yet", template: finalized, want: true},
		{name: "failed preparation stays on the queue backoff", template: finalized, failure: prepareFailed},
	} {
		t.Run(test.name, func(t *testing.T) {
			failure := test.failure
			if failure == nil {
				failure = test.golden.failure()
			}
			state := sandboxReconciliation{
				Template: &kagentv1alpha3.SandboxTemplate{ObjectMeta: test.template}, DesiredActorTemplate: &ateapipb.ActorTemplate{},
				Failure: failure, Golden: test.golden,
			}
			assert.Equal(t, test.want, needsPreparationPoll(state, now))
		})
	}
}

func (s *sandboxTestStore) GetSandboxEgressGrant(_ context.Context, namespace, uid string) (egress.Access, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.desired.Namespace == namespace && s.desired.SandboxTemplateUID == uid && !s.retired {
		return s.desired.EgressGrant, nil
	}
	return egress.Access{}, nil
}
