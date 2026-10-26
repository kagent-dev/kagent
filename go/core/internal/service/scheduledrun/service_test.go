package scheduledrun_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/authorization"
	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/scheduledrun"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type policy struct {
	auth.NoopAuthorizer
	deny    string
	checked []auth.Resource
	none    bool
}

func (p *policy) Check(_ context.Context, _ auth.Principal, _ auth.Verb, resource auth.Resource) error {
	p.checked = append(p.checked, resource)
	if resource.Type == p.deny {
		return errors.New("denied")
	}
	return nil
}
func (p *policy) Scope(context.Context, auth.Principal, auth.Verb, string) (authorization.AuthorizationScope, error) {
	if p.none {
		return authorization.AuthorizationScope{Kind: authorization.ScopeNone}, nil
	}
	return authorization.AuthorizationScope{Kind: authorization.ScopeAll}, nil
}
func caller(t *testing.T) context.Context {
	t.Helper()
	return auth.AuthSessionTo(t.Context(), auth.NewCredentialSession(auth.Principal{ID: "operator"}, nil))
}
func resource() *v1alpha3.ScheduledRun {
	return &v1alpha3.ScheduledRun{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "daily", UID: types.UID(uuid.NewString()), Generation: 1}, Spec: v1alpha3.ScheduledRunSpec{AgentRef: corev1.LocalObjectReference{Name: "assistant"}, Schedule: "0 9 * * *", Prompt: "Report"}}
}
func scheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	result := runtime.NewScheme()
	require.NoError(t, v1alpha3.AddToScheme(result))
	return result
}
func TestConfigurationUsesKubernetesAndPreservesConcurrency(t *testing.T) {
	ctx := caller(t)
	object := resource()
	kube := fake.NewClientBuilder().WithScheme(scheme(t)).WithStatusSubresource(&v1alpha3.ScheduledRun{}).Build()
	svc := scheduledrun.NewService(nil, kube, &policy{}, nil)
	created, err := svc.Create(ctx, object)
	require.NoError(t, err)
	require.NotEmpty(t, created.ResourceVersion)
	stale := created.DeepCopy()
	changed := created.DeepCopy()
	changed.Spec.Prompt = "Updated"
	updated, err := svc.Update(ctx, changed)
	require.NoError(t, err)
	require.NotEqual(t, created.ResourceVersion, updated.ResourceVersion)
	_, err = svc.Update(ctx, stale)
	require.Error(t, err)
	require.Error(t, svc.Delete(ctx, types.NamespacedName{Namespace: "team", Name: "daily"}, created.UID, created.ResourceVersion))
	require.NoError(t, svc.Delete(ctx, types.NamespacedName{Namespace: "team", Name: "daily"}, updated.UID, updated.ResourceVersion))
	_, err = svc.Get(ctx, types.NamespacedName{Namespace: "team", Name: "daily"})
	require.Error(t, err)
}
func TestConfigurationAuthorizationAndInvalidSchedule(t *testing.T) {
	for _, kind := range []string{"ScheduledRun", "Agent", "Session"} {
		t.Run(kind, func(t *testing.T) {
			kube := fake.NewClientBuilder().WithScheme(scheme(t)).Build()
			p := &policy{deny: kind}
			svc := scheduledrun.NewService(nil, kube, p, nil)
			_, err := svc.Create(caller(t), resource())
			objects := &v1alpha3.ScheduledRunList{}
			require.NoError(t, kube.List(t.Context(), objects))
			if kind == "ScheduledRun" {
				require.Error(t, err)
				require.Empty(t, objects.Items)
			} else {
				require.NoError(t, err, "embedded schedules require no Agent or Session creation authority")
				require.Len(t, objects.Items, 1)
			}
			require.Equal(t, "team", p.checked[len(p.checked)-1].Namespace)
		})
	}
	svc := scheduledrun.NewService(nil, fake.NewClientBuilder().WithScheme(scheme(t)).Build(), &policy{}, nil)
	_, err := svc.Create(caller(t), nil)
	require.Error(t, err)
	_, err = svc.Update(caller(t), nil)
	require.Error(t, err)
	invalid := resource()
	invalid.Spec.Schedule = "@daily"
	_, err = svc.Create(caller(t), invalid)
	require.Error(t, err)
}

