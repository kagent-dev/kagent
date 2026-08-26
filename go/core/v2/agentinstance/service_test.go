package agentinstance

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"strings"
	"testing"

	a2a "github.com/a2aproject/a2a-go/v2/a2a"
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
	createInput  *apiv1alpha1.AgentInstance
	requestID    string
	createErr    error
	instances    []*apiv1alpha1.AgentInstance
	listQuery    dbpkg.AgentInstanceQuery
	share        dbpkg.AgentInstanceShare
	shares       []dbpkg.AgentInstanceShare
	shareAfterID string
	shareLimit   int
	renamed      *apiv1alpha1.AgentInstance
	renameName   string
	renameUserID string
	renameErr    error
	getCreator   string
	activeTask   *a2a.Task
	interrupted  string
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

func (s *serviceTestStore) GetAgentInstance(_ context.Context, _, _, creator string) (*apiv1alpha1.AgentInstance, error) {
	s.getCreator = creator
	return &apiv1alpha1.AgentInstance{State: apiv1alpha1.AgentInstanceState_AGENT_INSTANCE_STATE_READY}, nil
}

func (s *serviceTestStore) ListAgentInstances(_ context.Context, query dbpkg.AgentInstanceQuery) ([]*apiv1alpha1.AgentInstance, error) {
	s.listQuery = query
	return s.instances, nil
}

func (s *serviceTestStore) RenameAgentInstance(_ context.Context, _, id, userID, name string) (*apiv1alpha1.AgentInstance, error) {
	if s.renameErr != nil {
		return nil, s.renameErr
	}
	s.renameName, s.renameUserID = name, userID
	s.renamed = &apiv1alpha1.AgentInstance{Id: id, Name: name}
	return s.renamed, nil
}

func (s *serviceTestStore) GetActiveAgentInstanceTask(context.Context, string) (*a2a.Task, error) {
	if s.activeTask == nil {
		return nil, dbpkg.ErrNotFound
	}
	return s.activeTask, nil
}

func (s *serviceTestStore) InterruptActiveAgentInstanceTask(_ context.Context, _, taskID string) (bool, error) {
	s.interrupted = taskID
	return true, nil
}

func (s *serviceTestStore) CreateAgentInstanceShare(_ context.Context, share dbpkg.AgentInstanceShare) (*dbpkg.AgentInstanceShare, error) {
	s.share = share
	return &s.share, nil
}

func (s *serviceTestStore) ListAgentInstanceShares(_ context.Context, _, _, _, afterID string, limit int) ([]dbpkg.AgentInstanceShare, error) {
	s.shareAfterID, s.shareLimit = afterID, limit
	return s.shares, nil
}

func (*serviceTestStore) DeleteAgentInstanceShare(context.Context, string, string, string) error {
	return nil
}

type serviceTestWorkflow struct{ err error }

func (w serviceTestWorkflow) Create(_ context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	return instance, w.err
}

func (w serviceTestWorkflow) Suspend(_ context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	return instance, w.err
}

func (w serviceTestWorkflow) Resume(_ context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	return instance, w.err
}

func (w serviceTestWorkflow) Delete(_ context.Context, instance *apiv1alpha1.AgentInstance) (*apiv1alpha1.AgentInstance, error) {
	return instance, w.err
}

func serviceTestContext(userID string) context.Context {
	return auth.AuthSessionTo(context.Background(), serviceTestSession{userID: userID})
}

func TestServiceCreateUsesAuthenticatedOwnerAndGeneratedUUID(t *testing.T) {
	store := &serviceTestStore{}
	service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})

	instance, err := service.Create(serviceTestContext("alice"), "team-a", "kagent", "assistant", "request-1", "")
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
			_, err := service.Create(serviceTestContext("alice"), "team-a", "kagent", "assistant", "request-1", "")
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
			_, err := service.Create(test.ctx, test.namespace, "kagent", "assistant", "request-1", "")
			if !serviceerrors.IsCode(err, test.code) {
				t.Fatalf("Create() error = %v, want code %s", err, test.code)
			}
		})
	}
}

