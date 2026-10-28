package runtimeauth_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/authorization"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/jwttest"
	"github.com/kagent-dev/kagent/go/core/internal/runtimeauth"
	"github.com/kagent-dev/kagent/go/core/internal/substrate"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type actorPolicy struct {
	auth.NoopAuthorizer
	principal auth.Principal
	checks    int
	denied    bool
	scope     authorization.AuthorizationScope
	scopeErr  error
}

var _ auth.CollectionAuthorizer = (*actorPolicy)(nil)

func (p *actorPolicy) Check(_ context.Context, principal auth.Principal, _ auth.Verb, _ auth.Resource) error {
	p.principal = principal
	p.checks++
	if p.denied {
		return errors.New("execution revoked")
	}
	return nil
}

func (p *actorPolicy) Scope(_ context.Context, principal auth.Principal, _ auth.Verb, _ string) (authorization.AuthorizationScope, error) {
	p.principal = principal
	return p.scope, p.scopeErr
}

func TestConfiguredActorPolicyNarrowsNativePermissions(t *testing.T) {
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile)
	require.NoError(t, err)
	store := newBinding()
	provider := runtimeauth.NewProvider(&authimpl.InsecureAuthenticator{}, verifier, store, issuer.URL)
	identity, err := provider.Authenticate(t.Context(), http.Header{"Authorization": {"Bearer " + issuer.Token(t, issuer.Claims("team-a", substrate.ActorName(store.session.Id), store.actorUID))}}, nil)
	require.NoError(t, err)
	ctx := auth.AuthSessionTo(t.Context(), identity)
	configured := &actorPolicy{}
	policy := runtimeauth.NewAuthorizer(configured, store)
	resource := auth.Resource{Type: "TaskStore", Namespace: "team-a", Name: store.session.Id}
	require.NoError(t, policy.Check(ctx, identity.Principal(), auth.VerbCreate, resource))
	require.Equal(t, identity.Principal(), configured.principal)
	require.NotEqual(t, store.session.Creator, configured.principal.ID)
	require.Equal(t, store.session.Creator, configured.principal.RuntimeBinding.CreatorID)
	require.Equal(t, store.session.Id, configured.principal.RuntimeBinding.SessionID)

	configured.denied = true
	require.EqualError(t, policy.Check(ctx, identity.Principal(), auth.VerbCreate, resource), "execution revoked")
	configured.denied = false
	checks := configured.checks
	require.Error(t, policy.Check(ctx, identity.Principal(), auth.VerbDelete, resource))
	require.Equal(t, checks, configured.checks, "configured permission cannot expand the native boundary")
	store.actorUID = "replacement"
	require.Error(t, policy.Check(ctx, identity.Principal(), auth.VerbCreate, resource))
	require.Equal(t, checks, configured.checks, "a stale Actor must fail before policy evaluation")
}

func TestConfiguredActorScopeIntersectsOwnAgent(t *testing.T) {
	issuer := jwttest.New(t)
	verifier, err := substrate.NewActorJWTVerifier(t.Context(), issuer.URL, issuer.CAFile)
	require.NoError(t, err)
	store := newBinding()
	provider := runtimeauth.NewProvider(&authimpl.InsecureAuthenticator{}, verifier, store, issuer.URL)
	identity, err := provider.Authenticate(t.Context(), http.Header{"Authorization": {"Bearer " + issuer.Token(t, issuer.Claims("team-a", substrate.ActorName(store.session.Id), store.actorUID))}}, nil)
	require.NoError(t, err)
	ctx := auth.AuthSessionTo(t.Context(), identity)
	for _, test := range []struct {
		name    string
		scope   authorization.AuthorizationScope
		want    authorization.ScopeKind
		wantErr bool
		err     error
	}{
		{name: "all narrows to own agent", scope: authorization.AuthorizationScope{Kind: authorization.ScopeAll}, want: authorization.ScopeAnyOf},
		{name: "none", scope: authorization.AuthorizationScope{Kind: authorization.ScopeNone}, want: authorization.ScopeNone},
		{name: "foreign agent", scope: authorization.AuthorizationScope{Kind: authorization.ScopeAnyOf, AnyOf: []authorization.ScopeClause{{All: []authorization.ScopePredicate{{Attribute: authorization.AttributeName, Operator: authorization.ScopeIn, Values: []string{"other"}}}}}}, want: authorization.ScopeNone},
		{name: "malformed", scope: authorization.AuthorizationScope{Kind: "unknown"}, wantErr: true},
		{name: "unavailable policy", err: errors.New("policy unavailable"), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			configured := &actorPolicy{scope: test.scope, scopeErr: test.err}
			policy := runtimeauth.NewAuthorizer(configured, store)
			scope, err := policy.Scope(ctx, identity.Principal(), auth.VerbList, "Agent")
			if test.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, identity.Principal(), configured.principal)
			require.Equal(t, test.want, scope.Kind)
			if scope.Kind == authorization.ScopeAnyOf {
				require.Equal(t, []authorization.ScopeClause{{All: []authorization.ScopePredicate{
					{Attribute: authorization.AttributeNamespace, Operator: authorization.ScopeIn, Values: []string{"team-a"}},
					{Attribute: authorization.AttributeName, Operator: authorization.ScopeIn, Values: []string{"agent"}},
				}}}, scope.AnyOf)
			}
		})
	}
}