type executionStore struct {
	*database.Client
	schedule   *database.ScheduledRun
	execution  *apiv1.ScheduledRunExecution
	initiator  string
	generation int64
	listed     bool
}

func (s *executionStore) GetScheduledRun(context.Context, uuid.UUID, string) (*database.ScheduledRun, error) {
	return s.schedule, nil
}
func (s *executionStore) TriggerScheduledRun(_ context.Context, _ uuid.UUID, initiator, _ string, generation int64) (*apiv1.ScheduledRunExecution, error) {
	s.initiator, s.generation = initiator, generation
	return s.execution, nil
}
func (s *executionStore) GetScheduledRunExecution(context.Context, uuid.UUID, string) (*apiv1.ScheduledRunExecution, error) {
	return s.execution, nil
}
func (s *executionStore) ListScheduledRunExecutions(context.Context, database.ScheduledRunExecutionQuery) ([]*apiv1.ScheduledRunExecution, error) {
	s.listed = true
	return []*apiv1.ScheduledRunExecution{s.execution}, nil
}

func TestManualTriggerUsesSchedulerAndSeparateInitiator(t *testing.T) {
	object := resource()
	ref := &apiv1.ResourceReference{Namespace: object.Namespace, Name: object.Name}
	store := &executionStore{schedule: &database.ScheduledRun{ID: string(object.UID), Creator: "machine", Ref: types.NamespacedName{Namespace: object.Namespace, Name: object.Name}, Spec: object.Spec, Generation: 1, AgentUID: "agent-uid"}, execution: &apiv1.ScheduledRunExecution{Creator: "machine", ScheduledRun: ref}}
	svc := scheduledrun.NewService(store, fake.NewClientBuilder().WithScheme(scheme(t)).WithObjects(object).Build(), &policy{}, nil)
	execution, err := svc.Trigger(caller(t), uuid.MustParse(string(object.UID)), "manual")
	require.NoError(t, err)
	require.Equal(t, "machine", execution.Creator)
	require.Equal(t, "operator", store.initiator)
	require.Equal(t, int64(1), store.generation)
}
func TestRetainedHistoryHasIndependentAuthorizationBeforePaging(t *testing.T) {
	ref := &apiv1.ResourceReference{Namespace: "team", Name: "daily"}
	store := &executionStore{schedule: &database.ScheduledRun{Ref: types.NamespacedName{Namespace: ref.Namespace, Name: ref.Name}, AgentUID: "agent-uid"}, execution: &apiv1.ScheduledRunExecution{ScheduledRun: proto.CloneOf(ref)}}
	p := &policy{deny: "ScheduledRunHistory", none: true}
	svc := scheduledrun.NewService(store, fake.NewClientBuilder().WithScheme(scheme(t)).Build(), p, nil)
	_, err := svc.GetExecution(caller(t), uuid.New())
	require.Error(t, err)
	require.Equal(t, auth.Resource{Type: "ScheduledRunHistory", Namespace: "team", Name: "daily"}, p.checked[0])
	rows, _, err := svc.ListExecutions(caller(t), uuid.New(), nil)
	require.NoError(t, err)
	require.Empty(t, rows)
	require.False(t, store.listed)
	p.deny, p.none = "", false
	rows, _, err = svc.ListExecutions(caller(t), uuid.New(), nil)
	require.NoError(t, err)
	require.Len(t, rows, 1, "history does not require a live CRD")
}

func TestTriggerRejectsUnwatchedNamespace(t *testing.T) {
	object := resource()
	store := &executionStore{schedule: &database.ScheduledRun{ID: string(object.UID), Ref: types.NamespacedName{Namespace: object.Namespace, Name: object.Name}, AgentUID: "agent-uid"}}
	svc := scheduledrun.NewService(store, fake.NewClientBuilder().WithScheme(scheme(t)).Build(), &policy{}, []string{"other-namespace"})
	_, err := svc.Trigger(caller(t), uuid.MustParse(string(object.UID)), "manual")
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeFailedPrecondition))
	require.Empty(t, store.initiator)
}
