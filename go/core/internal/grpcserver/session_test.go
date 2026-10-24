package grpcserver

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// Unused embedded methods panic, proving denied calls cannot reach mutations.
type sessionAuthorizationStore struct {
	*database.Client
	sessions []*apiv1.Session
	reads    atomic.Int32
	lists    atomic.Int32
}

func (s *sessionAuthorizationStore) GetSession(_ context.Context, id, owner string) (*apiv1.Session, error) {
	s.reads.Add(1)
	for _, session := range s.sessions {
		if session.Id == id && session.Creator == owner {
			return session, nil
		}
	}
	return nil, database.ErrNotFound
}

func (s *sessionAuthorizationStore) UpdateSessionName(ctx context.Context, id, owner, name string) (*apiv1.Session, error) {
	session, err := s.GetSession(ctx, id, owner)
	if err != nil {
		return nil, err
	}
	return &apiv1.Session{Id: session.Id, Creator: session.Creator, Agent: session.Agent, Name: name}, nil
}

func (s *sessionAuthorizationStore) ListSessions(_ context.Context, query database.SessionQuery) ([]*apiv1.Session, error) {
	s.lists.Add(1)
	var rows []*apiv1.Session
	for _, session := range s.sessions {
		if session.Id <= query.AfterID || (!query.AllUsers && session.Creator != query.UserID) {
			continue
		}
		if query.Agent != nil && (session.Agent.Namespace != query.Agent.Namespace || session.Agent.Name != query.Agent.Name) {
			continue
		}
		rows = append(rows, session)
		if len(rows) == query.Limit {
			break
		}
	}
	return rows, nil
}

type sessionAuthorizationPolicy struct{}

func (sessionAuthorizationPolicy) Check(_ context.Context, principal auth.Principal, verb auth.Verb, resource auth.Resource) error {
	if principal.ID == "blocked" || resource.Type == "Agent" {
		return errors.New("denied")
	}
	if resource.Type == "SessionAllCreators" {
		if principal.ID == "operator" && verb == auth.VerbList {
			return nil
		}
		return errors.New("denied")
	}
	if resource.Type == "Session" {
		if principal.ID == "no-share-list" && verb == auth.VerbList && strings.HasSuffix(resource.Name, "/shares") {
			return errors.New("share listing denied")
		}
		if verb == auth.VerbGet || verb == auth.VerbList || principal.ID != "viewer" {
			return nil
		}
	}
	return errors.New("denied")
}

