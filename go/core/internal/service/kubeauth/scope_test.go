package kubeauth_test

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kagent-dev/kagent/go/api/authorization"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubeauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

func TestKubernetesIdentities(t *testing.T) {
	scope := authorization.AuthorizationScope{Kind: authorization.ScopeAnyOf, AnyOf: []authorization.ScopeClause{{All: []authorization.ScopePredicate{
		{Attribute: authorization.AttributeName, Operator: authorization.ScopeIn, Values: []string{"agent"}},
	}}}}
	matcher, err := kubeauth.CompileScope(scope)
	require.NoError(t, err)
	require.True(t, matcher.Matches(&metav1.ObjectMeta{Namespace: "team", Name: "agent"}))
	require.True(t, matcher.Matches(&metav1.ObjectMeta{Name: "agent"}), "name-only scopes support cluster-scoped objects")
	require.False(t, matcher.Matches(&metav1.ObjectMeta{Namespace: "team", Name: "other"}))

	scope.AnyOf[0].All = append(scope.AnyOf[0].All, authorization.ScopePredicate{Attribute: authorization.AttributeNamespace, Operator: authorization.ScopeIn, Values: []string{"team"}})
	matcher, err = kubeauth.CompileScope(scope)
	require.NoError(t, err)
	require.True(t, matcher.Matches(&metav1.ObjectMeta{Namespace: "team", Name: "agent"}))
	require.False(t, matcher.Matches(&metav1.ObjectMeta{Namespace: "other", Name: "agent"}))
	require.False(t, matcher.Matches(&metav1.ObjectMeta{Name: "agent"}))

	all, err := kubeauth.CompileScope(authorization.AuthorizationScope{Kind: authorization.ScopeAll})
	require.NoError(t, err)
	require.True(t, all.Matches(&metav1.ObjectMeta{}), "ALL retains Kubernetes identity semantics")
	var zero kubeauth.Matcher
	require.True(t, zero.Empty())
	require.False(t, zero.Matches(nil))
}

func TestKubernetesScopeAttributeValidation(t *testing.T) {
	scope := authorization.AuthorizationScope{Kind: authorization.ScopeAnyOf, AnyOf: []authorization.ScopeClause{{All: []authorization.ScopePredicate{
		{Attribute: authorization.AttributeAtespace, Operator: authorization.ScopeIn, Values: []string{"team"}},
	}}}}
	_, err := kubeauth.CompileScope(scope)
	require.Error(t, err, "Kubernetes must not accept atespace predicates")
	// Preserve the existing policy grammar: only inventory rejects blank values.
	scope.AnyOf[0].All[0].Attribute = authorization.AttributeNamespace
	scope.AnyOf[0].All[0].Values = []string{" "}
	_, err = kubeauth.CompileScope(scope)
	require.NoError(t, err)
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

func TestCollectionMatcherUsesKubernetesValidation(t *testing.T) {
	ctx := auth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: auth.Principal{ID: "reader"}})
	matcher, err := kubeauth.CollectionMatcher(ctx, collectionAuthorizer{location: authorization.AttributeNamespace}, "WorkerPool")
	require.NoError(t, err)
	require.True(t, matcher.Matches(&metav1.ObjectMeta{Namespace: "team", Name: "pool"}))
	matcher, err = kubeauth.CollectionMatcher(ctx, collectionAuthorizer{location: authorization.AttributeAtespace}, "WorkerPool")
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInternal))
	require.True(t, matcher.Empty())
}
