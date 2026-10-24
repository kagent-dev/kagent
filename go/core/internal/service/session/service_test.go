package session

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type serviceTestSession struct{ userID string }

func (s serviceTestSession) Principal() auth.Principal {
	return auth.Principal{ID: s.userID}
}

type serviceTestAuthorizer struct{ err error }

func (a serviceTestAuthorizer) Check(context.Context, auth.Principal, auth.Verb, auth.Resource) error {
	return a.err
}

type serviceTestStore struct {
	createInput    *apiv1.Session
	requestID      string
	createErr      error
	sessions       []*apiv1.Session
	listQuery      database.SessionQuery
	listCalls      int
	share          *apiv1.SessionShare
	tokenHash      []byte
	shares         []*apiv1.SessionShare
	shareAfterID   string
	shareSessionID string
	deletedShareID string
	shareLimit     int
	renamed        *apiv1.Session
	renameName     string
	renameUserID   string
	renameErr      error
	getCreator     string
	getID          string
	unscoped       bool
	getErr         error
	getResult      *apiv1.Session
	shareUserID    string
	shareErr       error
}

func (s *serviceTestStore) CreateSession(_ context.Context, session *apiv1.Session, requestID string) (*apiv1.Session, bool, error) {
	s.createInput = session
	s.requestID = requestID
	if s.createErr != nil {
		return nil, false, s.createErr
	}
	session.State = apiv1.RuntimeState_RUNTIME_STATE_READY
	session.Operation = apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE
	return session, true, nil
}

func (s *serviceTestStore) GetSession(_ context.Context, id, creator string) (*apiv1.Session, error) {
	s.getID, s.getCreator = id, creator
	if s.getResult != nil || s.getErr != nil {
		return s.getResult, s.getErr
	}
	return &apiv1.Session{Id: id, State: apiv1.RuntimeState_RUNTIME_STATE_READY, Operation: apiv1.RuntimeOperation_RUNTIME_OPERATION_NONE}, nil
}

func (s *serviceTestStore) GetSessionByID(ctx context.Context, id string) (*apiv1.Session, error) {
	s.unscoped = true
	return s.GetSession(ctx, id, "")
}

func (s *serviceTestStore) ListSessions(_ context.Context, query database.SessionQuery) ([]*apiv1.Session, error) {
	s.listQuery = query
	s.listCalls++
	var result []*apiv1.Session
	for _, session := range s.sessions {
		if session.Id > query.AfterID {
			result = append(result, session)
			if len(result) == query.Limit {
				break
			}
		}
	}
	return result, nil
}

func (s *serviceTestStore) UpdateSessionName(_ context.Context, id, userID, name string) (*apiv1.Session, error) {
	if s.renameErr != nil {
		return nil, s.renameErr
	}
	s.renameName, s.renameUserID = name, userID
	s.renamed = &apiv1.Session{Id: id, Name: name}
	return s.renamed, nil
}

func (s *serviceTestStore) CreateSessionShare(_ context.Context, share *apiv1.SessionShare, tokenHash []byte, userID string) (*apiv1.SessionShare, error) {
	s.share, s.tokenHash, s.shareUserID = share, tokenHash, userID
	return s.share, s.shareErr
}

func (s *serviceTestStore) ListSessionShares(_ context.Context, sessionID, _, afterID string, limit int) ([]*apiv1.SessionShare, error) {
	s.shareSessionID, s.shareAfterID, s.shareLimit = sessionID, afterID, limit
	return s.shares, nil
}

func (s *serviceTestStore) DeleteSessionShare(_ context.Context, id, _ string) error {
	s.deletedShareID = id
	return nil
}

type serviceTestWorkflow struct{ err error }

func (w serviceTestWorkflow) Create(_ context.Context, session *apiv1.Session) (*apiv1.Session, error) {
	return session, w.err
}

func (w serviceTestWorkflow) Suspend(_ context.Context, session *apiv1.Session) (*apiv1.Session, error) {
	return session, w.err
}

func (w serviceTestWorkflow) Resume(_ context.Context, session *apiv1.Session) (*apiv1.Session, error) {
	return session, w.err
}

func (w serviceTestWorkflow) Delete(_ context.Context, session *apiv1.Session) (*apiv1.Session, error) {
	return session, w.err
}

func serviceTestContext(userID string) context.Context {
	return auth.AuthSessionTo(context.Background(), serviceTestSession{userID: userID})
}

