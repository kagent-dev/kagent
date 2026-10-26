package scheduledrun_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/dbtest"
	"github.com/kagent-dev/kagent/go/core/internal/service/scheduledrun"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type reconcileDuringAuthorization struct {
	auth.NoopAuthorizer
	onAuthorization func()
	checked         []string
}

func (a *reconcileDuringAuthorization) Check(_ context.Context, _ auth.Principal, _ auth.Verb, r auth.Resource) error {
	if r.Type == "ScheduledRunInvocation" {
		a.checked = append(a.checked, r.Name)
		a.onAuthorization()
	}
	return nil
}

func TestTriggerRejectsGenerationChangedDuringAuthorization(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(fmt.Sprintf("initially-disabled=%t", disabled), func(t *testing.T) {
			ctx := t.Context()
			dsn := dbtest.StartT(context.Background(), t)
			dbtest.MigrateT(t, dsn, false)
			db, err := database.Connect(ctx, &database.PostgresConfig{Pool: dbtest.PoolConfig(dsn)})
			require.NoError(t, err)
			t.Cleanup(db.Close)
			store := database.NewClient(db, "public")
			id := uuid.New()
			schedule := &database.ScheduledRun{
				ID: id.String(), Creator: "scheduler", Generation: 1, ResourceVersion: "1",
				Ref:  types.NamespacedName{Namespace: "team", Name: "daily"},
				Spec: v1alpha3.ScheduledRunSpec{AgentRef: corev1.LocalObjectReference{Name: "assistant"}, Schedule: "0 9 * * *", Prompt: "old"}, AgentUID: "agent-uid",
			}
			_, err = store.SyncScheduledRun(ctx, schedule)
			require.NoError(t, err)
			if disabled {
				// A rejected generation retains the previously valid target until its
				// missing dependency is restored, without another CRD generation change.
				require.NoError(t, store.DisableScheduledRun(ctx, id, 2))
			}
			replacement := *schedule
			replacement.Spec = *schedule.Spec.DeepCopy()
			replacement.Spec.AgentRef.Name = "agent-b"
			replacement.Generation, replacement.ResourceVersion, replacement.Spec.Prompt = 2, "2", "new target"
			resource := &v1alpha3.ScheduledRun{ObjectMeta: metav1.ObjectMeta{Namespace: "team", Name: "daily", UID: types.UID(id.String()), Generation: 2, ResourceVersion: "2"}, Spec: replacement.Spec}
			scheme := runtime.NewScheme()
			require.NoError(t, v1alpha3.AddToScheme(scheme))
			kube := fake.NewClientBuilder().WithScheme(scheme).WithObjects(resource).Build()
			authorizer := &reconcileDuringAuthorization{onAuthorization: func() {
				// Model reconciliation committing after the initial projection read.
				_, err := store.SyncScheduledRun(ctx, &replacement)
				require.NoError(t, err)
			}}
			svc := scheduledrun.NewService(store, kube, authorizer, nil)
			caller := auth.AuthSessionTo(ctx, auth.NewCredentialSession(auth.Principal{ID: "operator"}, nil))
			execution, err := svc.Trigger(caller, id, "race")
			require.Error(t, err)
			require.Nil(t, execution)
			require.Equal(t, []string{"daily"}, authorizer.checked)
			executions, err := store.ListScheduledRunExecutions(ctx, database.ScheduledRunExecutionQuery{ScheduledRunID: id, ScheduledRunQuery: database.ScheduledRunQuery{Limit: 10}})
			require.NoError(t, err)
			require.Empty(t, executions, "rejected authorization race must not leave an accepted execution")
		})
	}
}
