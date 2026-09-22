package kubeauth_test

import (
	"context"
	"errors"
	"testing"

	apiauthorization "github.com/kagent-dev/kagent/go/api/authorization"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubeauth"
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

type checkCall struct {
	principal auth.Principal
	verb      auth.Verb
	resource  auth.Resource
}

type checkKey struct {
	verb                          auth.Verb
	resourceType, namespace, name string
}

type testAuthorizer struct {
	scopes     map[auth.Verb]apiauthorization.AuthorizationScope
	scopeErrs  map[auth.Verb]error
	scopeCalls []scopeCall
	checkErrs  map[checkKey]error
	checkCalls []checkCall
}

func (a *testAuthorizer) Check(_ context.Context, principal auth.Principal, verb auth.Verb, resource auth.Resource) error {
	a.checkCalls = append(a.checkCalls, checkCall{principal: principal, verb: verb, resource: resource})
	return a.checkErrs[checkKey{verb: verb, resourceType: resource.Type, namespace: resource.Namespace, name: resource.Name}]
}

func (a *testAuthorizer) Scope(_ context.Context, principal auth.Principal, verb auth.Verb, resourceType string) (apiauthorization.AuthorizationScope, error) {
	a.scopeCalls = append(a.scopeCalls, scopeCall{principal: principal, verb: verb, resourceType: resourceType})
	return a.scopes[verb], a.scopeErrs[verb]
}

func TestCheckAccessMatrix(t *testing.T) {
	principal := auth.Principal{User: auth.User{ID: "reader"}}
	ctx := auth.AuthSessionTo(t.Context(), testSession{principal: principal})
	denied := errors.New("denied")
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
	}, checkErrs: map[checkKey]error{
		{verb: auth.VerbUpdate, resourceType: auth.ResourceAgentTemplate, namespace: "team-b", name: "assistant"}: denied,
		{verb: auth.VerbCreate, resourceType: auth.ResourceAgentTemplate, namespace: "team-b", name: "assistant"}: denied,
		{verb: auth.VerbUpdate, resourceType: auth.ResourceAgentTemplate, namespace: "team-a", name: "other"}:     denied,
	}}
	targets := []kubeauth.ReviewTarget{
		{Namespace: "team-a", Name: "assistant"},
		{Namespace: "team-b", Name: "assistant"},
		{Namespace: "team-a"},
		{Namespace: "team-a", Name: "other"},
	}

	results, err := kubeauth.NewAccessReviewer(authorizer).Review(
		ctx,
		auth.ResourceAgentTemplate,
		[]auth.Verb{auth.VerbUpdate, auth.VerbCreate},
		targets,
	)
	require.NoError(t, err)
	assert.Equal(t, []kubeauth.ReviewResult{
		{Target: targets[0], AllowedVerbs: []auth.Verb{auth.VerbUpdate, auth.VerbCreate}},
		{Target: targets[1]},
		{Target: targets[2], AllowedVerbs: []auth.Verb{auth.VerbUpdate, auth.VerbCreate}},
		{Target: targets[3], AllowedVerbs: []auth.Verb{auth.VerbCreate}},
	}, results)
	assert.Equal(t, []scopeCall{
		{principal: principal, verb: auth.VerbUpdate, resourceType: auth.ResourceAgentTemplate},
		{principal: principal, verb: auth.VerbCreate, resourceType: auth.ResourceAgentTemplate},
	}, authorizer.scopeCalls)
	assert.Equal(t, []checkCall{
		{principal: principal, verb: auth.VerbUpdate, resource: auth.Resource{Type: auth.ResourceAgentTemplate, Namespace: "team-a", Name: "assistant"}},
		{principal: principal, verb: auth.VerbUpdate, resource: auth.Resource{Type: auth.ResourceAgentTemplate, Namespace: "team-b", Name: "assistant"}},
		{principal: principal, verb: auth.VerbUpdate, resource: auth.Resource{Type: auth.ResourceAgentTemplate, Namespace: "team-a", Name: "other"}},
		{principal: principal, verb: auth.VerbCreate, resource: auth.Resource{Type: auth.ResourceAgentTemplate, Namespace: "team-a", Name: "assistant"}},
		{principal: principal, verb: auth.VerbCreate, resource: auth.Resource{Type: auth.ResourceAgentTemplate, Namespace: "team-b", Name: "assistant"}},
		{principal: principal, verb: auth.VerbCreate, resource: auth.Resource{Type: auth.ResourceAgentTemplate, Namespace: "team-a", Name: "other"}},
	}, authorizer.checkCalls)
}

func TestCheckAccessNamedTargetsDoNotReadScope(t *testing.T) {
	authorizer := &testAuthorizer{scopeErrs: map[auth.Verb]error{auth.VerbGet: errors.New("scope unavailable")}}
	ctx := auth.AuthSessionTo(t.Context(), testSession{})
	target := kubeauth.ReviewTarget{Namespace: "team-a", Name: "assistant"}

	results, err := kubeauth.NewAccessReviewer(authorizer).Review(
		ctx,
		auth.ResourceAgentTemplate,
		[]auth.Verb{auth.VerbGet},
		[]kubeauth.ReviewTarget{target},
	)
	require.NoError(t, err)
	assert.Equal(t, []kubeauth.ReviewResult{{Target: target, AllowedVerbs: []auth.Verb{auth.VerbGet}}}, results)
	assert.Empty(t, authorizer.scopeCalls)
}

func TestCheckAccessHonorsReadOnlyShare(t *testing.T) {
	authorizer := &testAuthorizer{}
	ctx := auth.AuthSessionTo(t.Context(), testSession{})
	ctx = auth.ShareContextTo(ctx, &auth.ShareContext{ReadOnly: true})
	target := kubeauth.ReviewTarget{Namespace: "team-a", Name: "assistant"}

	results, err := kubeauth.NewAccessReviewer(authorizer).Review(
		ctx,
		auth.ResourceAgentTemplate,
		[]auth.Verb{auth.VerbGet, auth.VerbCreate, auth.VerbUpdate, auth.VerbDelete},
		[]kubeauth.ReviewTarget{target},
	)
	require.NoError(t, err)
	assert.Equal(t, []kubeauth.ReviewResult{{Target: target, AllowedVerbs: []auth.Verb{auth.VerbGet}}}, results)
	assert.Equal(t, []checkCall{{verb: auth.VerbGet, resource: auth.Resource{Type: auth.ResourceAgentTemplate, Namespace: "team-a", Name: "assistant"}}}, authorizer.checkCalls)
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

			_, err := kubeauth.NewAccessReviewer(authorizer).Review(
				ctx,
				auth.ResourceModelConfig,
				[]auth.Verb{auth.VerbCreate},
				[]kubeauth.ReviewTarget{{Namespace: "team-a"}},
			)
			assert.True(t, serviceerrors.IsCode(err, test.wantCode), "error = %v", err)
		})
	}
}

func TestCheckAccessRequiresSession(t *testing.T) {
	_, err := kubeauth.NewAccessReviewer(&testAuthorizer{}).Review(
		t.Context(),
		auth.ResourceAgentTemplate,
		[]auth.Verb{auth.VerbGet},
		[]kubeauth.ReviewTarget{{Namespace: "team-a", Name: "assistant"}},
	)
	assert.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnauthenticated), "error = %v", err)
}
