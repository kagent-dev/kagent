package controller

import (
	"context"
	"errors"
	"fmt"
	"testing"

	a2apb "github.com/a2aproject/a2a-go/v2/a2apb/v1"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"istio.io/istio/pkg/kube/krt"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestScheduledRunReconcilerRetiresOrphanedProjections(t *testing.T) {
	ctx := t.Context()
	dsn := dbtest.StartT(ctx, t)
	dbtest.MigrateT(t, dsn, false)
	db, err := database.Connect(ctx, &database.PostgresConfig{Pool: dbtest.PoolConfig(dsn)})
	require.NoError(t, err)
	t.Cleanup(db.Close)
	store := database.NewClient(db, "public")
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	for i, test := range []struct {
		name        string
		wantRetired bool
	}{
		{name: "deleted while offline", wantRetired: true},
		{name: "replaced while offline", wantRetired: true},
		{name: "watch has not observed replacement", wantRetired: true},
		{name: "delayed delete event for live name"},
		{name: "API unavailable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			id := uuid.New()
			ref := types.NamespacedName{Namespace: fmt.Sprintf("team-%d", i), Name: "daily"}
			paused := true
			projection := &database.ScheduledRun{
				ID: id.String(), Creator: "scheduler", Generation: 1, ResourceVersion: "1", Ref: ref,
				Spec: v1alpha3.ScheduledRunSpec{AgentRef: corev1.LocalObjectReference{Name: "assistant"}, Schedule: "* * * * *", Prompt: "hello", Paused: &paused}, AgentUID: "agent-uid",
			}
			recordScheduleRevision(t, store, projection)
			_, err := store.SyncScheduledRun(ctx, projection)
			require.NoError(t, err)
			accepted, err := store.TriggerScheduledRun(ctx, id, "operator", "accepted", 1)
			require.NoError(t, err)
			resource := &v1alpha3.ScheduledRun{ObjectMeta: metav1.ObjectMeta{
				Namespace: ref.Namespace, Name: ref.Name, UID: types.UID(id.String()), Generation: 1, ResourceVersion: "1",
			}, Spec: projection.Spec}
			observed := resource.DeepCopy()
			objects := []client.Object{resource}
			var state []ScheduledRunReconciliation
			switch test.name {
			case "deleted while offline":
				objects = nil
			case "replaced while offline", "watch has not observed replacement":
				resource.UID = types.UID(uuid.NewString())
				if test.name == "replaced while offline" {
					observed = resource.DeepCopy()
				}
				projection.ID = string(resource.UID)
				recordScheduleRevision(t, store, projection)
				state = []ScheduledRunReconciliation{{Resource: observed, AgentUID: projection.AgentUID}}
			}
			kube := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(&v1alpha3.ScheduledRun{}).WithObjects(objects...).WithInterceptorFuncs(interceptor.Funcs{
				Get: func(ctx context.Context, reader client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
					if test.name == "API unavailable" {
						return errors.New("API unavailable")
					}
					return reader.Get(ctx, key, obj, opts...)
				},
			}).Build()
			opts := krt.NewOptionsBuilder(t.Context().Done(), test.name, nil)
			states := krt.NewStaticCollection(nil, state, opts.WithName("states")...)
			reconciler := &ScheduledRunReconciler{states: states, store: store, kube: kube, reader: kube, caller: auth.NewCredentialSession(auth.Principal{ID: "scheduler"}, nil)}
			err = reconciler.reconcile(ctx, ref.String())
			if test.name == "API unavailable" {
				require.ErrorContains(t, err, "API unavailable")
			} else {
				require.NoError(t, err)
			}
			previous, err := store.GetScheduledRun(ctx, id, "")
			require.NoError(t, err)
			require.Equal(t, test.wantRetired, previous.DeletedAt != nil)
			if test.name == "watch has not observed replacement" {
				_, err := store.GetActiveScheduledRunID(ctx, ref)
				require.ErrorIs(t, err, database.ErrNotFound, "a stale watch must not recreate the removed UID")
				projection.ID = string(resource.UID)
				recordScheduleRevision(t, store, projection)
				states.UpdateObject(ScheduledRunReconciliation{Resource: resource, AgentUID: projection.AgentUID})
				require.NoError(t, reconciler.reconcile(ctx, ref.String()))
			}
			activeID, err := store.GetActiveScheduledRunID(ctx, ref)
			if len(objects) == 0 {
				require.ErrorIs(t, err, database.ErrNotFound)
			} else {
				require.NoError(t, err)
				require.Equal(t, string(resource.UID), activeID.String(), "a retained old projection must not block a same-name replacement")
			}
			replayed, err := store.TriggerScheduledRun(ctx, id, "operator", "accepted", 0)
			require.NoError(t, err)
			require.Equal(t, accepted.Id, replayed.Id)
		})
	}
}

func recordScheduleRevision(t *testing.T, store *database.Client, schedule *database.ScheduledRun) {
	t.Helper()
	agent := &apiv1.AgentProvenance{Ref: &apiv1.ResourceReference{Namespace: schedule.Ref.Namespace, Name: schedule.Spec.AgentRef.Name}, Uid: schedule.AgentUID}
	require.NoError(t, store.UpsertAgentDefinition(t.Context(), database.AgentDefinition{Namespace: agent.Ref.Namespace, AgentName: agent.Ref.Name, AgentUID: agent.Uid, DesiredRevision: schedule.ID}))
	_, err := store.RecordRuntimeRevision(t.Context(), database.RuntimeRevision{Namespace: agent.Ref.Namespace, AgentName: agent.Ref.Name, AgentUID: agent.Uid, Revision: schedule.ID,
		SourceSnapshot: []byte("{}"), AgentCard: &a2apb.AgentCard{}, EgressDestinations: []string{},
		ActorTemplateAtespace: agent.Ref.Namespace, ActorTemplateName: schedule.ID + "-runtime", ActorTemplateUID: schedule.ID + "-runtime-uid",
	}, database.RecordRevisionOptions{Ready: true})
	require.NoError(t, err)
}