func TestServiceCreateUsesAuthenticatedOwnerAndGeneratedUUID(t *testing.T) {
	store := &serviceTestStore{}
	service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})

	session, err := service.Create(serviceTestContext("alice"), &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"}, "request-1", "")
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
			_, err := service.Create(serviceTestContext("alice"), &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"}, "request-1", "")
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
			_, err := service.Create(test.ctx, &apiv1.ResourceReference{Namespace: test.namespace, Name: "assistant"}, "request-1", "")
			if !serviceerrors.IsCode(err, test.code) {
				t.Fatalf("Create() error = %v, want code %s", err, test.code)
			}
		})
	}
}

func TestServiceLifecycleMethodsPreserveContentionErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		call func(*Service, context.Context, string) (*apiv1.Session, error)
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
	store := &serviceTestStore{sessions: []*apiv1.Session{{Id: ids[0]}, {Id: ids[1]}, {Id: ids[2]}}}
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

	share, token, err := service.CreateShare(serviceTestContext("alice"), sessionID, 0)
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
	if share.GetExpiresAt() != nil {
		t.Fatalf("a share without a ttl expires at %v, want never", share.GetExpiresAt().AsTime())
	}
}

func TestServiceCreateShareTTL(t *testing.T) {
	for _, test := range []struct {
		name    string
		maxTTL  time.Duration
		ttl     time.Duration
		want    time.Duration
		wantErr bool
	}{
		{name: "ttl without a maximum is kept", ttl: time.Hour, want: time.Hour},
		{name: "unset ttl takes the maximum", maxTTL: 24 * time.Hour, want: 24 * time.Hour},
		{name: "shorter ttl is kept", maxTTL: 24 * time.Hour, ttl: time.Hour, want: time.Hour},
		{name: "ttl equal to the maximum is kept", maxTTL: 24 * time.Hour, ttl: 24 * time.Hour, want: 24 * time.Hour},
		{name: "ttl above the maximum is refused", maxTTL: 24 * time.Hour, ttl: 25 * time.Hour, wantErr: true},
		{name: "negative ttl is refused", ttl: -time.Second, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &serviceTestStore{}
			service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{}, WithShareMaxTTL(test.maxTTL))
			before := time.Now()

			share, _, err := service.CreateShare(serviceTestContext("alice"), "11111111-1111-4111-8111-111111111111",
				test.ttl)
			if test.wantErr {
				if !serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument) {
					t.Fatalf("CreateShare error = %v, want InvalidArgument", err)
				}
				if store.share != nil {
					t.Fatal("a refused share reached the store")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			expires := share.GetExpiresAt().AsTime()
			if expires.Before(before.Add(test.want)) || expires.After(time.Now().Add(test.want)) {
				t.Fatalf("share expires at %v, want %v after its creation", expires, test.want)
			}
		})
	}
}

