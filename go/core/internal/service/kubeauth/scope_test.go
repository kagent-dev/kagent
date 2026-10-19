package kubeauth_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiauthorization "github.com/kagent-dev/kagent/go/api/authorization"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubeauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
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

type collectionAuthorizer struct {
	auth.NoopAuthorizer
	scope     apiauthorization.AuthorizationScope
	err       error
	principal auth.Principal
	verb      auth.Verb
	resource  string
}

func (a *collectionAuthorizer) Scope(_ context.Context, principal auth.Principal, verb auth.Verb, resource string) (apiauthorization.AuthorizationScope, error) {
	a.principal, a.verb, a.resource = principal, verb, resource
	return a.scope, a.err
}

func TestCollectionMatcher(t *testing.T) {
	principal := auth.Principal{User: auth.User{ID: "reader"}}
	ctx := auth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: principal})
	for _, test := range []struct {
		name    string
		scope   apiauthorization.AuthorizationScope
		err     error
		code    serviceerrors.Code
		matches bool
	}{
		{name: "all", scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAll}, matches: true},
		{name: "none", scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeNone}},
		{name: "invalid", scope: apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf}, code: serviceerrors.CodeInternal},
		{name: "unavailable", err: errors.New("evaluator unavailable"), code: serviceerrors.CodeUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			authorizer := &collectionAuthorizer{scope: test.scope, err: test.err}
			matcher, err := kubeauth.CollectionMatcher(ctx, authorizer, "WorkerPool")
			if test.code != "" {
				require.True(t, serviceerrors.IsCode(err, test.code))
				require.False(t, matcher.Matches(&metav1.ObjectMeta{Namespace: "team", Name: "pool"}))
				if test.err != nil {
					require.ErrorIs(t, err, test.err)
				}
			} else {
				require.NoError(t, err)
				require.Equal(t, test.matches, matcher.Matches(&metav1.ObjectMeta{Namespace: "team", Name: "pool"}))
				require.Equal(t, test.scope.Kind == apiauthorization.ScopeNone, matcher.Empty())
			}
			require.Equal(t, principal, authorizer.principal)
			require.Equal(t, auth.VerbList, authorizer.verb)
			require.Equal(t, "WorkerPool", authorizer.resource)
		})
	}
	_, err := kubeauth.CollectionMatcher(t.Context(), auth.NoopAuthorizer{}, "WorkerPool")
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnauthenticated))
	_, err = kubeauth.CollectionMatcher(ctx, nil, "WorkerPool")
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInternal))
}
