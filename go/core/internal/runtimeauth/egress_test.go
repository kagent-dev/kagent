package runtimeauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	"github.com/kagent-dev/kagent/go/core/internal/database"
	"github.com/kagent-dev/kagent/go/core/internal/egress"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/kagent-dev/kagent/go/core/pkg/egresspolicy"
	"github.com/kagent-dev/kagent/go/pkg/invocationauth"
)

type policyStore struct {
	session *apiv1.Session
	access  egress.Access
}

var _ egressPolicyStore = (*policyStore)(nil)

func (s *policyStore) GetSessionForRuntime(_ context.Context, id, uid string) (*apiv1.Session, error) {
	if s.session == nil || s.session.Id != id || uid != "actor" {
		return nil, database.ErrNotFound
	}
	return s.session, nil
}
func (s *policyStore) GetRuntimeEgressPolicy(context.Context, string) (egress.Access, error) {
	return s.access, nil
}

func TestEgressPolicyIsLiveAndRequiresCurrentActor(t *testing.T) {
	id := uuid.NewString()
	store := &policyStore{
		session: &apiv1.Session{Id: id, A2AAuthority: substrate.ActorHost("team", substrate.ActorName(id), "")},
		access: egress.Access{
			Credentials:             []egress.Credential{{Origin: "https://model.example:443", Header: "authorization", Prefix: "Bearer ", URI: "secret://team/model/key"}},
			TokenPassthroughTargets: []egresspolicy.GatewayTarget{{Origin: "https://mcp.example:443", Header: "x-caller"}},
		},
	}
	handler := NewEgressPolicyHandler(store)
	actor := runtimeSession{sessionID: id, actorUID: "actor", atespace: "team"}
	request := httptest.NewRequest(http.MethodGet, invocationauth.EgressPolicyPath, nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusForbidden, response.Code)

	request = request.WithContext(auth.AuthSessionTo(t.Context(), actor))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	require.JSONEq(t, `[{"origin":"https://mcp.example:443","passthrough":[{"name":"x-caller"}]},{"origin":"https://model.example:443","headers":{"authorization":"Bearer kagent-credential-injected"}}]`, response.Body.String())
	require.NotContains(t, response.Body.String(), "secret://")
	require.Equal(t, "no-store", response.Header().Get("Cache-Control"))
	store.access = egress.Access{}
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.JSONEq(t, "[]", response.Body.String())
	store.session = nil
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	require.Equal(t, http.StatusForbidden, response.Code)
}