func TestServiceListSharesPaginatesInStore(t *testing.T) {
	ids := []string{
		"11111111-1111-4111-8111-111111111111",
		"22222222-2222-4222-8222-222222222222",
		"33333333-3333-4333-8333-333333333333",
		"44444444-4444-4444-8444-444444444444",
	}
	store := &serviceTestStore{shares: []*apiv1.SessionShare{
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
			session, err := service.Create(serviceTestContext("alice"), &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"}, "request-1", test.given)
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

type recordingAuthorizer struct {
	verb        auth.Verb
	resource    auth.Resource
	principal   auth.Principal
	denied      map[string]bool
	deniedTypes map[string]bool
	resources   []auth.Resource
	verbs       []auth.Verb
}

func (a *recordingAuthorizer) Check(_ context.Context, principal auth.Principal, verb auth.Verb, resource auth.Resource) error {
	a.verb, a.resource, a.principal = verb, resource, principal
	a.resources = append(a.resources, resource)
	a.verbs = append(a.verbs, verb)
	if a.denied[resource.Name] || a.deniedTypes[resource.Type] {
		return errors.New("denied")
	}
	return nil
}

func TestServiceListPassesAgentToStore(t *testing.T) {
	store := &serviceTestStore{}
	service := NewService(store, serviceTestAuthorizer{}, serviceTestWorkflow{})
	agent := &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"}
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
	share, token, err := service.CreateShare(serviceTestContext("alice"), uuid.NewString(), 0)
	if !serviceerrors.IsCode(err, serviceerrors.CodeNotFound) {
		t.Fatalf("CreateShare error = %v, want NotFound", err)
	}
	if share != nil || token != "" {
		t.Fatal("failed share creation returned credentials")
	}
}

func TestServiceGetScopesReadsToAuthorizedIdentity(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	for _, test := range []struct {
		name     string
		ctx      context.Context
		share    *auth.ShareContext
		denied   bool
		owner    string
		unscoped bool
		code     serviceerrors.Code
	}{
		{name: "owner", ctx: serviceTestContext("alice"), owner: "alice"},
		{name: "denied owner", ctx: serviceTestContext("alice"), denied: true, code: serviceerrors.CodePermissionDenied},
		{name: "unauthenticated", ctx: context.Background(), code: serviceerrors.CodeUnauthenticated},
		{name: "empty caller ID stays scoped", ctx: serviceTestContext("")},
		{name: "matching share", ctx: serviceTestContext("visitor"), share: &auth.ShareContext{SessionID: id, UserID: "owner"}, owner: "owner"},
		{name: "matching share denied by policy", ctx: serviceTestContext("visitor"), share: &auth.ShareContext{SessionID: id, UserID: "owner"}, denied: true, code: serviceerrors.CodePermissionDenied},
		{name: "share without authentication", ctx: context.Background(), share: &auth.ShareContext{SessionID: id, UserID: "owner"}, code: serviceerrors.CodeUnauthenticated},
		{name: "unrelated share denies permitted caller", ctx: serviceTestContext("alice"), share: &auth.ShareContext{SessionID: uuid.NewString(), UserID: "owner"}, code: serviceerrors.CodePermissionDenied},
		{name: "unrelated share grants no access", ctx: serviceTestContext("visitor"), share: &auth.ShareContext{SessionID: uuid.NewString(), UserID: "owner"}, denied: true, code: serviceerrors.CodePermissionDenied},
		{name: "controller", ctx: auth.AuthSessionTo(context.Background(), auth.ControlPlaneSession{}), unscoped: true},
		{name: "denied controller", ctx: auth.AuthSessionTo(context.Background(), auth.ControlPlaneSession{}), denied: true, code: serviceerrors.CodePermissionDenied},
		{name: "controller with share stays scoped", ctx: auth.AuthSessionTo(context.Background(), auth.ControlPlaneSession{}), share: &auth.ShareContext{SessionID: id, UserID: "owner"}, owner: "owner"},
	} {
		t.Run(test.name, func(t *testing.T) {
			ctx := test.ctx
			if test.share != nil {
				ctx = auth.ShareContextTo(ctx, test.share)
			}
			stored := &apiv1.Session{Id: id, State: apiv1.RuntimeState_RUNTIME_STATE_SUSPENDED}
			store := &serviceTestStore{getResult: stored}
			authorizer := &recordingAuthorizer{denied: map[string]bool{id: test.denied}}
			// A suspended read must never touch a workflow.
			service := NewService(store, authorizer, nil)
			result, err := service.Get(ctx, id)
			if test.code != "" {
				if !serviceerrors.IsCode(err, test.code) || store.getID != "" {
					t.Fatalf("Get() = %v, read ID = %q, want %s before storage", err, store.getID, test.code)
				}
				return
			}
			if err != nil || result != stored || store.getCreator != test.owner || store.unscoped != test.unscoped {
				t.Fatalf("Get() = %v, owner = %q, unscoped = %v", err, store.getCreator, store.unscoped)
			}
			caller, _ := auth.AuthSessionFrom(ctx)
			if authorizer.verb != auth.VerbGet || authorizer.resource.Type != "Session" || authorizer.resource.Name != id || authorizer.principal.ID != caller.Principal().ID {
				t.Fatalf("unexpected authorization: %+v", authorizer)
			}
		})
	}
}

func TestServiceGetMapsErrors(t *testing.T) {
	for _, test := range []struct {
		name string
		id   string
		err  error
		code serviceerrors.Code
	}{
		{name: "invalid ID", id: "bad-id", code: serviceerrors.CodeInvalidArgument},
		{name: "missing session", id: uuid.NewString(), err: database.ErrNotFound, code: serviceerrors.CodeNotFound},
		{name: "store failure", id: uuid.NewString(), err: errors.New("database unavailable"), code: serviceerrors.CodeInternal},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &serviceTestStore{getErr: test.err}
			service := NewService(store, serviceTestAuthorizer{}, nil)
			_, err := service.Get(serviceTestContext("alice"), test.id)
			if !serviceerrors.IsCode(err, test.code) {
				t.Fatalf("Get() = %v, want %s", err, test.code)
			}
			if test.code == serviceerrors.CodeInvalidArgument && store.getID != "" {
				t.Fatal("invalid identity reached storage")
			}
		})
	}
}

func TestServiceRejectsReadOnlyShareMutationsWithoutTransport(t *testing.T) {
	id := uuid.NewString()
	ctx := auth.ShareContextTo(serviceTestContext("visitor"), &auth.ShareContext{SessionID: id, UserID: "owner"})
	// Nil dependencies prove denial happens before storage or lifecycle work,
	// even though the caller's ordinary authorizer would permit the operation.
	service := NewService(nil, serviceTestAuthorizer{}, nil)
	for _, test := range []struct {
		name string
		call func() error
	}{
		{name: "suspend", call: func() error { _, err := service.Suspend(ctx, id); return err }},
		{name: "resume", call: func() error { _, err := service.Resume(ctx, id); return err }},
		{name: "delete", call: func() error { _, err := service.Delete(ctx, id); return err }},
		{name: "rename", call: func() error { _, err := service.Rename(ctx, id, "title"); return err }},
		{name: "share listing", call: func() error { _, err := service.ListShares(ctx, id, 0, ""); return err }},
		{name: "share revocation", call: func() error { return service.RevokeShare(ctx, id) }},
		{name: "share creation", call: func() error {
			_, _, err := service.CreateShare(ctx, id, 0)
			return err
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := test.call(); !serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied) {
				t.Fatalf("mutation = %v, want permission denied", err)
			}
		})
	}
}

func TestServiceShareCannotCreateSession(t *testing.T) {
	ctx := auth.ShareContextTo(serviceTestContext("alice"), &auth.ShareContext{SessionID: uuid.NewString(), UserID: "alice"})
	service := NewService(nil, serviceTestAuthorizer{}, nil)
	_, err := service.Create(ctx, &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"}, "request-1", "")
	if !serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied) {
		t.Fatalf("Create() = %v, want permission denied", err)
	}
}

func TestActorCannotUseShareAuthorityWithoutTransport(t *testing.T) {
	id := uuid.NewString()
	ctx := auth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: auth.Principal{
		ID: "owner", RuntimeBinding: &auth.RuntimeBinding{SessionID: uuid.NewString()},
	}})
	if _, err := ResolveShare(ctx, nil, "share-token"); !serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied) {
		t.Fatalf("ResolveShare() = %v, want permission denied before reading the share", err)
	}
	ctx = auth.ShareContextTo(ctx, &auth.ShareContext{SessionID: id, UserID: "owner"})
	service := NewService(nil, serviceTestAuthorizer{}, nil)
	if _, err := service.Get(ctx, id); !serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied) {
		t.Fatalf("Get() = %v, want permission denied before reading another session", err)
	}
	if _, err := service.List(ctx, ListRequest{}); !serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied) {
		t.Fatalf("List() = %v, want permission denied before listing shared sessions", err)
	}
}

