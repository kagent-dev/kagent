package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
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
	createInput  *apiv1alpha1.Session
	requestID    string
	createErr    error
	sessions     []*apiv1alpha1.Session
	listQuery    database.SessionQuery
	share        *apiv1alpha1.SessionShare
	tokenHash    []byte
	shares       []*apiv1alpha1.SessionShare
	shareAfterID string
	shareLimit   int
	renamed      *apiv1alpha1.Session
	renameName   string
	renameUserID string
	renameErr    error
	getCreator   string
	shareUserID  string
	shareErr     error
}

func (s *serviceTestStore) CreateSession(_ context.Context, session *apiv1alpha1.Session, requestID string) (*apiv1alpha1.Session, bool, error) {
	s.createInput = session
	s.requestID = requestID
	if s.createErr != nil {
		return nil, false, s.createErr
	}
	session.State = apiv1alpha1.SessionState_SESSION_STATE_READY
	return session, true, nil
}

func (s *serviceTestStore) GetSession(_ context.Context, _, creator string) (*apiv1alpha1.Session, error) {
	s.getCreator = creator
	return &apiv1alpha1.Session{State: apiv1alpha1.SessionState_SESSION_STATE_READY}, nil
}

func (s *serviceTestStore) ListSessions(_ context.Context, query database.SessionQuery) ([]*apiv1alpha1.Session, error) {
	s.listQuery = query
	return s.sessions, nil
}

func (s *serviceTestStore) UpdateSessionName(_ context.Context, id, userID, name string) (*apiv1alpha1.Session, error) {
	if s.renameErr != nil {
		return nil, s.renameErr
	}
	s.renameName, s.renameUserID = name, userID
	s.renamed = &apiv1alpha1.Session{Id: id, Name: name}
	return s.renamed, nil
}

func (s *serviceTestStore) CreateSessionShare(_ context.Context, share *apiv1alpha1.SessionShare, tokenHash []byte, userID string) (*apiv1alpha1.SessionShare, error) {
	s.share, s.tokenHash, s.shareUserID = share, tokenHash, userID
	return s.share, s.shareErr
}

func (s *serviceTestStore) ListSessionShares(_ context.Context, _, _, afterID string, limit int) ([]*apiv1alpha1.SessionShare, error) {
	s.shareAfterID, s.shareLimit = afterID, limit
	return s.shares, nil
}

func (*serviceTestStore) DeleteSessionShare(context.Context, string, string) error {
	return nil
}

type serviceTestWorkflow struct{ err error }

func (w serviceTestWorkflow) Create(_ context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	return session, w.err
}

func (w serviceTestWorkflow) Suspend(_ context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	return session, w.err
}

func (w serviceTestWorkflow) Resume(_ context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	return session, w.err
}

func (w serviceTestWorkflow) Delete(_ context.Context, session *apiv1alpha1.Session) (*apiv1alpha1.Session, error) {
	return session, w.err
}

func serviceTestContext(userID string) context.Context {
	return auth.AuthSessionTo(context.Background(), serviceTestSession{userID: userID})
}

func TestServiceCreateUsesAuthenticatedOwnerAndGeneratedUUID(t *testing.T) {
	store := &serviceTestStore{}
	service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})

	session, err := service.Create(serviceTestContext("alice"), &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"}, "request-1", "")
	if err != nil {
		t.Fatal(err)
	}
	id, err := uuid.Parse(session.GetId())
	if err != nil {
		t.Fatalf("generated id %q is not a UUID: %v", session.GetId(), err)
	}
	if id.Version() != 7 {
		t.Fatalf("generated id %q is UUIDv%d, want UUIDv7", id, id.Version())
	}
	if store.createInput.GetCreator() != "alice" || store.createInput.GetId() != session.GetId() || store.requestID != "request-1" {
		t.Fatalf("create input = %+v, request ID = %q", store.createInput, store.requestID)
	}
}

func TestServiceCreateMapsStoreErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		err  error
		code serviceerrors.Code
	}{
		{name: "idempotency conflict", err: database.ErrIdempotencyConflict, code: serviceerrors.CodeAlreadyExists},
		{name: "deleted request", err: database.ErrFailedPrecondition, code: serviceerrors.CodeFailedPrecondition},
		{name: "missing revision", err: database.ErrNotFound, code: serviceerrors.CodeFailedPrecondition},
		{name: "deleting revision", err: database.ErrObjectDeleting, code: serviceerrors.CodeFailedPrecondition},
		{name: "database failure", err: errors.New("database unavailable"), code: serviceerrors.CodeInternal},
	} {
		t.Run(test.name, func(t *testing.T) {
			service := NewService(&serviceTestStore{createErr: test.err}, serviceTestAuthorizer{}, serviceTestWorkflow{})
			_, err := service.Create(serviceTestContext("alice"), &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"}, "request-1", "")
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
			_, err := service.Create(test.ctx, &apiv1alpha1.ResourceReference{Namespace: test.namespace, Name: "assistant"}, "request-1", "")
			if !serviceerrors.IsCode(err, test.code) {
				t.Fatalf("Create() error = %v, want code %s", err, test.code)
			}
		})
	}
}

