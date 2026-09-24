package kubeauth_test

import (
	"testing"

	apiauthorization "github.com/kagent-dev/kagent/go/api/authorization"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubeauth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestMatcher(t *testing.T) {
	object := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "agent-a"}}
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
				{Attribute: apiauthorization.AttributeNamespace, Operator: apiauthorization.ScopeIn, Values: []string{"team-a"}},
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
			matcher, err := kubeauth.CompileScope(test.scope)
			if err != nil {
				t.Fatalf("CompileScope() error = %v", err)
			}
			if got := matcher.Matches(object); got != test.want {
				t.Fatalf("Matches() = %v, want %v", got, test.want)
			}
		})
	}
	if (kubeauth.Matcher{}).Matches(object) {
		t.Fatal("zero Matcher matches object")
	}
}

func TestMatcherOwnsCompiledScope(t *testing.T) {
	values := []string{"team-a"}
	predicates := []apiauthorization.ScopePredicate{{
		Attribute: apiauthorization.AttributeNamespace,
		Operator:  apiauthorization.ScopeIn,
		Values:    values,
	}}
	clauses := []apiauthorization.ScopeClause{{All: predicates}}
	matcher, err := kubeauth.CompileScope(apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: clauses})
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

	object := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "agent-a"}}
	if !matcher.Matches(object) {
		t.Fatal("matcher changed after its source scope was mutated")
	}
}

func TestMatcherMatchesAnyName(t *testing.T) {
	tests := []struct {
		name      string
		namespace string
		scope     apiauthorization.AuthorizationScope
		want      bool
	}{
		{name: "all", scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAll}, want: true},
		{name: "none", scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeNone}},
		{name: "invalid namespace", namespace: "NOT A NAMESPACE", scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAll}},
		{
			name:  "namespace",
			scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeNamespace, Operator: apiauthorization.ScopeIn, Values: []string{"team-a"}}}}}},
			want:  true,
		},
		{
			name:  "different namespace",
			scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeNamespace, Operator: apiauthorization.ScopeIn, Values: []string{"team-b"}}}}}},
		},
		{
			name: "intersecting names",
			scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{
				{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{"agent-a", "agent-b"}},
				{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{"agent-b"}},
			}}}},
			want: true,
		},
		{
			name: "disjoint names",
			scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{
				{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{"agent-a"}},
				{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{"agent-b"}},
			}}}},
		},
		{
			name:  "invalid resource name",
			scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{"NOT A NAME"}}}}}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			namespace := test.namespace
			if namespace == "" {
				namespace = "team-a"
			}
			matcher, err := kubeauth.CompileScope(test.scope)
			if err != nil {
				t.Fatalf("CompileScope() error = %v", err)
			}
			if got := matcher.MatchesAnyName(namespace); got != test.want {
				t.Fatalf("MatchesAnyName() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestCompileScopeRejectsInvalidScopes(t *testing.T) {
	tests := []struct {
		name  string
		scope apiauthorization.AuthorizationScope
	}{
		{name: "missing kind"},
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
			if _, err := kubeauth.CompileScope(test.scope); err == nil {
				t.Error("CompileScope() error = nil")
			}
		})
	}
}
