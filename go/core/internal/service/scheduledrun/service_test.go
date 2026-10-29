package scheduledrun_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/service/scheduledrun"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type session struct{}

func (session) Principal() auth.Principal { return auth.Principal{ID: "alice"} }

type authorizer struct {
	deny  string
	check func(auth.Resource)
}

func (a authorizer) Check(_ context.Context, _ auth.Principal, _ auth.Verb, r auth.Resource) error {
	if a.check != nil {
		a.check(r)
	}
	if r.Type == a.deny {
		return errors.New("denied")
	}
	return nil
}

type missingRequestStore struct{ *database.Client }

func (missingRequestStore) FindScheduledRunRequest(context.Context, string, string, []byte) (*apiv1.ScheduledRun, error) {
	return nil, database.ErrNotFound
}

func TestScheduleAuthorizationFailsBeforePersistence(t *testing.T) {
	owner := auth.AuthSessionTo(t.Context(), session{})
	shared := auth.ShareContextTo(owner, &auth.ShareContext{SessionID: "session", UserID: "other-owner"})
	for _, tc := range []struct {
		name string
		ctx  context.Context
		deny string
		want serviceerrors.Code
	}{
		{"anonymous", t.Context(), "", serviceerrors.CodeUnauthenticated},
		{"session share", shared, "", serviceerrors.CodePermissionDenied},
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
	request := scheduledrun.CreateRequest{Agent: &apiv1.ResourceReference{Namespace: "team", Name: "template"}, RequestID: "id", Config: &apiv1.ScheduledRunConfig{Schedule: "* * * * *", Prompt: "run"}}
	for _, denied := range []string{"Agent", "Session"} {
		svc := scheduledrun.NewService(missingRequestStore{}, kube, authorizer{deny: denied})
		_, err := svc.Create(owner, request)
		require.Equal(t, serviceerrors.CodePermissionDenied, serviceerrors.CodeOf(err))
	}
	svc := scheduledrun.NewService(missingRequestStore{}, kube, authorizer{check: func(resource auth.Resource) {
		if resource.Type == "Agent" {
			require.Equal(t, "team", resource.Namespace)
			require.Equal(t, "template", resource.Name)
		}
	}})
	_, err := svc.Create(owner, request)
	require.Equal(t, serviceerrors.CodeFailedPrecondition, serviceerrors.CodeOf(err))
}

type scheduleOperationPolicy struct {
	verb     auth.Verb
	readOnly bool
	last     auth.Verb
}

func (p *scheduleOperationPolicy) Check(_ context.Context, _ auth.Principal, verb auth.Verb, resource auth.Resource) error {
	p.last = verb
	if resource.Type != "ScheduledRun" || (!p.readOnly && p.verb != verb) || (p.readOnly && verb != auth.VerbGet && verb != auth.VerbList) {
		return errors.New("denied")
	}
	return nil
}
func TestScheduleAndExecutionOperationsRejectDeniedCallers(t *testing.T) {
	id := uuid.New()
	for _, op := range []struct {
		name string
		verb auth.Verb
		call func(context.Context, *scheduledrun.Service) error
	}{
		{"create", auth.VerbCreate, func(ctx context.Context, s *scheduledrun.Service) error {
			_, err := s.Create(ctx, scheduledrun.CreateRequest{})
			return err
		}},
		{"get", auth.VerbGet, func(ctx context.Context, s *scheduledrun.Service) error { _, err := s.Get(ctx, id); return err }},
		{"list", auth.VerbList, func(ctx context.Context, s *scheduledrun.Service) error { _, _, err := s.List(ctx, nil); return err }},
		{"update", auth.VerbUpdate, func(ctx context.Context, s *scheduledrun.Service) error {
			_, err := s.Update(ctx, id, "etag", nil)
			return err
		}},
		{"delete", auth.VerbDelete, func(ctx context.Context, s *scheduledrun.Service) error { _, err := s.Delete(ctx, id); return err }},
		{"trigger", auth.VerbCreate, func(ctx context.Context, s *scheduledrun.Service) error {
			_, err := s.Trigger(ctx, id, "request")
			return err
		}},
		{"get execution", auth.VerbGet, func(ctx context.Context, s *scheduledrun.Service) error {
			_, err := s.GetExecution(ctx, id)
			return err
		}},
		{"list executions", auth.VerbList, func(ctx context.Context, s *scheduledrun.Service) error {
			_, _, err := s.ListExecutions(ctx, id, nil)
			return err
		}},
	} {
		t.Run(op.name, func(t *testing.T) {
			policy := &scheduleOperationPolicy{}
			ctx := auth.AuthSessionTo(t.Context(), session{})
			svc := scheduledrun.NewService(nil, nil, policy)
			require.True(t, serviceerrors.IsCode(op.call(ctx, svc), serviceerrors.CodePermissionDenied))
			require.Equal(t, op.verb, policy.last)
			require.True(t, serviceerrors.IsCode(op.call(auth.ShareContextTo(ctx, &auth.ShareContext{SessionID: id.String(), UserID: "alice"}), svc), serviceerrors.CodePermissionDenied))
			require.True(t, serviceerrors.IsCode(op.call(t.Context(), svc), serviceerrors.CodeUnauthenticated))
			if op.verb != auth.VerbGet && op.verb != auth.VerbList {
				require.True(t, serviceerrors.IsCode(op.call(ctx, scheduledrun.NewService(nil, nil, &scheduleOperationPolicy{readOnly: true})), serviceerrors.CodePermissionDenied))
			}
		})
	}
}

type existingScheduleStore struct {
	*database.Client
	schedule *apiv1.ScheduledRun
	triggers int
	updates  int
}

func (s *existingScheduleStore) FindScheduledRunRequest(context.Context, string, string, []byte) (*apiv1.ScheduledRun, error) {
	return s.schedule, nil
}
func (s *existingScheduleStore) GetScheduledRun(_ context.Context, _ uuid.UUID, owner string) (*apiv1.ScheduledRun, error) {
	if owner != s.schedule.Creator {
		return nil, database.ErrNotFound
	}
	return s.schedule, nil
}
func (s *existingScheduleStore) TriggerScheduledRun(context.Context, uuid.UUID, string, string) (*apiv1.ScheduledRunExecution, error) {
	s.triggers++
	return &apiv1.ScheduledRunExecution{}, nil
}
func TestScheduleRetriesAndTriggersRecheckTargetGrants(t *testing.T) {
	ctx := auth.AuthSessionTo(t.Context(), session{})
	target := &apiv1.ResourceReference{Namespace: "team", Name: "assistant"}
	for _, denied := range []string{"Agent", "Session"} {
		store := &existingScheduleStore{schedule: &apiv1.ScheduledRun{Agent: target, Creator: "alice"}}
		svc := scheduledrun.NewService(store, nil, authorizer{deny: denied})
		_, err := svc.Create(ctx, scheduledrun.CreateRequest{Agent: target, RequestID: "retry", Config: &apiv1.ScheduledRunConfig{Schedule: "* * * * *", Prompt: "run"}})
		require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))
		_, err = svc.Trigger(ctx, uuid.New(), "manual")
		require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))
		require.Zero(t, store.triggers)
	}
}