func TestServiceLifecycleMethodsPreserveContentionErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		call func(*Service, context.Context, string) (*apiv1alpha1.Session, error)
	}{
		{name: "suspend", call: (*Service).Suspend},
		{name: "resume", call: (*Service).Resume},
		{name: "delete", call: (*Service).Delete},
	} {
		t.Run(test.name, func(t *testing.T) {
			for _, failure := range []struct {
				cause error
				code  serviceerrors.Code
			}{
				{database.ErrConflict, serviceerrors.CodeAborted},
				{database.ErrFailedPrecondition, serviceerrors.CodeFailedPrecondition},
			} {
				conflict := fmt.Errorf("Session is already suspending: %w", failure.cause)
				service := NewService(&serviceTestStore{}, serviceTestAuthorizer{}, serviceTestWorkflow{err: conflict})
				_, err := test.call(service, serviceTestContext("alice"), "8bd650a8-9775-488f-8bc1-0d52bf7bdcab")
				if !serviceerrors.IsCode(err, failure.code) {
					t.Fatalf("error = %v, want code %s", err, failure.code)
				}
				if serviceerrors.MessageOf(err) != conflict.Error() || !errors.Is(err, failure.cause) {
					t.Fatalf("error = %v, want preserved contention reason", err)
				}
			}
		})
	}
}

func TestServiceListPaginatesBySessionID(t *testing.T) {
	ids := []string{
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333",
	}
	store := &serviceTestStore{sessions: []*apiv1alpha1.Session{{Id: ids[0]}, {Id: ids[1]}, {Id: ids[2]}}}
	service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})

	result, err := service.List(serviceTestContext("alice"), ListRequest{PageSize: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Sessions) != 2 || store.listQuery.UserID != "alice" || store.listQuery.AllUsers || store.listQuery.Limit != 3 {
		t.Fatalf("List() = %+v, query = %+v", result, store.listQuery)
	}
	afterID, err := decodePageToken(result.NextPageToken)
	if err != nil || afterID != ids[1] {
		t.Fatalf("next page token = %q (%v), want %q", afterID, err, ids[1])
	}
	if _, err := service.List(serviceTestContext("alice"), ListRequest{AllCreators: true}); err != nil {
		t.Fatal(err)
	}
	if store.listQuery.UserID != "alice" || !store.listQuery.AllUsers {
		t.Fatalf("operator list query = %+v", store.listQuery)
	}
}

func TestServiceCreateShareGeneratesTokenAndUUID(t *testing.T) {
	store := &serviceTestStore{}
	service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})
	sessionID := "11111111-1111-4111-8111-111111111111"

	share, token, err := service.CreateShare(serviceTestContext("alice"), sessionID, apiv1alpha1.SessionSharePermission_SESSION_SHARE_PERMISSION_READ_ONLY)
	if err != nil {
		t.Fatal(err)
	}
	if uuid.MustParse(share.GetId()) == uuid.Nil {
		t.Fatalf("generated share id %q is not a UUID: %v", uuid.MustParse(share.GetId()), err)
	}
	if uuid.MustParse(share.GetId()).Version() != 7 {
		t.Fatalf("generated share id %q is UUIDv%d, want UUIDv7", uuid.MustParse(share.GetId()), uuid.MustParse(share.GetId()).Version())
	}
	digest := sha256.Sum256([]byte(token))
	if !bytes.Equal(store.tokenHash, digest[:]) {
		t.Fatal("stored token hash does not match returned token")
	}
	if store.shareUserID != "alice" || store.getCreator != "" {
		t.Fatalf("share owner = %q, preparatory lookup owner = %q", store.shareUserID, store.getCreator)
	}
}

func TestServiceListSharesPaginatesInStore(t *testing.T) {
	ids := []string{
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333",
		"44444444-4444-4444-8444-444444444444",
	}
	store := &serviceTestStore{shares: []*apiv1alpha1.SessionShare{
		{Id: ids[1]}, {Id: ids[2]}, {Id: ids[3]},
	}}
	service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})
	result, err := service.ListShares(serviceTestContext("alice"), ids[0], 2, encodePageToken(ids[0]))
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
			session, err := service.Create(serviceTestContext("alice"), &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"}, "request-1", test.given)
			if err != nil {
				t.Fatal(err)
			}
			if store.createInput.GetName() != test.want || session.GetName() != test.want {
				t.Fatalf("stored name = %q, returned name = %q, want %q", store.createInput.GetName(), session.GetName(), test.want)
			}
			if session.GetName() == session.GetId() && test.want == "" {
				t.Fatal("an unnamed session was given its id as a name")
			}
		})
	}
}