func TestServiceLifecycleMethodsMapConflictToAborted(t *testing.T) {
	service := NewService(&serviceTestStore{}, serviceTestAuthorizer{}, serviceTestWorkflow{err: dbpkg.ErrAgentInstanceConflict})
	for _, test := range []struct {
		name string
		call func(*Service, context.Context, string, string) (*apiv1alpha1.AgentInstance, error)
	}{
		{name: "suspend", call: (*Service).Suspend},
		{name: "resume", call: (*Service).Resume},
		{name: "delete", call: (*Service).Delete},
	} {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.call(service, serviceTestContext("alice"), "team-a", "8bd650a8-9775-488f-8bc1-0d52bf7bdcab")
			if !serviceerrors.IsCode(err, serviceerrors.CodeAborted) {
				t.Fatalf("error = %v, want code %s", err, serviceerrors.CodeAborted)
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
	if len(result.Instances) != 2 || store.listQuery.UserID != "alice" || store.listQuery.AllUsers || store.listQuery.Limit != 3 {
		t.Fatalf("List() = %+v, query = %+v", result, store.listQuery)
	}
	afterID, err := decodePageToken(result.NextPageToken)
	if err != nil || afterID != ids[1] {
		t.Fatalf("next page token = %q (%v), want %q", afterID, err, ids[1])
	}
	if _, err := service.List(serviceTestContext("alice"), ListRequest{Namespace: "team-a", AllCreators: true}); err != nil {
		t.Fatal(err)
	}
	if store.listQuery.UserID != "alice" || !store.listQuery.AllUsers {
		t.Fatalf("operator list query = %+v", store.listQuery)
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

func TestServiceListSharesPaginatesInStore(t *testing.T) {
	ids := []string{
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333",
		"44444444-4444-4444-8444-444444444444",
	}
	store := &serviceTestStore{shares: []dbpkg.AgentInstanceShare{{ID: ids[1]}, {ID: ids[2]}, {ID: ids[3]}}}
	service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})
	result, err := service.ListShares(serviceTestContext("alice"), "team-a", ids[0], 2, encodePageToken(ids[0]))
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Shares) != 2 || store.shareAfterID != ids[0] || store.shareLimit != 3 {
		t.Fatalf("ListShares() = %+v, after ID = %q, limit = %d", result, store.shareAfterID, store.shareLimit)
	}
	afterID, err := decodePageToken(result.NextPageToken)
	if err != nil || afterID != ids[2] {
		t.Fatalf("next page token = %q (%v), want %q", afterID, err, ids[2])
	}
}

func TestServiceCreateCarriesTheNameAndLeavesAnOmittedOneEmpty(t *testing.T) {
	for _, test := range []struct {
		name  string
		given string
		want  string
	}{
		{name: "named", given: "Debugging the ingress", want: "Debugging the ingress"},
		// An omitted name must stay empty rather than being filled in with the id:
		// the whole change is additive, and a caller that never mentions a name has
		// to behave exactly as it did before the field existed.
		{name: "omitted stays empty", given: "", want: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &serviceTestStore{}
			service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})
			instance, err := service.Create(serviceTestContext("alice"), "team-a", "kagent", "assistant", "request-1", test.given)
			if err != nil {
				t.Fatal(err)
			}
			if store.createInput.GetName() != test.want || instance.GetName() != test.want {
				t.Fatalf("stored name = %q, returned name = %q, want %q", store.createInput.GetName(), instance.GetName(), test.want)
			}
			if instance.GetName() == instance.GetId() && test.want == "" {
				t.Fatal("an unnamed instance was given its id as a name")
			}
		})
	}
}

