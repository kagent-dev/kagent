package kubeauth

import (
	"testing"

	apiauthorization "github.com/kagent-dev/kagent/go/api/authorization"
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
			matcher, err := CompileScope(test.scope)
			if err != nil {
				t.Fatalf("CompileScope() error = %v", err)
			}
			if got := matcher.Matches(object); got != test.want {
				t.Fatalf("Matches() = %v, want %v", got, test.want)
			}
		})
	}
	if (Matcher{}).Matches(object) {
		t.Fatal("zero Matcher matches object")
	}
}

func TestCompileScopeRejectsInvalidScopes(t *testing.T) {
	tests := []apiauthorization.AuthorizationScope{
		{},
		{Kind: apiauthorization.ScopeAll, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{"x"}}}}}},
		{Kind: apiauthorization.ScopeNone, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{"x"}}}}}},
		{Kind: apiauthorization.ScopeAnyOf},
		{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{}}},
		{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: "label", Operator: apiauthorization.ScopeIn, Values: []string{"x"}}}}}},
		{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeName, Operator: "MISSING"}}}}},
		{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeName, Operator: "EQUALS", Values: []string{"x"}}}}}},
		{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn}}}}},
		{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{""}}}}}},
	}

	for index, scope := range tests {
		if _, err := CompileScope(scope); err == nil {
			t.Errorf("CompileScope(invalid scope %d) error = nil", index)
		}
	}
}

func TestResourceUsesObjectMetadata(t *testing.T) {
	object := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: "team-a", Name: "agent-a"}}
	resource := Resource("Harness", object)
	if resource.Type != "Harness" || resource.Name != "team-a/agent-a" {
		t.Fatalf("Resource() = %+v", resource)
	}
	if got := resource.Attributes[apiauthorization.AttributeNamespace]; len(got) != 1 || got[0] != "team-a" {
		t.Fatalf("namespace attribute = %v", got)
	}
	if got := resource.Attributes[apiauthorization.AttributeName]; len(got) != 1 || got[0] != "agent-a" {
		t.Fatalf("name attribute = %v", got)
	}
}