func TestServiceSharedListCannotBroadenAccess(t *testing.T) {
	id := uuid.NewString()
	agent := &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"}
	ctx := auth.ShareContextTo(serviceTestContext("visitor"), &auth.ShareContext{SessionID: id, UserID: "owner"})
	for _, test := range []struct {
		name    string
		request ListRequest
		count   int
	}{
		{name: "only shared conversation", count: 1},
		{name: "all creators", request: ListRequest{AllCreators: true}, count: 1},
		{name: "matching Agent", request: ListRequest{Agent: agent}, count: 1},
		{name: "other Agent", request: ListRequest{Agent: &apiv1.ResourceReference{Namespace: "team-a", Name: "other"}}},
		{name: "past shared conversation", request: ListRequest{PageToken: encodePageToken(id)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			stored := &apiv1.Session{Id: id, Agent: agent}
			store := &serviceTestStore{getResult: stored}
			service := NewService(store, serviceTestAuthorizer{}, nil)
			result, err := service.List(ctx, test.request)
			if err != nil || len(result.Sessions) != test.count || result.NextPageToken != "" {
				t.Fatalf("List() = %+v, %v", result, err)
			}
			if store.listCalls != 0 || store.getCreator != "owner" || store.unscoped {
				t.Fatal("shared listing escaped owner-scoped single-session lookup")
			}
			if test.count == 1 && result.Sessions[0] != stored {
				t.Fatal("List() returned a different conversation")
			}
		})
	}
}

