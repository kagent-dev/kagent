package accessreview_test

import (
	"context"
	"errors"
	"testing"

	apiauthorization "github.com/kagent-dev/kagent/go/api/authorization"
	"github.com/kagent-dev/kagent/go/core/internal/service/accessreview"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type testSession struct{ principal auth.Principal }

func (s testSession) Principal() auth.Principal { return s.principal }

type scopeCall struct {
	principal    auth.Principal
	verb         auth.Verb
	resourceType string
}

type testAuthorizer struct {
	scopes     map[auth.Verb]apiauthorization.AuthorizationScope
	scopeErrs  map[auth.Verb]error
	scopeCalls []scopeCall
	checkCalls int
}

func (a *testAuthorizer) Check(context.Context, auth.Principal, auth.Verb, auth.Resource) error {
	a.checkCalls++
	return nil
}

func (a *testAuthorizer) Scope(_ context.Context, principal auth.Principal, verb auth.Verb, resourceType string) (apiauthorization.AuthorizationScope, error) {
	a.scopeCalls = append(a.scopeCalls, scopeCall{principal: principal, verb: verb, resourceType: resourceType})
	return a.scopes[verb], a.scopeErrs[verb]
}

func TestCheckAccessMatrix(t *testing.T) {
	principal := auth.Principal{User: auth.User{ID: "reader"}}
	ctx := auth.AuthSessionTo(t.Context(), testSession{principal: principal})
	authorizer := &testAuthorizer{scopes: map[auth.Verb]apiauthorization.AuthorizationScope{
		auth.VerbUpdate: {
			Kind: apiauthorization.ScopeAnyOf,
			AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{
				{Attribute: apiauthorization.AttributeNamespace, Operator: apiauthorization.ScopeIn, Values: []string{"team-a"}},
				{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: []string{"assistant"}},
			}}},
		},
		auth.VerbCreate: {
			Kind: apiauthorization.ScopeAnyOf,
			AnyOf: []apiauthorization.ScopeClause{{All: []apiauthorization.ScopePredicate{
				{Attribute: apiauthorization.AttributeNamespace, Operator: apiauthorization.ScopeIn, Values: []string{"team-a"}},
			}}},
		},
	}}
	targets := []accessreview.Target{
		{Namespace: "team-a", Name: "assistant"},
		{Namespace: "team-b", Name: "assistant"},
		{Namespace: "team-a"},
		{Namespace: "team-a", Name: "other"},
	}

	results, err := accessreview.NewService(authorizer).Check(
		ctx,
		auth.ResourceAgentTemplate,
		[]auth.Verb{auth.VerbUpdate, auth.VerbCreate},
		targets,
	)
	require.NoError(t, err)
	assert.Equal(t, []accessreview.Result{
		{Target: targets[0], AllowedVerbs: []auth.Verb{auth.VerbUpdate, auth.VerbCreate}},
		{Target: targets[1]},
		{Target: targets[2], AllowedVerbs: []auth.Verb{auth.VerbUpdate, auth.VerbCreate}},
		{Target: targets[3], AllowedVerbs: []auth.Verb{auth.VerbCreate}},
	}, results)
	assert.Equal(t, []scopeCall{
		{principal: principal, verb: auth.VerbUpdate, resourceType: auth.ResourceAgentTemplate},
		{principal: principal, verb: auth.VerbCreate, resourceType: auth.ResourceAgentTemplate},
	}, authorizer.scopeCalls)
	assert.Zero(t, authorizer.checkCalls)
}

func TestCheckAccessScopeFailures(t *testing.T) {
	tests := []struct {
		name       string
		scope      apiauthorization.AuthorizationScope
		scopeError error
		wantCode   serviceerrors.Code
	}{
		{
			name:       "authorizer unavailable",
			scopeError: errors.New("unavailable"),
			wantCode:   serviceerrors.CodeUnavailable,
		},
		{
			name:     "malformed scope",
			scope:    apiauthorization.AuthorizationScope{},
			wantCode: serviceerrors.CodeInternal,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			authorizer := &testAuthorizer{
				scopes:    map[auth.Verb]apiauthorization.AuthorizationScope{auth.VerbCreate: test.scope},
				scopeErrs: map[auth.Verb]error{auth.VerbCreate: test.scopeError},
			}
			ctx := auth.AuthSessionTo(t.Context(), testSession{})

			_, err := accessreview.NewService(authorizer).Check(
				ctx,
				auth.ResourceModelConfig,
				[]auth.Verb{auth.VerbCreate},
				[]accessreview.Target{{Namespace: "team-a"}},
			)
			assert.True(t, serviceerrors.IsCode(err, test.wantCode), "error = %v", err)
		})
	}
}

func TestCheckAccessRequiresSession(t *testing.T) {
	_, err := accessreview.NewService(&testAuthorizer{}).Check(
		t.Context(),
		auth.ResourceAgentTemplate,
		[]auth.Verb{auth.VerbGet},
		[]accessreview.Target{{Namespace: "team-a", Name: "assistant"}},
	)
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnauthenticated), "error = %v", err)
}