type scheduleNamespacePolicy struct {
	namespace string
	check     func(auth.Verb, auth.Resource)
}

func (s scheduleNamespacePolicy) Check(_ context.Context, _ auth.Principal, verb auth.Verb, resource auth.Resource) error {
	s.check(verb, resource)
	if verb == auth.VerbCreate && resource.Type == "Session" && resource.Namespace != s.namespace {
		return errors.New("Session creation denied in target namespace")
	}
	return nil
}

func (s *existingScheduleStore) UpdateScheduledRun(_ context.Context, _ uuid.UUID, _ string, _ string, config *apiv1.ScheduledRunConfig) (*apiv1.ScheduledRun, error) {
	s.updates++
	s.schedule.Config = config
	return s.schedule, nil
}

func TestScheduleTargetChecksUseSessionCreateNamespace(t *testing.T) {
	for _, operation := range []string{"create", "retry", "update", "trigger"} {
		for _, allowed := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s/allowed=%v", operation, allowed), func(t *testing.T) {
				target := &apiv1.ResourceReference{Namespace: "team", Name: "assistant"}
				store := &existingScheduleStore{schedule: &apiv1.ScheduledRun{Agent: target, Creator: "alice"}}
				namespace := "team"
				if !allowed {
					namespace = "other"
				}
				checks := 0
				policy := scheduleNamespacePolicy{namespace: namespace, check: func(verb auth.Verb, resource auth.Resource) {
					if resource.Type == "Session" {
						checks++
						require.Equal(t, auth.VerbCreate, verb)
						require.Equal(t, auth.Resource{Type: "Session", Namespace: target.Namespace}, resource)
					}
					if resource.Type == "ScheduledRun" {
						require.Empty(t, resource.Namespace, "schedule category remains unscoped")
					}
				}}
				ctx := auth.AuthSessionTo(t.Context(), session{})
				config := &apiv1.ScheduledRunConfig{Schedule: "* * * * *", Prompt: "run"}
				var err error
				if operation == "create" {
					scheme := runtime.NewScheme()
					require.NoError(t, v1alpha3.AddToScheme(scheme))
					svc := scheduledrun.NewService(missingRequestStore{}, fake.NewClientBuilder().WithScheme(scheme).Build(), policy)
					_, err = svc.Create(ctx, scheduledrun.CreateRequest{Agent: target, RequestID: "create", Config: config})
					if allowed {
						require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeFailedPrecondition), "target authorization should pass before discovering the missing Agent")
					}
				} else {
					svc := scheduledrun.NewService(store, nil, policy)
					switch operation {
					case "retry":
						_, err = svc.Create(ctx, scheduledrun.CreateRequest{Agent: target, RequestID: "retry", Config: config})
					case "update":
						_, err = svc.Update(ctx, uuid.New(), "etag", config)
					case "trigger":
						_, err = svc.Trigger(ctx, uuid.New(), "manual")
					}
					if allowed {
						require.NoError(t, err)
					}
				}
				require.Equal(t, 1, checks)
				if !allowed {
					require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))
					require.Zero(t, store.triggers)
					require.Zero(t, store.updates)
				}
			})
		}
	}
}