func TestSessionPermissionsAndOwnershipThroughGeneratedClient(t *testing.T) {
	aliceID := "11111111-1111-4111-8111-111111111111"
	bobID := "22222222-2222-4222-8222-222222222222"
	agent := &apiv1.ResourceReference{Namespace: "team-a", Name: "assistant"}
	store := &sessionAuthorizationStore{sessions: []*apiv1.Session{
		{Id: aliceID, Creator: "alice", Agent: agent},
		{Id: bobID, Creator: "bob", Agent: agent},
	}}
	connection := newConfigurationConnection(t, nil, func(config *Config) {
		config.SessionService = sessionsvc.NewService(store, sessionAuthorizationPolicy{}, nil)
		config.ShareStore = &testShareStore{sessionShare: &apiv1.SessionShare{SessionId: aliceID}, ownerUserID: "alice"}
	})
	client := apiv1.NewSessionServiceClient(connection)
	caller := func(user string, shared bool) context.Context {
		pairs := []string{"x-user-id", user}
		if shared {
			pairs = append(pairs, "x-share-token", "share")
		}
		return metadata.NewOutgoingContext(t.Context(), metadata.Pairs(pairs...))
	}
	for _, owner := range []string{"alice", "bob"} {
		page, err := client.ListSessions(caller(owner, false), &apiv1.ListSessionsRequest{Agent: agent})
		require.NoError(t, err)
		require.Len(t, page.GetSessions(), 1)
		require.Equal(t, owner, page.GetSessions()[0].Creator)
	}
	before := store.lists.Load()
	_, err := client.ListSessions(caller("alice", false), &apiv1.ListSessionsRequest{AllCreators: true})
	assertGRPCCode(t, err, codes.PermissionDenied)
	_, err = client.ListSessions(caller("blocked", false), &apiv1.ListSessionsRequest{})
	assertGRPCCode(t, err, codes.PermissionDenied)
	require.Equal(t, before, store.lists.Load())

	wide, err := client.ListSessions(caller("operator", false), &apiv1.ListSessionsRequest{AllCreators: true, Page: &apiv1.PageRequest{Limit: 1}})
	require.NoError(t, err)
	require.Len(t, wide.GetSessions(), 1)
	require.NotEmpty(t, wide.GetPage().GetNextPageToken())
	owned, err := client.ListSessions(caller("bob", false), &apiv1.ListSessionsRequest{Page: &apiv1.PageRequest{PageToken: wide.GetPage().GetNextPageToken()}})
	require.NoError(t, err)
	require.Len(t, owned.GetSessions(), 1)
	require.Equal(t, "bob", owned.GetSessions()[0].Creator)
	_, err = client.GetSession(caller("operator", false), &apiv1.GetSessionRequest{SessionId: aliceID})
	assertGRPCCode(t, err, codes.NotFound)
	_, err = client.GetSession(caller("bob", false), &apiv1.GetSessionRequest{SessionId: aliceID})
	assertGRPCCode(t, err, codes.NotFound)

	_, err = client.UpdateSessionName(caller("operator", false), &apiv1.UpdateSessionNameRequest{SessionId: aliceID, Name: "forged"})
	assertGRPCCode(t, err, codes.NotFound)
	_, err = client.DeleteSession(caller("operator", false), &apiv1.DeleteSessionRequest{SessionId: aliceID})
	assertGRPCCode(t, err, codes.NotFound)
	renamed, err := client.UpdateSessionName(caller("bob", false), &apiv1.UpdateSessionNameRequest{SessionId: bobID, Name: "owned"})
	require.NoError(t, err)
	require.Equal(t, "owned", renamed.GetSession().GetName())
	before = store.reads.Load()
	_, err = client.UpdateSessionName(caller("viewer", false), &apiv1.UpdateSessionNameRequest{SessionId: bobID, Name: "forged"})
	assertGRPCCode(t, err, codes.PermissionDenied)
	require.Equal(t, before, store.reads.Load())

	shared, err := client.GetSession(caller("bob", true), &apiv1.GetSessionRequest{SessionId: aliceID})
	require.NoError(t, err)
	require.Equal(t, "alice", shared.GetSession().Creator)
	before = store.reads.Load()
	_, err = client.GetSession(caller("blocked", true), &apiv1.GetSessionRequest{SessionId: aliceID})
	assertGRPCCode(t, err, codes.PermissionDenied)
	_, err = client.GetSession(caller("bob", true), &apiv1.GetSessionRequest{SessionId: bobID})
	assertGRPCCode(t, err, codes.PermissionDenied)
	_, err = client.ListSessionShares(caller("bob", true), &apiv1.ListSessionSharesRequest{SessionId: aliceID})
	assertGRPCCode(t, err, codes.PermissionDenied)
	_, err = client.UpdateSessionName(caller("bob", true), &apiv1.UpdateSessionNameRequest{SessionId: aliceID, Name: "forged"})
	assertGRPCCode(t, err, codes.PermissionDenied)
	require.Equal(t, before, store.reads.Load())

	// Ordinary callers also need the list grant; share restrictions alone are insufficient.
	_, err = client.ListSessionShares(caller("no-share-list", false), &apiv1.ListSessionSharesRequest{SessionId: aliceID})
	assertGRPCCode(t, err, codes.PermissionDenied)

	// A permitted Session create cannot reserve anything without target Agent access.
	_, err = client.CreateSession(caller("alice", false), &apiv1.CreateSessionRequest{Agent: agent, RequestId: "request-1"})
	assertGRPCCode(t, err, codes.PermissionDenied)
}

type sessionNamespaceRPCPolicy struct {
	creates atomic.Int32
	agents  atomic.Int32
}

func (s *sessionNamespaceRPCPolicy) Check(_ context.Context, principal auth.Principal, verb auth.Verb, resource auth.Resource) error {
	if principal.ID != "alice" {
		return errors.New("unexpected principal")
	}
	if resource.Type == "Session" && verb == auth.VerbCreate {
		s.creates.Add(1)
		if resource.Namespace == "team-a" && resource.Name == "" {
			return nil
		}
	}
	if resource.Type == "Agent" && verb == auth.VerbGet {
		s.agents.Add(1)
	}
	return errors.New("denied")
}

func TestSessionCreateNamespaceThroughGeneratedClient(t *testing.T) {
	for _, namespace := range []string{"team-a", "team-b"} {
		t.Run(namespace, func(t *testing.T) {
			policy := &sessionNamespaceRPCPolicy{}
			connection := newConfigurationConnection(t, nil, func(config *Config) {
				config.SessionService = sessionsvc.NewService(nil, policy, nil)
			})
			ctx := metadata.NewOutgoingContext(t.Context(), metadata.Pairs("x-user-id", "alice"))
			_, err := apiv1.NewSessionServiceClient(connection).CreateSession(ctx, &apiv1.CreateSessionRequest{Agent: &apiv1.ResourceReference{Namespace: namespace, Name: "assistant"}, RequestId: "namespace-check"})
			// A namespace grant must still be followed by the independent Agent check;
			// neither denied path may reach storage or runtime provisioning.
			assertGRPCCode(t, err, codes.PermissionDenied)
			require.EqualValues(t, 1, policy.creates.Load())
			if namespace == "team-a" {
				require.EqualValues(t, 1, policy.agents.Load())
			} else {
				require.Zero(t, policy.agents.Load())
			}
		})
	}
}
