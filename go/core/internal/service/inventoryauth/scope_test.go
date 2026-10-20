package inventoryauth_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/authorization"
	"github.com/kagent-dev/kagent/go/core/internal/service/inventoryauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubeauth"
)

func TestInventoryScopeGrammarAndRequiredFacts(t *testing.T) {
	predicate := func(attr string, values ...string) authorization.ScopePredicate {
		return authorization.ScopePredicate{Attribute: attr, Operator: authorization.ScopeIn, Values: values}
	}
	scope := authorization.AuthorizationScope{Kind: authorization.ScopeAnyOf, AnyOf: []authorization.ScopeClause{
		{All: []authorization.ScopePredicate{predicate(authorization.AttributeAtespace, "team"), predicate(authorization.AttributeName, "visible", "second")}},
		{All: []authorization.ScopePredicate{predicate(authorization.AttributeAtespace, "shared")}},
	}}
	matcher, err := inventoryauth.CompileScope(scope, authorization.AttributeAtespace)
	require.NoError(t, err)
	for _, tc := range []struct {
		space, name string
		want        bool
	}{{"team", "visible", true}, {"team", "second", true}, {"team", "hidden", false}, {"shared", "anything", true}, {"other", "visible", false}, {"", "visible", false}, {"team", "", false}} {
		require.Equal(t, tc.want, matcher.Matches(tc.space, tc.name), "%s/%s", tc.space, tc.name)
	}
	_, err = kubeauth.CompileScope(scope)
	require.Error(t, err, "Kubernetes must reject atespace predicates")
	scope.AnyOf[0].All[0].Values[0] = "changed"
	require.True(t, matcher.Matches("team", "visible"), "compiled scopes must not alias caller slices")
	for _, kind := range []authorization.ScopeKind{authorization.ScopeAll, authorization.ScopeNone} {
		m, err := inventoryauth.CompileScope(authorization.AuthorizationScope{Kind: kind}, authorization.AttributeNamespace)
		require.NoError(t, err)
		require.Equal(t, kind == authorization.ScopeNone, m.Empty())
		require.Equal(t, kind == authorization.ScopeAll, m.Matches("workers", "metadata-name"))
		require.False(t, m.Matches("", "metadata-name"))
		require.False(t, m.Matches("workers", ""))
	}
	var zero inventoryauth.Matcher
	require.True(t, zero.Empty())
	require.False(t, zero.Matches("space", "name"))
}

func TestInventoryScopeRejectsUnsupportedOrMalformedPredicates(t *testing.T) {
	valid := func() authorization.AuthorizationScope {
		return authorization.AuthorizationScope{Kind: authorization.ScopeAnyOf, AnyOf: []authorization.ScopeClause{{All: []authorization.ScopePredicate{{Attribute: authorization.AttributeAtespace, Operator: authorization.ScopeIn, Values: []string{"team"}}}}}}
	}
	for _, tc := range []struct {
		name   string
		change func(*authorization.AuthorizationScope)
	}{
		{"unknown kind", func(s *authorization.AuthorizationScope) { s.Kind = "INVALID" }},
		{"ALL with clauses", func(s *authorization.AuthorizationScope) { s.Kind = authorization.ScopeAll }},
		{"NONE with clauses", func(s *authorization.AuthorizationScope) { s.Kind = authorization.ScopeNone }},
		{"empty clauses", func(s *authorization.AuthorizationScope) { s.AnyOf = nil }},
		{"empty predicates", func(s *authorization.AuthorizationScope) { s.AnyOf[0].All = nil }},
		{"namespace is not atespace", func(s *authorization.AuthorizationScope) {
			s.AnyOf[0].All[0].Attribute = authorization.AttributeNamespace
		}},
		{"unknown attribute", func(s *authorization.AuthorizationScope) { s.AnyOf[0].All[0].Attribute = "pod" }},
		{"unknown operator", func(s *authorization.AuthorizationScope) { s.AnyOf[0].All[0].Operator = "NOT_IN" }},
		{"empty values", func(s *authorization.AuthorizationScope) { s.AnyOf[0].All[0].Values = nil }},
		{"empty value", func(s *authorization.AuthorizationScope) { s.AnyOf[0].All[0].Values = []string{""} }},
		{"blank value", func(s *authorization.AuthorizationScope) { s.AnyOf[0].All[0].Values = []string{" "} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := valid()
			tc.change(&s)
			_, err := inventoryauth.CompileScope(s, authorization.AttributeAtespace)
			require.Error(t, err)
		})
	}
	_, err := inventoryauth.CompileScope(valid(), "pod")
	require.Error(t, err)
	_, err = inventoryauth.CompileScope(valid(), authorization.AttributeNamespace)
	require.Error(t, err, "worker scopes must reject atespace")
}