func TestServiceListAuthorizesBeforePagination(t *testing.T) {
	for _, allDenied := range []bool{false, true} {
		t.Run(fmt.Sprintf("allDenied=%v", allDenied), func(t *testing.T) {
			store := &serviceTestStore{}
			authorizer := &recordingAuthorizer{denied: map[string]bool{}}
			var want []string
			for i := 1; i <= 10; i++ {
				id := fmt.Sprintf("00000000-0000-4000-8000-%012d", i)
				store.sessions = append(store.sessions, &apiv1.Session{Id: id})
				authorizer.denied[id] = allDenied || i%3 != 0
				if !authorizer.denied[id] {
					want = append(want, id)
				}
			}
			service := NewService(store, authorizer, nil)
			var got []string
			request := ListRequest{PageSize: 2}
			for page := 0; ; page++ {
				if page > len(want) {
					t.Fatal("pagination did not terminate")
				}
				result, err := service.List(serviceTestContext("alice"), request)
				if err != nil {
					t.Fatal(err)
				}
				for _, session := range result.Sessions {
					got = append(got, session.Id)
				}
				if result.NextPageToken == "" {
					break
				}
				afterID, err := decodePageToken(result.NextPageToken)
				if err != nil || len(result.Sessions) != request.PageSize || afterID != result.Sessions[len(result.Sessions)-1].Id || authorizer.denied[afterID] {
					t.Fatalf("cursor %q exposed a denied or unreturned Session", afterID)
				}
				request.PageToken = result.NextPageToken
			}
			if !slices.Equal(got, want) || store.listCalls < 4 {
				t.Fatalf("List() = %v, want %v; storage batches = %d", got, want, store.listCalls)
			}
			if store.getID != "" {
				t.Fatal("listing reread each Session instead of authorizing its stored record")
			}
		})
	}
}

func TestServiceCreateRequiresTargetAgentBeforeReservation(t *testing.T) {
	for _, deniedType := range []string{"Session", "Agent", ""} {
		t.Run("denied="+deniedType, func(t *testing.T) {
			store := &serviceTestStore{}
			authorizer := &recordingAuthorizer{deniedTypes: map[string]bool{deniedType: true}}
			service := NewService(store, authorizer, serviceTestWorkflow{})
			agent := &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"}
			_, err := service.Create(serviceTestContext("alice"), agent, "request-1", "")
			if deniedType != "" {
				if !serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied) || store.createInput != nil {
					t.Fatalf("Create() = %v, reserved = %v", err, store.createInput)
				}
			} else if err != nil || store.createInput == nil {
				t.Fatalf("Create() = %v, reserved = %v", err, store.createInput)
			}
			if deniedType != "Session" {
				want := auth.Resource{Type: "Agent", Namespace: agent.Namespace, Name: agent.Name}
				if authorizer.resource != want || authorizer.verb != auth.VerbGet || authorizer.principal.ID != "alice" {
					t.Fatalf("unexpected target authorization: %+v", authorizer)
				}
			}
		})
	}
}

func TestServiceListRequiresIndependentCategoryPermissions(t *testing.T) {
	for _, test := range []struct {
		name        string
		allCreators bool
		deniedType  string
		wantDenied  bool
	}{
		{name: "owned list denied", deniedType: "Session", wantDenied: true},
		{name: "owned list needs no broad grant", deniedType: "SessionAllCreators"},
		{name: "broad list needs Session", allCreators: true, deniedType: "Session", wantDenied: true},
		{name: "broad list needs separate grant", allCreators: true, deniedType: "SessionAllCreators", wantDenied: true},
		{name: "broad list permitted", allCreators: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &serviceTestStore{}
			authorizer := &recordingAuthorizer{deniedTypes: map[string]bool{test.deniedType: true}}
			service := NewService(store, authorizer, nil)
			_, err := service.List(serviceTestContext("alice"), ListRequest{AllCreators: test.allCreators})
			if test.wantDenied {
				if !serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied) || store.listCalls != 0 {
					t.Fatalf("List() = %v, storage calls = %d", err, store.listCalls)
				}
			} else if err != nil || store.listCalls != 1 || store.listQuery.UserID != "alice" || store.listQuery.AllUsers != test.allCreators {
				t.Fatalf("List() = %v, query = %+v", err, store.listQuery)
			}
			for i, resource := range authorizer.resources {
				if resource.Name != "" || resource.Namespace != "" || authorizer.verbs[i] != auth.VerbList {
					t.Fatalf("collection check = %s %+v", authorizer.verbs[i], resource)
				}
			}
		})
	}
}