func TestServiceRejectsInvalidNames(t *testing.T) {
	for _, test := range []struct {
		name    string
		given   string
		wantErr bool
	}{
		{name: "empty is unnamed", given: "", wantErr: false},
		{name: "ordinary title", given: "Why is the pod pending?", wantErr: false},
		{name: "punctuation and emoji", given: "deploy 🚀 v2 — take 3", wantErr: false},
		{name: "at the length limit", given: strings.Repeat("a", maxNameLength), wantErr: false},
		{name: "runes not bytes at the limit", given: strings.Repeat("é", maxNameLength), wantErr: false},
		{name: "over the length limit", given: strings.Repeat("a", maxNameLength+1), wantErr: true},
		{name: "newline", given: "first line\nsecond line", wantErr: true},
		{name: "carriage return", given: "title\r", wantErr: true},
		{name: "tab", given: "a\tb", wantErr: true},
		{name: "leading whitespace", given: " title", wantErr: true},
		{name: "trailing whitespace", given: "title ", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(&serviceTestStore{}, serviceTestAuthorizer{}, serviceTestWorkflow{})
			ctx := serviceTestContext("alice")
			createErr := service.createError(ctx, test.given)
			renameErr := service.renameError(ctx, test.given)
			if (createErr != nil) != test.wantErr || (renameErr != nil) != test.wantErr {
				t.Fatalf("create error = %v, rename error = %v, want error %t", createErr, renameErr, test.wantErr)
			}
			if test.wantErr && !serviceerrors.IsCode(createErr, serviceerrors.CodeInvalidArgument) {
				t.Fatalf("create error = %v, want code %s", createErr, serviceerrors.CodeInvalidArgument)
			}
		})
	}
}

// createError and renameError keep the validation table above honest: both entry
// points must apply the same rules, or a name refused on create is accepted on
// rename and reaches the database anyway.
func (s *Service) createError(ctx context.Context, name string) error {
	_, err := s.Create(ctx, "team-a", "kagent", "assistant", "request-1", name)
	return err
}

func (s *Service) renameError(ctx context.Context, name string) error {
	_, err := s.Rename(ctx, "team-a", "11111111-1111-4111-8111-111111111111", name)
	return err
}

func TestServiceRenameRequiresWriteAuthorizationAndScopesToTheOwner(t *testing.T) {
	instanceID := "11111111-1111-4111-8111-111111111111"

	t.Run("refused without authorization", func(t *testing.T) {
		store := &serviceTestStore{}
		service := NewService(store, serviceTestAuthorizer{err: errors.New("denied")}, serviceTestWorkflow{})
		_, err := service.Rename(serviceTestContext("alice"), "team-a", instanceID, "New title")
		if !serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied) {
			t.Fatalf("Rename() error = %v, want code %s", err, serviceerrors.CodePermissionDenied)
		}
		if store.renamed != nil {
			t.Fatal("Rename() reached the store despite being unauthorized")
		}
	})

	t.Run("authorizes as an update, not a read", func(t *testing.T) {
		authorizer := &recordingAuthorizer{}
		store := &serviceTestStore{}
		service := NewService(store, authorizer, serviceTestWorkflow{})
		instance, err := service.Rename(serviceTestContext("alice"), "team-a", instanceID, "New title")
		if err != nil {
			t.Fatal(err)
		}
		if authorizer.verb != auth.VerbUpdate {
			t.Fatalf("authorized verb = %q, want %q", authorizer.verb, auth.VerbUpdate)
		}
		if store.renameUserID != "alice" || store.renameName != "New title" || instance.GetName() != "New title" {
			t.Fatalf("rename owner = %q, name = %q, returned = %+v", store.renameUserID, store.renameName, instance)
		}
	})

	t.Run("clearing the name is allowed", func(t *testing.T) {
		store := &serviceTestStore{}
		service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})
		instance, err := service.Rename(serviceTestContext("alice"), "team-a", instanceID, "")
		if err != nil || instance.GetName() != "" {
			t.Fatalf("Rename(\"\") = %+v, error %v", instance, err)
		}
	})

	t.Run("a missing instance is not found", func(t *testing.T) {
		store := &serviceTestStore{renameErr: dbpkg.ErrNotFound}
		service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})
		_, err := service.Rename(serviceTestContext("alice"), "team-a", instanceID, "New title")
		if !serviceerrors.IsCode(err, serviceerrors.CodeNotFound) {
			t.Fatalf("Rename() error = %v, want code %s", err, serviceerrors.CodeNotFound)
		}
	})
}

