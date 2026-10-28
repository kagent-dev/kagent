package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	sessionsvc "github.com/kagent-dev/kagent/go/core/internal/service/session"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type actorSessionStore struct {
	*fakeSessionStore
	sessions []*apiv1.Session
}

func (s *actorSessionStore) GetSessionForRuntime(_ context.Context, id, uid string) (*apiv1.Session, error) {
	if id == s.sessions[0].Id && uid == "actor-uid" {
		return s.sessions[0], nil
	}
	return nil, database.ErrNotFound
}

func (s *actorSessionStore) ListSessions(_ context.Context, query database.SessionQuery) ([]*apiv1.Session, error) {
	var result []*apiv1.Session
	for _, session := range s.sessions {
		if session.Creator == query.UserID && session.Id > query.AfterID {
			result = append(result, session)
		}
	}
	return result, nil
}

func TestMCPActorListsOnlyItsSession(t *testing.T) {
	store := &actorSessionStore{fakeSessionStore: &fakeSessionStore{}}
	for range 3 {
		store.sessions = append(store.sessions, &apiv1.Session{Id: uuid.NewString(), Creator: "owner", Agent: &apiv1.ResourceReference{Namespace: "team-a", Name: "agent"}, State: apiv1.RuntimeState_RUNTIME_STATE_READY})
	}
	own := store.sessions[0]
	own.A2AAuthority = substrate.ActorHost("team-a", substrate.ActorName(own.Id), "")
	store.sessions[2].Creator = "other-user"
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile, substrate.RuntimeJWTAudience)
	require.NoError(t, err)
	provider := runtimeauth.NewProvider(&authimpl.InsecureAuthenticator{}, verifier, store, issuer.URL)
	sessions := sessionsvc.NewService(store, runtimeauth.NewAuthorizer(auth.NoopAuthorizer{}, store), &fakeSessionWorkflow{})
	handler, err := New(sessions, testCheckpointService(), &fakeGateway{}, nil, nil)
	require.NoError(t, err)
	server := httptest.NewServer(auth.AuthnMiddleware(provider)(handler))
	t.Cleanup(server.Close)
	token := "Bearer " + issuer.Token(t, issuer.Claims("team-a", substrate.ActorName(own.Id), "actor-uid"))
	for _, test := range []struct {
		name    string
		headers http.Header
		count   int
	}{
		{"actor", http.Header{"Authorization": {token}, "X-User-Id": {"other-user"}}, 1},
		{"user", http.Header{"X-User-Id": {"owner"}}, 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := rawMCPCall(t, server.URL, "tools/call", map[string]any{"name": listToolName, "arguments": map[string]any{}}, false, test.headers)
			result := response["result"].(map[string]any)
			require.NotEqual(t, true, result["isError"])
			listed := result["structuredContent"].(map[string]any)["sessions"].([]any)
			require.Len(t, listed, test.count)
			if test.name == "actor" {
				require.Equal(t, own.Id, listed[0].(map[string]any)["id"])
			}
		})
	}
	// Knowing another session's ID, even for the same agent and owner, cannot
	// grant invocation authority through a different MCP tool.
	response := rawMCPCall(t, server.URL, "tools/call", map[string]any{"name": invokeToolName, "arguments": map[string]any{"session_id": store.sessions[1].Id, "message": "hello"}}, false, http.Header{"Authorization": {token}})
	require.Equal(t, true, response["result"].(map[string]any)["isError"])
}
