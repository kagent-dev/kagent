package scheduledrun

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apiMeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	schedules "github.com/kagent-dev/kagent/go/api/scheduledrun"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
)

type fakeReconcilerStore struct {
	applied   *apiv1.ScheduledRun
	requestID string
	applies   int
	applyErr  error
	retired   []string
}

func (f *fakeReconcilerStore) ApplyScheduledRun(_ context.Context, request *apiv1.ScheduledRun, requestID string) (*apiv1.ScheduledRun, error) {
	f.applies++
	if f.applyErr != nil {
		return nil, f.applyErr
	}
	f.applied, f.requestID = proto.CloneOf(request), requestID
	stored := proto.CloneOf(request)
	stored.Id = "11111111-1111-1111-1111-111111111111"
	return stored, nil
}

func (f *fakeReconcilerStore) RetireScheduledRuns(_ context.Context, creator string) error {
	f.retired = append(f.retired, creator)
	return nil
}

func testSchedule() *v1alpha3.ScheduledRun {
	return &v1alpha3.ScheduledRun{
		ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "nightly", UID: types.UID("uid-1"), Generation: 2},
		Spec: v1alpha3.ScheduledRunSpec{
			AgentRef: corev1.LocalObjectReference{Name: "report"},
			Schedule: "0  3 * * *",
			Prompt:   "Summarize yesterday",
		},
	}
}

func testAgent(name string) *v1alpha3.Agent {
	return &v1alpha3.Agent{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: name}}
}

func testKube(t *testing.T, objects ...client.Object) client.Client {
	t.Helper()
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	return fake.NewClientBuilder().WithScheme(scheme).
		WithStatusSubresource(&v1alpha3.ScheduledRun{}).
		WithObjects(objects...).Build()
}

func reconcileOnce(t *testing.T, kube client.Client, store *fakeReconcilerStore, schedule *v1alpha3.ScheduledRun) (*v1alpha3.ScheduledRun, error) {
	t.Helper()
	_, err := NewReconciler(kube, store).Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(schedule)})
	updated := &v1alpha3.ScheduledRun{}
	if getErr := kube.Get(t.Context(), client.ObjectKeyFromObject(schedule), updated); getErr != nil {
		return nil, errors.Join(err, getErr)
	}
	return updated, err
}

func requireCondition(t *testing.T, schedule *v1alpha3.ScheduledRun, conditionType string, status metav1.ConditionStatus, reason string) {
	t.Helper()
	condition := apiMeta.FindStatusCondition(schedule.Status.Conditions, conditionType)
	require.NotNil(t, condition, "condition %s", conditionType)
	require.Equal(t, status, condition.Status, "%s: %s", conditionType, condition.Message)
	require.Equal(t, reason, condition.Reason, "%s: %s", conditionType, condition.Message)
	require.Equal(t, schedule.Generation, condition.ObservedGeneration)
}

func TestReconcilePersistsDeclaredSchedule(t *testing.T) {
	schedule := testSchedule()
	schedule.Spec.TimeZone = new("Europe/Paris")
	schedule.Spec.ExecutionTimeout = &metav1.Duration{Duration: time.Minute}
	store := &fakeReconcilerStore{}
	kube := testKube(t, schedule, testAgent("report"))

	updated, err := reconcileOnce(t, kube, store, schedule)
	require.NoError(t, err)
	require.Equal(t, "uid-1", store.requestID)
	require.Equal(t, schedules.KubernetesCreator("team-a", "nightly"), store.applied.Creator)
	require.True(t, proto.Equal(&apiv1.ResourceReference{Namespace: "team-a", Name: "report"}, store.applied.Agent))
	require.True(t, proto.Equal(&apiv1.ScheduledRunConfig{
		Schedule: "0 3 * * *", TimeZone: "Europe/Paris", Prompt: "Summarize yesterday",
		ExecutionTimeout: store.applied.Config.ExecutionTimeout,
	}, store.applied.Config))
	require.Equal(t, time.Minute, store.applied.Config.ExecutionTimeout.AsDuration())
	require.Equal(t, schedule.Generation, updated.Status.ObservedGeneration)
	requireCondition(t, updated, v1alpha3.ScheduledRunConditionAccepted, metav1.ConditionTrue, "Accepted")
	requireCondition(t, updated, v1alpha3.ScheduledRunConditionResolvedRefs, metav1.ConditionTrue, "ResolvedRefs")
	requireCondition(t, updated, v1alpha3.ScheduledRunConditionReady, metav1.ConditionTrue, "Scheduled")
	require.Contains(t, apiMeta.FindStatusCondition(updated.Status.Conditions, v1alpha3.ScheduledRunConditionReady).Message, "11111111-1111-1111-1111-111111111111")
}

func TestReconcileAppliesDefaultsAndPause(t *testing.T) {
	schedule := testSchedule()
	schedule.Spec.Paused = new(true)
	store := &fakeReconcilerStore{}
	updated, err := reconcileOnce(t, testKube(t, schedule, testAgent("report")), store, schedule)
	require.NoError(t, err)
	require.Equal(t, "UTC", store.applied.Config.TimeZone)
	require.Equal(t, 15*time.Minute, store.applied.Config.ExecutionTimeout.AsDuration())
	require.True(t, store.applied.Config.Paused)
	require.Contains(t, apiMeta.FindStatusCondition(updated.Status.Conditions, v1alpha3.ScheduledRunConditionReady).Message, "paused")
}

