package taskstore

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	runtimev1 "github.com/kagent-dev/kagent/go/api/gen/kagent/runtime/v1"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type policyStore struct{ Store }

func (policyStore) GetSessionForRuntime(_ context.Context, id, _ string) (*apiv1.Session, error) {
	return &apiv1.Session{
		Id: id, Agent: &apiv1.ResourceReference{Namespace: "team-a", Name: "agent"},
		A2AAuthority: substrate.ActorHost("team-a", substrate.ActorName(id), ""),
	}, nil
}

type denyingPolicy struct {
	principal auth.Principal
	verb      auth.Verb
	resource  auth.Resource
}

func (p *denyingPolicy) Check(_ context.Context, principal auth.Principal, verb auth.Verb, resource auth.Resource) error {
	p.principal, p.verb, p.resource = principal, verb, resource
	return errors.New("revoked")
}

func TestTaskStoreChecksPolicyBeforePersistence(t *testing.T) {
	const sessionID = "00000000-0000-0000-0000-000000000001"
	for _, test := range []struct {
		name string
		verb auth.Verb
		call func(context.Context, *Service) error
	}{
		{"create", auth.VerbCreate, func(ctx context.Context, s *Service) error {
			_, err := s.CreateTask(ctx, &runtimev1.TaskStoreServiceCreateTaskRequest{SessionId: sessionID})
			return err
		}},
		{"get", auth.VerbGet, func(ctx context.Context, s *Service) error {
			_, err := s.GetTask(ctx, &runtimev1.TaskStoreServiceGetTaskRequest{SessionId: sessionID})
			return err
		}},
		{"update", auth.VerbUpdate, func(ctx context.Context, s *Service) error {
			_, err := s.UpdateTask(ctx, &runtimev1.TaskStoreServiceUpdateTaskRequest{SessionId: sessionID})
			return err
		}},
		{"list", auth.VerbList, func(ctx context.Context, s *Service) error {
			_, err := s.ListTasks(ctx, &runtimev1.TaskStoreServiceListTasksRequest{SessionId: sessionID})
			return err
		}},
		{"settle", auth.VerbUpdate, func(ctx context.Context, s *Service) error {
			_, err := s.SettleTask(ctx, &runtimev1.TaskStoreServiceSettleTaskRequest{SessionId: sessionID})
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			configured := &denyingPolicy{}
			service := NewService(policyStore{}, configured, nil, nil)
			ctx := runtimeContext(t)
			require.Equal(t, codes.PermissionDenied, status.Code(test.call(ctx, service)))
			identity, ok := auth.AuthSessionFrom(ctx)
			require.True(t, ok)
			require.Equal(t, identity.Principal(), configured.principal)
			require.Equal(t, test.verb, configured.verb)
			require.Equal(t, auth.Resource{Type: "TaskStore", Namespace: "team-a", Name: sessionID}, configured.resource)
		})
	}
}
