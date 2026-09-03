package kubeauth

import (
	"testing"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestScopeMatcher(t *testing.T) {
	object := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "agent-a"}}
	tests := []struct {
		name  string
		scope auth.AuthorizationScope
		want  bool
	}{
		{name: "all", scope: auth.AuthorizationScope{Kind: auth.ScopeAll}, want: true},
		{name: "none", scope: auth.AuthorizationScope{Kind: auth.ScopeNone}},
		{
			name: "or clauses",
			scope: auth.AuthorizationScope{Kind: auth.ScopeAnyOf, AnyOf: []auth.ScopeClause{
				{All: []auth.ScopePredicate{{Attribute: auth.AttributeName, Operator: auth.ScopeIn, Values: []string{"other"}}}},
				{All: []auth.ScopePredicate{{Attribute: auth.AttributeNamespace, Operator: auth.ScopeIn, Values: []string{"team-a"}}}},
			}},
			want: true,
		},
		{
			name: "and predicates",
			scope: auth.AuthorizationScope{Kind: auth.ScopeAnyOf, AnyOf: []auth.ScopeClause{{All: []auth.ScopePredicate{
				{Attribute: auth.AttributeNamespace, Operator: auth.ScopeIn, Values: []string{"team-a"}},
				{Attribute: auth.AttributeName, Operator: auth.ScopeIn, Values: []string{"agent-a"}},
			}}}},
			want: true,
		},
		{
			name: "and mismatch",
			scope: auth.AuthorizationScope{Kind: auth.ScopeAnyOf, AnyOf: []auth.ScopeClause{{All: []auth.ScopePredicate{
				{Attribute: auth.AttributeNamespace, Operator: auth.ScopeIn, Values: []string{"team-a"}},
				{Attribute: auth.AttributeName, Operator: auth.ScopeIn, Values: []string{"other"}},
			}}}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			matches, err := ScopeMatcher(test.scope)
			if err != nil {
				t.Fatalf("ScopeMatcher() error = %v", err)
			}
			if got := matches(object); got != test.want {
				t.Fatalf("matches() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestScopeMatcherRejectsInvalidScopes(t *testing.T) {
	tests := []auth.AuthorizationScope{
		{},
		{Kind: auth.ScopeAll, AnyOf: []auth.ScopeClause{{All: []auth.ScopePredicate{{Attribute: auth.AttributeName, Operator: auth.ScopeIn, Values: []string{"x"}}}}}},
		{Kind: auth.ScopeNone, AnyOf: []auth.ScopeClause{{All: []auth.ScopePredicate{{Attribute: auth.AttributeName, Operator: auth.ScopeIn, Values: []string{"x"}}}}}},
		{Kind: auth.ScopeAnyOf},
		{Kind: auth.ScopeAnyOf, AnyOf: []auth.ScopeClause{{}}},
		{Kind: auth.ScopeAnyOf, AnyOf: []auth.ScopeClause{{All: []auth.ScopePredicate{{Attribute: "label", Operator: auth.ScopeIn, Values: []string{"x"}}}}}},
		{Kind: auth.ScopeAnyOf, AnyOf: []auth.ScopeClause{{All: []auth.ScopePredicate{{Attribute: auth.AttributeName, Operator: "MISSING"}}}}},
		{Kind: auth.ScopeAnyOf, AnyOf: []auth.ScopeClause{{All: []auth.ScopePredicate{{Attribute: auth.AttributeName, Operator: "EQUALS", Values: []string{"x"}}}}}},
		{Kind: auth.ScopeAnyOf, AnyOf: []auth.ScopeClause{{All: []auth.ScopePredicate{{Attribute: auth.AttributeName, Operator: auth.ScopeIn}}}}},
		{Kind: auth.ScopeAnyOf, AnyOf: []auth.ScopeClause{{All: []auth.ScopePredicate{{Attribute: auth.AttributeName, Operator: auth.ScopeIn, Values: []string{""}}}}}},
	}

	for index, scope := range tests {
		if _, err := ScopeMatcher(scope); err == nil {
			t.Errorf("ScopeMatcher(invalid scope %d) error = nil", index)
		}
	}
}

func TestResourceUsesObjectMetadata(t *testing.T) {
	object := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "agent-a"}}
	resource := Resource("Harness", object)
	if resource.Type != "Harness" || resource.Name != "team-a/agent-a" {
		t.Fatalf("Resource() = %+v", resource)
	}
	if got := resource.Attributes[auth.AttributeNamespace]; len(got) != 1 || got[0] != "team-a" {
		t.Fatalf("namespace attribute = %v", got)
	}
	if got := resource.Attributes[auth.AttributeName]; len(got) != 1 || got[0] != "agent-a" {
		t.Fatalf("name attribute = %v", got)
	}
}