/*
 * A share over an instance is authority to act on it, and the record is read as its owner.
 *
 * The same rule the A2A gateway applies, which is the point: the visitor could already
 * talk to a shared conversation through the gateway, because that understands shares,
 * and could not suspend or resume it through this service, because this did not. So a
 * shared conversation offered a live agent with no way to give its worker back.
 *
 * An instance is scoped to its creator, so reading it as the visitor finds nothing —
 * which is why this asserts the creator the store is asked for, not merely that the
 * call succeeded. A call that authorized correctly and then looked the record up under
 * the wrong user would fail as "not found", which is the confusing half of this bug.
 *
 * Read-only shares are refused before reaching here, by the interceptor, and are tested
 * where that rule lives.
 */
func TestServiceSuspendAcceptsAShareOverThatInstance(t *testing.T) {
	instanceID := "11111111-1111-4111-8111-111111111111"
	shared := auth.ShareContextTo(serviceTestContext("visitor"), &auth.ShareContext{
		AgentInstanceID: instanceID,
		UserID:          "owner",
	})

	t.Run("acts as the share's owner, not the visitor", func(t *testing.T) {
		store := &serviceTestStore{}
		// The authorizer refuses everything: a share that still needed its approval
		// would pass this test for the wrong reason.
		service := NewService(store, serviceTestAuthorizer{err: errors.New("denied")}, serviceTestWorkflow{})
		if _, err := service.Suspend(shared, "team-a", instanceID); err != nil {
			t.Fatalf("Suspend() with a share over this instance = %v, want it accepted", err)
		}
		if store.getCreator != "owner" {
			t.Fatalf("record read as %q, want the share's owner", store.getCreator)
		}
	})

	t.Run("a share over a different instance is no authority here", func(t *testing.T) {
		elsewhere := auth.ShareContextTo(serviceTestContext("visitor"), &auth.ShareContext{
			AgentInstanceID: "22222222-2222-4222-8222-222222222222",
			UserID:          "owner",
		})
		store := &serviceTestStore{}
		service := NewService(store, serviceTestAuthorizer{err: errors.New("denied")}, serviceTestWorkflow{})
		if _, err := service.Suspend(elsewhere, "team-a", instanceID); !serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied) {
			t.Fatalf("Suspend() with a share over another instance = %v, want it refused", err)
		}
	})

	t.Run("a session share is not an instance share", func(t *testing.T) {
		// Two different kinds of share over two different resources. Treating one as
		// the other is exactly what `IsForAgentInstance` exists to prevent.
		session := auth.ShareContextTo(serviceTestContext("visitor"), &auth.ShareContext{
			SessionID: instanceID,
			UserID:    "owner",
		})
		store := &serviceTestStore{}
		service := NewService(store, serviceTestAuthorizer{err: errors.New("denied")}, serviceTestWorkflow{})
		if _, err := service.Suspend(session, "team-a", instanceID); !serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied) {
			t.Fatalf("Suspend() with a session share = %v, want it refused", err)
		}
	})
}

type recordingAuthorizer struct {
	verb     auth.Verb
	resource auth.Resource
}

func (a *recordingAuthorizer) Check(_ context.Context, _ auth.Principal, verb auth.Verb, resource auth.Resource) error {
	a.verb, a.resource = verb, resource
	return nil
}

