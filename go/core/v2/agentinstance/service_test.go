package agentinstance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/google/uuid"
	dbpkg "github.com/kagent-dev/kagent/go/api/database"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type serviceTestSession struct{ userID string }

func (s serviceTestSession) Principal() auth.Principal {
	return auth.Principal{User: auth.User{ID: s.userID}}
}

type serviceTestAuthorizer struct{ err error }

func (a serviceTestAuthorizer) Check(context.Context, auth.Principal, auth.Verb, auth.Resource) error {
	return a.err
}

type serviceTestStore struct {
	createInput *apiv1alpha1.AgentInstance
	requestID   string
	createErr   error
	instances   []*apiv1alpha1.AgentInstance
	listCreator string
	listAfterID string
	listLimit   int
	share       dbpkg.AgentInstanceShare
}

func (s *serviceTestStore) CreateAgentInstance(_ context.Context, instance *apiv1alpha1.AgentInstance, requestID string) (*apiv1alpha1.AgentInstance, bool, error) {
	s.createInput = instance
	s.requestID = requestID
	if s.createErr != nil {
		return nil, false, s.createErr
	}
	instance.State = apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY
	return instance, true, nil
}

func (s *serviceTestStore) GetAgentInstance(context.Context, string, string, string) (*apiv1alpha1.AgentInstance, error) {
	return &apiv1alpha1.AgentInstance{State: apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY}, nil
}

func (s *serviceTestStore) ListAgentInstances(_ context.Context, _ string, creator string, _ map[string]string, afterID string, limit int) ([]*apiv1alpha1.AgentInstance, error) {
	s.listCreator, s.listAfterID, s.listLimit = creator, afterID, limit
	return s.instances, nil
}

func (*serviceTestStore) MarkAgentInstanceDeleting(context.Context, string, string, string) (*apiv1alpha1.AgentInstance, error) {
	return nil, nil
}

func (s *serviceTestStore) CreateAgentInstanceShare(_ context.Context, share dbpkg.AgentInstanceShare) (*dbpkg.AgentInstanceShare, error) {
	s.share = share
	return &s.share, nil
}

func (*serviceTestStore) ListAgentInstanceShares(context.Context, string, string, string) ([]dbpkg.AgentInstanceShare, error) {
	return nil, nil
}

func (*serviceTestStore) DeleteAgentInstanceShare(context.Context, string, string, string) error {
	return nil
}

type serviceTestWorkflow struct{}

func (serviceTestWorkflow) Create(_ context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	return instance, nil
}

func (serviceTestWorkflow) Delete(_ context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	return instance, nil
}

func serviceTestContext(userID string) context.Context {
	return auth.AuthSessionTo(context.Background(), serviceTestSession{userID: userID})
}

func TestServiceCreateUsesAuthenticatedOwnerAndGeneratedUUID(t *testing.T) {
	store := &serviceTestStore{}
	service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})

	instance, err := service.Create(serviceTestContext("alice"), "team-a", "kagent", "assistant", "request-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(instance.GetId()); err != nil {
		t.Fatalf("generated id %q is not a UUID: %v", instance.GetId(), err)
	}
	if store.createInput.GetCreator() != "alice" || store.createInput.GetId() != instance.GetId() || store.requestID != "request-1" {
		t.Fatalf("create input = %+v, request ID = %q", store.createInput, store.requestID)
	}
}

func TestServiceCreateMapsStoreErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		code serviceerrors.Code
	}{
		{name: "idempotency conflict", err: dbpkg.ErrIdempotencyConflict, code: serviceerrors.CodeAlreadyExists},
		{name: "missing revision", err: dbpkg.ErrNotFound, code: serviceerrors.CodeFailedPrecondition},
		{name: "database failure", err: errors.New("database unavailable"), code: serviceerrors.CodeInternal},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(&serviceTestStore{createErr: test.err}, serviceTestAuthorizer{}, serviceTestWorkflow{})
			_, err := service.Create(serviceTestContext("alice"), "team-a", "kagent", "assistant", "request-1")
			if !serviceerrors.IsCode(err, test.code) {
				t.Fatalf("Create() error = %v, want code %s", err, test.code)
			}
		})
	}
}

func TestServiceCreateRejectsInvalidOrUnauthorizedRequests(t *testing.T) {
	for _, test := range []struct {
		name       string
		ctx        context.Context
		namespace  string
		authorizer serviceTestAuthorizer
		code       serviceerrors.Code
	}{
		{name: "invalid namespace", ctx: serviceTestContext("alice"), namespace: "INVALID", code: serviceerrors.CodeInvalidArgument},
		{name: "missing authentication", ctx: context.Background(), namespace: "team-a", code: serviceerrors.CodeUnauthenticated},
		{name: "permission denied", ctx: serviceTestContext("alice"), namespace: "team-a", authorizer: serviceTestAuthorizer{err: errors.New("denied")}, code: serviceerrors.CodePermissionDenied},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(&serviceTestStore{}, test.authorizer, serviceTestWorkflow{})
			_, err := service.Create(test.ctx, test.namespace, "kagent", "assistant", "request-1")
			if !serviceerrors.IsCode(err, test.code) {
				t.Fatalf("Create() error = %v, want code %s", err, test.code)
			}
		})
	}
}

func TestServiceListPaginatesByInstanceID(t *testing.T) {
	ids := []string{
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333",
	}
	store := &serviceTestStore{instances: []*apiv1alpha1.AgentInstance{{Id: ids[0]}, {Id: ids[1]}, {Id: ids[2]}}}
	service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})

	result, err := service.List(serviceTestContext("alice"), ListRequest{Namespace: "team-a", PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Instances) != 2 || store.listCreator != "alice" || store.listLimit != 3 {
		t.Fatalf("List() = %+v, creator = %q, limit = %d", result, store.listCreator, store.listLimit)
	}
	afterID, err := decodePageToken(result.NextPageToken)
	if err != nil || afterID != ids[1] {
		t.Fatalf("next page token = %q (%v), want %q", afterID, err, ids[1])
	}
}

func TestServiceCreateShareGeneratesTokenAndUUID(t *testing.T) {
	store := &serviceTestStore{}
	service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})
	instanceID := "11111111-1111-4111-8111-111111111111"

	share, token, err := service.CreateShare(serviceTestContext("alice"), "team-a", instanceID, "READ_ONLY")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uuid.Parse(share.ID); err != nil {
		t.Fatalf("generated share id %q is not a UUID: %v", share.ID, err)
	}
	digest := sha256.Sum256([]byte(token))
	if !bytes.Equal(store.share.TokenHash, digest[:]) {
		t.Fatal("stored token hash does not match returned token")
	}
}
