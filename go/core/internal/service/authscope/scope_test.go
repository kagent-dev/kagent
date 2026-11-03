package authscope_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	apiauthorization "github.com/kagent-dev/kagent/go/api/authorization"
	"github.com/kagent-dev/kagent/go/core/internal/service/authscope"
)

func TestMatcher(t *testing.T) {
	location, name := "team-a", "agent-a"
	tests := []struct {
		name  string
		scope apiauthorization.AuthorizationScope
		want  bool
	}{
		{name: "all", scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAll}, want: true},
		{name: "none", scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeNone}},
		{
			name: "or clauses",
			scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{
				{All: []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{"other"}}}},
				{All: []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeNamespace, Operator: apiauthorization.ScopeIn, Values: []string{"team-a"}}}},
			}},
			want: true,
		},
		{
			name: "and predicates",
			scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{
				{Attribute: apiauthorization.AttributeNamespace, Operator: apiauthorization.ScopeIn, Values: []string{"other", "team-a"}},
				{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{"agent-a"}},
			}}}},
			want: true,
		},
		{
			name: "and mismatch",
			scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{
				{Attribute: apiauthorization.AttributeNamespace, Operator: apiauthorization.ScopeIn, Values: []string{"team-a"}},
				{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{"other"}},
			}}}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			matcher, err := authscope.CompileScope(test.scope, apiauthorization.AttributeNamespace)
			if err != nil {
				t.Fatalf("CompileScope() error = %v", err)
			}
			if got := matcher.Matches(location, name); got != test.want {
				t.Fatalf("Matches() = %v, want %v", got, test.want)
			}
			require.Equal(t, test.scope.Kind == apiauthorization.ScopeNone, matcher.Empty())
		})
	}
	if (authscope.Matcher{}).Matches(location, name) {
		t.Fatal("zero Matcher matches object")
	}
	require.True(t, (authscope.Matcher{}).Empty())
}

func TestMatcherOwnsCompiledScope(t *testing.T) {
	values := []string{"team-a"}
	predicates := []apiauthorization.ScopePredicate{{
		Attribute: apiauthorization.AttributeNamespace,
		Operator:  apiauthorization.ScopeIn,
		Values:    values,
	}}
	clauses := []apiauthorization.ScopeClause{{All: predicates}}
	matcher, err := authscope.CompileScope(apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: clauses}, apiauthorization.AttributeNamespace)
	if err != nil {
		t.Fatal(err)
	}

	clauses[0] = apiauthorization.ScopeClause{All: []apiauthorization.ScopePredicate{{
		Attribute: apiauthorization.AttributeName,
		Operator:  apiauthorization.ScopeIn,
		Values:    []string{"other"},
	}}}
	predicates[0] = apiauthorization.ScopePredicate{
		Attribute: apiauthorization.AttributeName,
		Operator:  apiauthorization.ScopeIn,
		Values:    []string{"other"},
	}
	values[0] = "other"

	location, name := "team-a", "agent-a"
	if !matcher.Matches(location, name) {
		t.Fatal("matcher changed after its source scope was mutated")
	}
}

func TestCompileScopeRejectsInvalidScopes(t *testing.T) {
	tests := []struct {
		name  string
		scope apiauthorization.AuthorizationScope
	}{
		{name: "missing kind"},
		{name: "unknown kind", scope: apiauthorization.AuthorizationScope{Kind: "INVALID"}},
		{name: "all with clauses", scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAll, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{"x"}}}}}}},
		{name: "none with clauses", scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeNone, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{"x"}}}}}}},
		{name: "any of with no clauses", scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf}},
		{name: "empty clause", scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{}}}},
		{name: "unknown attribute", scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: "label", Operator: apiauthorization.ScopeIn, Values: []string{"x"}}}}}}},
		{name: "unknown operator", scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeName, Operator: "MISSING"}}}}}},
		{name: "unsupported operator", scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeName, Operator: "EQUALS", Values: []string{"x"}}}}}}},
		{name: "missing values", scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn}}}}}},
		{name: "empty value", scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{""}}}}}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := authscope.CompileScope(test.scope, apiauthorization.AttributeNamespace); err == nil {
				t.Error("CompileScope() error = nil")
			}
		})
	}
	_, err := authscope.CompileScope(apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAll}, "pod")
	require.Error(t, err, "unsupported location attributes must fail even for ALL")
}
