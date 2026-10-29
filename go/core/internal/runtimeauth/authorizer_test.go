package runtimeauth_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/authorization"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type denyUsers struct{ auth.NoopAuthorizer }

func (denyUsers) Check(context.Context, auth.Principal, auth.Verb, auth.Resource) error {
	return fmt.Errorf("user denied")
}

func (denyUsers) Scope(context.Context, auth.Principal, auth.Verb, string) (authorization.AuthorizationScope, error) {
	return authorization.AuthorizationScope{Kind: authorization.ScopeNone}, nil
}

func TestActorPolicyDoesNotInheritOwnerPermissions(t *testing.T) {
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile, substrate.RuntimeJWTAudience)
	require.NoError(t, err)
	store := newBinding()
	provider := runtimeauth.NewProvider(&authimpl.InsecureAuthenticator{}, verifier, store, issuer.URL)
	identity, err := provider.Authenticate(t.Context(), http.Header{"Authorization": {"Bearer " + issuer.Token(t, issuer.Claims("team-a", substrate.ActorName(store.session.Id), store.actorUID))}}, nil)
	require.NoError(t, err)
	ctx := auth.AuthSessionTo(t.Context(), identity)
	for _, users := range []auth.CollectionAuthorizer{auth.NoopAuthorizer{}, denyUsers{}} {
		policy := runtimeauth.NewAuthorizer(users, store)
		for _, test := range []struct {
			name     string
			verb     auth.Verb
			resource auth.Resource
			allowed  bool
		}{
			{"own agent", auth.VerbGet, auth.Resource{Type: "Agent", Namespace: "team-a", Name: "agent"}, true},
			{"other agent", auth.VerbGet, auth.Resource{Type: "Agent", Namespace: "team-a", Name: "other"}, false},
			{"other namespace", auth.VerbGet, auth.Resource{Type: "Agent", Namespace: "other", Name: "agent"}, false},
			{"mutate agent", auth.VerbUpdate, auth.Resource{Type: "Agent", Namespace: "team-a", Name: "agent"}, false},
			{"own session", auth.VerbGet, auth.Resource{Type: "Session", Name: store.session.Id}, true},
			{"own session send", auth.VerbCreate, auth.Resource{Type: "Session", Name: store.session.Id}, true},
			{"own session continue", auth.VerbUpdate, auth.Resource{Type: "Session", Name: store.session.Id}, true},
			{"create checkpoint", auth.VerbCreate, auth.Resource{Type: "Checkpoint"}, false},
			{"session collection", auth.VerbGet, auth.Resource{Type: "Session"}, true},
			{"session list category", auth.VerbList, auth.Resource{Type: "Session"}, true},
			{"session share administration", auth.VerbList, auth.Resource{Type: "Session", Name: store.session.Id + "/shares"}, false},
			{"other session", auth.VerbGet, auth.Resource{Type: "Session", Name: uuid.NewString()}, false},
			{"new session", auth.VerbCreate, auth.Resource{Type: "Session"}, false},
			{"all creators", auth.VerbGet, auth.Resource{Type: "SessionAllCreators"}, false},
			{"all creators list", auth.VerbList, auth.Resource{Type: "SessionAllCreators"}, false},
			{"delete session", auth.VerbDelete, auth.Resource{Type: "Session", Name: store.session.Id}, false},
			{"share", auth.VerbCreate, auth.Resource{Type: "SessionShare", Name: store.session.Id}, false},
			{"own memory", auth.VerbList, auth.Resource{Type: "Memory", Name: "team-a/agent"}, true},
			{"other memory", auth.VerbDelete, auth.Resource{Type: "Memory", Name: "team-a/other"}, false},
			{"sandbox", auth.VerbGet, auth.Resource{Type: "Sandbox", Name: store.session.Id}, false},
			{"namespaces", auth.VerbGet, auth.Resource{Type: "SystemNamespaces"}, false},
			{"unknown", auth.VerbGet, auth.Resource{Type: "Extension"}, false},
		} {
			t.Run(test.name, func(t *testing.T) {
				err := policy.Check(ctx, identity.Principal(), test.verb, test.resource)
				if test.allowed {
					require.NoError(t, err)
				} else {
					require.Error(t, err)
				}
			})
		}
		scope, err := policy.Scope(ctx, identity.Principal(), auth.VerbList, "Agent")
		require.NoError(t, err)
		require.Equal(t, authorization.AuthorizationScope{Kind: authorization.ScopeAnyOf, AnyOf: []authorization.ScopeClause{{All: []authorization.ScopePredicate{
			{Attribute: authorization.AttributeNamespace, Operator: authorization.ScopeIn, Values: []string{"team-a"}},
			{Attribute: authorization.AttributeName, Operator: authorization.ScopeIn, Values: []string{"agent"}},
		}}}}, scope)
		scope, err = policy.Scope(ctx, identity.Principal(), auth.VerbList, "SandboxTemplate")
		require.NoError(t, err)
		require.Equal(t, authorization.ScopeNone, scope.Kind)
	}

	policy := runtimeauth.NewAuthorizer(denyUsers{}, store)
	user, err := provider.Authenticate(t.Context(), http.Header{"X-User-Id": {"owner"}}, nil)
	require.NoError(t, err)
	require.EqualError(t, policy.Check(auth.AuthSessionTo(t.Context(), user), user.Principal(), auth.VerbGet, auth.Resource{Type: "Session", Name: store.session.Id}), "user denied")
	store.actorUID = "replacement"
	require.Error(t, policy.Check(ctx, identity.Principal(), auth.VerbGet, auth.Resource{Type: "Session", Name: store.session.Id}))
	_, err = policy.Scope(ctx, identity.Principal(), auth.VerbList, "Agent")
	require.Error(t, err)
}