func TestServiceListPassesTheAgentPairThroughToTheStore(t *testing.T) {
	for _, test := range []struct {
		name     string
		request  ListRequest
		wantErr  bool
		wantPair [2]string
	}{
		{
			name:     "both halves of the pair",
			request:  ListRequest{Namespace: "team-a", AgentTemplate: "assistant", Harness: "kagent"},
			wantPair: [2]string{"assistant", "kagent"},
		},
		{
			name:     "template alone",
			request:  ListRequest{Namespace: "team-a", AgentTemplate: "assistant"},
			wantPair: [2]string{"assistant", ""},
		},
		{
			name:     "neither, which lists everything",
			request:  ListRequest{Namespace: "team-a"},
			wantPair: [2]string{"", ""},
		},
		{
			name:    "an invalid template name is refused rather than matching nothing",
			request: ListRequest{Namespace: "team-a", AgentTemplate: "NOT A NAME"},
			wantErr: true,
		},
		{
			name:    "an invalid harness name is refused",
			request: ListRequest{Namespace: "team-a", Harness: "NOT A NAME"},
			wantErr: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &serviceTestStore{}
			service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})
			_, err := service.List(serviceTestContext("alice"), test.request)
			if test.wantErr {
				if !serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument) {
					t.Fatalf("List() error = %v, want code %s", err, serviceerrors.CodeInvalidArgument)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := [2]string{store.listQuery.AgentTemplate, store.listQuery.Harness}
			if got != test.wantPair {
				t.Fatalf("store query pair = %v, want %v", got, test.wantPair)
			}
		})
	}
}

// TestServiceSuspendReapsTheActiveTurn pins the half of the stranded-task fix that
// stops the strand forming. Suspending stops the runtime, so an in-flight turn is
// over; leaving it non-terminal holds the instance's one active-task slot and
// every later send is refused with "AgentInstance already has an active task".
func TestServiceSuspendReapsTheActiveTurn(t *testing.T) {
	for _, test := range []struct {
		name            string
		active          *a2a.Task
		workflowErr     error
		wantInterrupted string
	}{
		{
			name:            "an in-flight turn is interrupted",
			active:          &a2a.Task{ID: "task-1", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
			wantInterrupted: "task-1",
		},
		{
			name:            "no active turn is left alone",
			active:          nil,
			wantInterrupted: "",
		},
		{
			// A suspend that did not happen must not close a turn that is still running.
			name:            "a failed suspend interrupts nothing",
			active:          &a2a.Task{ID: "task-1", Status: a2a.TaskStatus{State: a2a.TaskStateWorking}},
			workflowErr:     errors.New("substrate unavailable"),
			wantInterrupted: "",
		},
		{
			// Suspending is a pause, not an abandonment. A question the agent asked is
			// still valid and still answerable after a resume, so failing it here would
			// destroy the thing the conversation is waiting for — invisibly, since a
			// suspend says nothing about tasks.
			name:            "a turn waiting on the reader survives a suspend",
			active:          &a2a.Task{ID: "task-1", Status: a2a.TaskStatus{State: a2a.TaskStateInputRequired}},
			wantInterrupted: "",
		},
		{
			name:            "a turn waiting on authorization survives a suspend",
			active:          &a2a.Task{ID: "task-1", Status: a2a.TaskStatus{State: a2a.TaskStateAuthRequired}},
			wantInterrupted: "",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &serviceTestStore{activeTask: test.active}
			service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{err: test.workflowErr})
			_, err := service.Suspend(serviceTestContext("alice"), "team-a", "8bd650a8-9775-488f-8bc1-0d52bf7bdcab")
			if (err != nil) != (test.workflowErr != nil) {
				t.Fatalf("Suspend() error = %v", err)
			}
			if store.interrupted != test.wantInterrupted {
				t.Fatalf("interrupted task = %q, want %q", store.interrupted, test.wantInterrupted)
			}
		})
	}
}