func TestServiceRenameRequiresWriteAuthorizationAndScopesToTheOwner(t *testing.T) {
	sessionID := "11111111-1111-4111-8111-111111111111"

	t.Run("refused without authorization", func(t *testing.T) {
		store := &serviceTestStore{}
		service := NewService(store, serviceTestAuthorizer{err: errors.New("denied")}, serviceTestWorkflow{})
		_, err := service.Rename(serviceTestContext("alice"), sessionID, "New title")
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
		session, err := service.Rename(serviceTestContext("alice"), sessionID, "New title")
		if err != nil {
			t.Fatal(err)
		}
		if authorizer.verb != auth.VerbUpdate {
			t.Fatalf("authorized verb = %q, want %q", authorizer.verb, auth.VerbUpdate)
		}
		if store.renameUserID != "alice" || store.renameName != "New title" || session.GetName() != "New title" {
			t.Fatalf("rename owner = %q, name = %q, returned = %+v", store.renameUserID, store.renameName, session)
		}
	})

	t.Run("clearing the name is allowed", func(t *testing.T) {
		store := &serviceTestStore{}
		service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})
		session, err := service.Rename(serviceTestContext("alice"), sessionID, "")
		if err != nil || session.GetName() != "" {
			t.Fatalf("Rename(\"\") = %+v, error %v", session, err)
		}
	})

	t.Run("a missing session is not found", func(t *testing.T) {
		store := &serviceTestStore{renameErr: database.ErrNotFound}
		service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})
		_, err := service.Rename(serviceTestContext("alice"), sessionID, "New title")
		if !serviceerrors.IsCode(err, serviceerrors.CodeNotFound) {
			t.Fatalf("Rename() error = %v, want code %s", err, serviceerrors.CodeNotFound)
		}
	})
}

/*
 * A share over a session is authority to act on it, and the record is read as its owner.
 *
 * The same rule the A2A gateway applies, which is the point: the visitor could already
 * talk to a shared conversation through the gateway, because that understands shares,
 * and could not suspend or resume it through this service, because this did not. So a
 * shared conversation offered a live agent with no way to give its worker back.
 *
 * A session is scoped to its creator, so reading it as the visitor finds nothing —
 * which is why this asserts the creator the store is asked for, not merely that the
 * call succeeded. A call that authorized correctly and then looked the record up under
 * the wrong user would fail as "not found", which is the confusing half of this bug.
 *
 * Read-only shares are refused before reaching here, by the interceptor, and are tested
 * where that rule lives.
 */
func TestServiceSuspendAcceptsAShareOverThatSession(t *testing.T) {
	sessionID := "11111111-1111-4111-8111-111111111111"
	shared := auth.ShareContextTo(serviceTestContext("visitor"), &auth.ShareContext{
		SessionID: sessionID,
		UserID:    "owner",
	})

	t.Run("acts as the share's owner, not the visitor", func(t *testing.T) {
		store := &serviceTestStore{}
		// The authorizer refuses everything: a share that still needed its approval
		// would pass this test for the wrong reason.
		service := NewService(store, serviceTestAuthorizer{err: errors.New("denied")}, serviceTestWorkflow{})
		if _, err := service.Suspend(shared, sessionID); err != nil {
			t.Fatalf("Suspend() with a share over this session = %v, want it accepted", err)
		}
		if store.getCreator != "owner" {
			t.Fatalf("record read as %q, want the share's owner", store.getCreator)
		}
	})

	t.Run("a share over a different session is no authority here", func(t *testing.T) {
		elsewhere := auth.ShareContextTo(serviceTestContext("visitor"), &auth.ShareContext{
			SessionID: "22222222-2222-4222-8222-222222222222",
			UserID:    "owner",
		})
		store := &serviceTestStore{}
		service := NewService(store, serviceTestAuthorizer{err: errors.New("denied")}, serviceTestWorkflow{})
		if _, err := service.Suspend(elsewhere, sessionID); !serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied) {
			t.Fatalf("Suspend() with a share over another session = %v, want it refused", err)
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

func TestServiceListPassesAgentToStore(t *testing.T) {
	store := &serviceTestStore{}
	service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})
	agent := &apiv1alpha1.ResourceReference{Namespace: "team-a", Name: "assistant"}
	_, err := service.List(serviceTestContext("alice"), ListRequest{Agent: agent})
	if err != nil {
		t.Fatal(err)
	}
	if store.listQuery.Agent != agent {
		t.Fatal("Agent filter was lost")
	}
}

func TestServiceCreateShareMapsMissingOwnerToNotFound(t *testing.T) {
	store := &serviceTestStore{shareErr: database.ErrNotFound}
	service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})
	share, token, err := service.CreateShare(serviceTestContext("alice"), uuid.NewString(),
		apiv1alpha1.SessionSharePermission_SESSION_SHARE_PERMISSION_READ_ONLY)
	if !serviceerrors.IsCode(err, serviceerrors.CodeNotFound) {
		t.Fatalf("CreateShare error = %v, want NotFound", err)
	}
	if share != nil || token != "" {
		t.Fatal("failed share creation returned credentials")
	}
}