func TestReconcileReportsMissingAgentButPersists(t *testing.T) {
	schedule := testSchedule()
	store := &fakeReconcilerStore{}
	updated, err := reconcileOnce(t, testKube(t, schedule), store, schedule)
	require.NoError(t, err)
	require.Equal(t, 1, store.applies)
	requireCondition(t, updated, v1alpha3.ScheduledRunConditionResolvedRefs, metav1.ConditionFalse, "AgentNotFound")
	requireCondition(t, updated, v1alpha3.ScheduledRunConditionReady, metav1.ConditionTrue, "Scheduled")
}

func TestReconcileRejectsInvalidScheduleWithoutTouchingStore(t *testing.T) {
	for name, mutate := range map[string]func(*v1alpha3.ScheduledRunSpec){
		"cron":      func(spec *v1alpha3.ScheduledRunSpec) { spec.Schedule = "61 * * * *" },
		"time zone": func(spec *v1alpha3.ScheduledRunSpec) { spec.TimeZone = new("Mars/Olympus") },
		"local":     func(spec *v1alpha3.ScheduledRunSpec) { spec.TimeZone = new("Local") },
		"timeout": func(spec *v1alpha3.ScheduledRunSpec) {
			spec.ExecutionTimeout = &metav1.Duration{Duration: time.Nanosecond}
		},
	} {
		t.Run(name, func(t *testing.T) {
			schedule := testSchedule()
			mutate(&schedule.Spec)
			store := &fakeReconcilerStore{}
			updated, err := reconcileOnce(t, testKube(t, schedule, testAgent("report")), store, schedule)
			require.NoError(t, err)
			require.Zero(t, store.applies)
			requireCondition(t, updated, v1alpha3.ScheduledRunConditionAccepted, metav1.ConditionFalse, "InvalidSchedule")
			requireCondition(t, updated, v1alpha3.ScheduledRunConditionReady, metav1.ConditionFalse, "NotAccepted")
		})
	}
}

func TestReconcileSurfacesStoreFailure(t *testing.T) {
	schedule := testSchedule()
	store := &fakeReconcilerStore{applyErr: errors.New("database unavailable")}
	updated, err := reconcileOnce(t, testKube(t, schedule, testAgent("report")), store, schedule)
	require.ErrorContains(t, err, "database unavailable")
	requireCondition(t, updated, v1alpha3.ScheduledRunConditionAccepted, metav1.ConditionTrue, "Accepted")
	requireCondition(t, updated, v1alpha3.ScheduledRunConditionReady, metav1.ConditionFalse, "PersistenceFailed")
}

func TestReconcileRetiresDeletedObject(t *testing.T) {
	store := &fakeReconcilerStore{}
	_, err := NewReconciler(testKube(t), store).Reconcile(t.Context(), ctrl.Request{NamespacedName: types.NamespacedName{Namespace: "team-a", Name: "nightly"}})
	require.NoError(t, err)
	require.Equal(t, []string{schedules.KubernetesCreator("team-a", "nightly")}, store.retired)
	require.Zero(t, store.applies)
}

func TestRequestsForAgentSelectsReferencingSchedules(t *testing.T) {
	referencing := testSchedule()
	other := testSchedule()
	other.Name, other.Spec.AgentRef = "weekly", corev1.LocalObjectReference{Name: "other"}
	elsewhere := testSchedule()
	elsewhere.Namespace = "team-b"
	kube := testKube(t, referencing, other, elsewhere)
	requests := NewReconciler(kube, &fakeReconcilerStore{}).requestsForAgent(t.Context(), testAgent("report"))
	require.Equal(t, []ctrl.Request{{NamespacedName: client.ObjectKeyFromObject(referencing)}}, requests)
}

func TestReconcileAddsFinalizerBeforePersisting(t *testing.T) {
	schedule := testSchedule()
	store := &fakeReconcilerStore{}
	updated, err := reconcileOnce(t, testKube(t, schedule, testAgent("report")), store, schedule)
	require.NoError(t, err)
	require.Contains(t, updated.Finalizers, retirementFinalizer)
	require.Equal(t, 1, store.applies)
}

// A deletion that happened while no controller ran is still pending on restart:
// the finalizer holds the object, so the first reconcile retires the row.
func TestReconcileRetiresObjectDeletedWhileDown(t *testing.T) {
	schedule := testSchedule()
	schedule.Finalizers = []string{retirementFinalizer}
	schedule.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	store := &fakeReconcilerStore{}
	kube := testKube(t, schedule)
	_, err := NewReconciler(kube, store).Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(schedule)})
	require.NoError(t, err)
	require.Equal(t, []string{schedules.KubernetesCreator("team-a", "nightly")}, store.retired)
	require.Zero(t, store.applies)
	// Removing the last finalizer lets the API server delete the object.
	require.True(t, apierrors.IsNotFound(kube.Get(t.Context(), client.ObjectKeyFromObject(schedule), &v1alpha3.ScheduledRun{})))
}

type failingRetireStore struct{ fakeReconcilerStore }

func (*failingRetireStore) RetireScheduledRuns(context.Context, string) error {
	return errors.New("database unavailable")
}

func TestReconcileKeepsFinalizerWhenRetirementFails(t *testing.T) {
	schedule := testSchedule()
	schedule.Finalizers = []string{retirementFinalizer}
	schedule.DeletionTimestamp = &metav1.Time{Time: time.Now()}
	kube := testKube(t, schedule)
	_, err := NewReconciler(kube, &failingRetireStore{}).Reconcile(t.Context(), ctrl.Request{NamespacedName: client.ObjectKeyFromObject(schedule)})
	require.ErrorContains(t, err, "database unavailable")
	current := &v1alpha3.ScheduledRun{}
	require.NoError(t, kube.Get(t.Context(), client.ObjectKeyFromObject(schedule), current))
	require.Contains(t, current.Finalizers, retirementFinalizer)
}