type sessionCreateNamespacePolicy struct{ namespace string }

func (s sessionCreateNamespacePolicy) Check(_ context.Context, principal auth.Principal, verb auth.Verb, resource auth.Resource) error {
	if verb == auth.VerbCreate && resource.Type == "Session" && (resource.Namespace != s.namespace || resource.Name != "" || principal.ID != "alice") {
		return errors.New("Session creation denied in target namespace")
	}
	return nil
}

func TestSessionCreateUsesTargetNamespace(t *testing.T) {
	for _, namespace := range []string{"team-a", "team-b"} {
		t.Run(namespace, func(t *testing.T) {
			store := &serviceTestStore{}
			svc := NewService(store, sessionCreateNamespacePolicy{namespace: "team-a"}, serviceTestWorkflow{})
			created, err := svc.Create(serviceTestContext("alice"), &apiv1.ResourceReference{Namespace: namespace, Name: "assistant"}, "request", "")
			if namespace == "team-a" {
				if err != nil || created.GetAgent().GetNamespace() != namespace || created.GetCreator() != "alice" {
					t.Fatalf("create = %+v, error = %v", created, err)
				}
			} else if !serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied) || store.createInput != nil {
				t.Fatalf("denied create = %+v, error = %v, persisted = %+v", created, err, store.createInput)
			}
		})
	}
}

func TestServiceCanonicalizesSessionAndShareIdentities(t *testing.T) {
	const id = "018f47a2-4efb-7c21-a848-abcdefabcdef"
	for _, operation := range []struct {
		name         string
		verb         auth.Verb
		resourceName string
		call         func(*Service, string) error
		storedID     func(*serviceTestStore) string
	}{
		{"rename", auth.VerbUpdate, id, func(s *Service, input string) error {
			_, err := s.Rename(serviceTestContext("alice"), input, "title")
			return err
		}, func(s *serviceTestStore) string { return s.renamed.GetId() }},
		{"create share", auth.VerbCreate, id + "/shares", func(s *Service, input string) error {
			_, _, err := s.CreateShare(serviceTestContext("alice"), input, 0)
			return err
		}, func(s *serviceTestStore) string { return s.share.GetSessionId() }},
		{"list shares", auth.VerbList, id + "/shares", func(s *Service, input string) error {
			_, err := s.ListShares(serviceTestContext("alice"), input, 0, "")
			return err
		}, func(s *serviceTestStore) string { return s.shareSessionID }},
		{"revoke share", auth.VerbDelete, "shares/" + id, func(s *Service, input string) error { return s.RevokeShare(serviceTestContext("alice"), input) }, func(s *serviceTestStore) string { return s.deletedShareID }},
	} {
		for _, scenario := range []struct {
			name   string
			input  string
			denied bool
		}{
			{"canonical", id, false},
			{"uppercase", strings.ToUpper(id), false},
			{"uppercase denied", strings.ToUpper(id), true},
			{"invalid", "not-a-uuid", false},
		} {
			t.Run(operation.name+"/"+scenario.name, func(t *testing.T) {
				store := &serviceTestStore{}
				policy := &recordingAuthorizer{denied: map[string]bool{operation.resourceName: scenario.denied}}
				err := operation.call(NewService(store, policy, nil), scenario.input)
				if scenario.input == "not-a-uuid" {
					require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInvalidArgument))
					require.Empty(t, policy.resources)
				} else {
					require.Equal(t, []auth.Resource{{Type: "Session", Name: operation.resourceName}}, policy.resources)
					require.Equal(t, []auth.Verb{operation.verb}, policy.verbs)
					if scenario.denied {
						require.True(t, serviceerrors.IsCode(err, serviceerrors.CodePermissionDenied))
					} else {
						require.NoError(t, err)
						require.Equal(t, id, operation.storedID(store))
					}
				}
				if err != nil {
					require.Empty(t, operation.storedID(store), "rejected input must not reach storage")
				}
			})
		}
	}
}
