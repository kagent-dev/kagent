package inventoryauth_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/authorization"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/inventoryauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestInventoryIdentityRequirements(t *testing.T) {
	for _, location := range []string{authorization.AttributeAtespace, authorization.AttributeNamespace} {
		t.Run(location, func(t *testing.T) {
			all, err := inventoryauth.CompileScope(authorization.AuthorizationScope{Kind: authorization.ScopeAll}, location)
			require.NoError(t, err)
			require.True(t, all.Matches("team", "agent"))
			for _, identity := range []struct{ location, name string }{{"", "agent"}, {"team", ""}, {" ", "agent"}, {"team", " "}} {
				require.False(t, all.Matches(identity.location, identity.name))
			}
			scope := authorization.AuthorizationScope{Kind: authorization.ScopeAnyOf, AnyOf: []authorization.ScopeClause{{All: []authorization.ScopePredicate{
				{Attribute: location, Operator: authorization.ScopeIn, Values: []string{"team"}},
			}}}}
			matcher, err := inventoryauth.CompileScope(scope, location)
			require.NoError(t, err)
			require.True(t, matcher.Matches("team", "agent"))
			require.False(t, matcher.Matches("other", "agent"))
			require.False(t, matcher.Matches("team", ""), "restricted scopes also require metadata names")

			other := authorization.AttributeNamespace
			if location == other {
				other = authorization.AttributeAtespace
			}
			_, err = inventoryauth.CompileScope(scope, other)
			require.Error(t, err, "namespace and atespace predicates must remain distinct")
			scope.AnyOf[0].All[0].Values = []string{" "}
			_, err = inventoryauth.CompileScope(scope, location)
			require.Error(t, err, "inventory predicates reject blank values")
		})
	}
	var zero inventoryauth.Matcher
	require.True(t, zero.Empty())
	require.False(t, zero.Matches("team", "agent"))
}

type collectionAuthorizer struct {
	auth.NoopAuthorizer
	location string
}

func (a collectionAuthorizer) Scope(context.Context, auth.Principal, auth.Verb, string) (authorization.AuthorizationScope, error) {
	return authorization.AuthorizationScope{Kind: authorization.ScopeAnyOf, AnyOf: []authorization.ScopeClause{{All: []authorization.ScopePredicate{
		{Attribute: a.location, Operator: authorization.ScopeIn, Values: []string{"team"}},
	}}}}, nil
}

func TestCollectionMatcherUsesInventoryValidation(t *testing.T) {
	ctx := auth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: auth.Principal{ID: "reader"}})
	matcher, err := inventoryauth.CollectionMatcher(ctx, collectionAuthorizer{location: authorization.AttributeAtespace}, "SubstrateActor", authorization.AttributeAtespace)
	require.NoError(t, err)
	require.True(t, matcher.Matches("team", "agent"))
	matcher, err = inventoryauth.CollectionMatcher(ctx, collectionAuthorizer{location: authorization.AttributeNamespace}, "SubstrateActor", authorization.AttributeAtespace)
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInternal))
	require.True(t, matcher.Empty())
}
