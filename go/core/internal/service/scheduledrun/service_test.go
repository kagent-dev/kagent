package scheduledrun_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/scheduledrun"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

type session struct{}

func (session) Principal() auth.Principal { return auth.Principal{User: auth.User{ID: "alice"}} }

type authorizer struct{ deny string }

func (a authorizer) Check(_ context.Context, _ auth.Principal, _ auth.Verb, r auth.Resource) error {
	if r.Type == a.deny {
		return errors.New("denied")
	}
	return nil
}

type missingRequestStore struct{ *database.Client }

func (missingRequestStore) FindScheduledRunRequest(context.Context, string, string, []byte) (*apiv1alpha1.ScheduledRun, error) {
	return nil, database.ErrNotFound
}

func TestScheduleAuthorizationFailsBeforePersistence(t *testing.T) {
	owner := auth.AuthSessionTo(t.Context(), session{})
	shared := auth.ShareContextTo(owner, &auth.ShareContext{AgentInstanceID: "instance", UserID: "other-owner"})
	for _, tc := range []struct {
		name string
		ctx  context.Context
		deny string
		want serviceerrors.Code
	}{
		{"anonymous", t.Context(), "", serviceerrors.CodeUnauthenticated},
		{"instance share", shared, "", serviceerrors.CodePermissionDenied},
		{"schedule denied", owner, "ScheduledRun", serviceerrors.CodePermissionDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := scheduledrun.NewService(nil, nil, authorizer{deny: tc.deny})
			_, err := svc.Get(tc.ctx, uuid.New())
			require.Equal(t, tc.want, serviceerrors.CodeOf(err))
		})
	}
}

func TestTargetPermissionAndExistence(t *testing.T) {
	owner := auth.AuthSessionTo(t.Context(), session{})
	scheme := runtime.NewScheme()
	require.NoError(t, v1alpha3.AddToScheme(scheme))
	kube := fake.NewClientBuilder().WithScheme(scheme).Build()
	request := scheduledrun.CreateRequest{Harness: &apiv1alpha1.ResourceReference{Namespace: "team", Name: "harness"}, AgentTemplate: &apiv1alpha1.ResourceReference{Namespace: "team", Name: "template"}, RequestID: "id", Config: &apiv1alpha1.ScheduledRunConfig{Schedule: "* * * * *", Prompt: "run"}}
	for _, denied := range []string{"Harness", "AgentTemplate", "AgentInstance"} {
		svc := scheduledrun.NewService(missingRequestStore{}, kube, authorizer{deny: denied})
		_, err := svc.Create(owner, request)
		require.Equal(t, serviceerrors.CodePermissionDenied, serviceerrors.CodeOf(err))
	}
	svc := scheduledrun.NewService(missingRequestStore{}, kube, authorizer{})
	_, err := svc.Create(owner, request)
	require.Equal(t, serviceerrors.CodeFailedPrecondition, serviceerrors.CodeOf(err))
}
