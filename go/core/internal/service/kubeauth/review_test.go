package kubeauth_test

import (
	"context"
	"errors"
	"fmt"
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
		auth.VerbGet: {Kind: apiauthorization.ScopeAll},
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
		{principal: principal, verb: auth.VerbGet, resourceType: auth.ResourceAgentTemplate},
		{principal: principal, verb: auth.VerbCreate, resourceType: auth.ResourceAgentTemplate},
	}, authorizer.scopeCalls)
	assert.Equal(t, []checkCall{
		{principal: principal, verb: auth.VerbUpdate, resource: auth.Resource{Type: auth.ResourceAgentTemplate, Namespace: "team-a", Name: "assistant"}},
		{principal: principal, verb: auth.VerbGet, resource: auth.Resource{Type: auth.ResourceAgentTemplate, Namespace: "team-a", Name: "assistant"}},
		{principal: principal, verb: auth.VerbUpdate, resource: auth.Resource{Type: auth.ResourceAgentTemplate, Namespace: "team-b", Name: "assistant"}},
		{principal: principal, verb: auth.VerbUpdate, resource: auth.Resource{Type: auth.ResourceAgentTemplate, Namespace: "team-a", Name: "other"}},
		{principal: principal, verb: auth.VerbCreate, resource: auth.Resource{Type: auth.ResourceAgentTemplate, Namespace: "team-a", Name: "assistant"}},
		{principal: principal, verb: auth.VerbCreate, resource: auth.Resource{Type: auth.ResourceAgentTemplate, Namespace: "team-b", Name: "assistant"}},
		{principal: principal, verb: auth.VerbCreate, resource: auth.Resource{Type: auth.ResourceAgentTemplate, Namespace: "team-a", Name: "other"}},
	}, authorizer.checkCalls)
}

func TestCheckAccessUpdateRequiresGet(t *testing.T) {
	for _, resourceType := range []string{auth.ResourceAgent, auth.ResourceAgentTemplate, auth.ResourceModelConfig} {
		for _, deniedVerb := range []auth.Verb{"", auth.VerbGet, auth.VerbUpdate} {
			t.Run(resourceType+"/denied="+string(deniedVerb), func(t *testing.T) {
				target := kubeauth.ReviewTarget{Namespace: "team-a", Name: "assistant"}
				authorizer := &testAuthorizer{checkErrs: map[checkKey]error{
					{verb: deniedVerb, resourceType: resourceType, namespace: target.Namespace, name: target.Name}: errors.New("denied"),
				}}
				ctx := auth.AuthSessionTo(t.Context(), testSession{})
				results, err := kubeauth.NewAccessReviewer(authorizer).Review(ctx, resourceType, []auth.Verb{auth.VerbUpdate}, []kubeauth.ReviewTarget{target})
				require.NoError(t, err)
				want := kubeauth.ReviewResult{Target: target}
				if deniedVerb == "" {
					want.AllowedVerbs = []auth.Verb{auth.VerbUpdate}
				}
				assert.Equal(t, []kubeauth.ReviewResult{want}, results)
				assert.Empty(t, authorizer.scopeCalls)
			})
		}
	}
}

func TestCheckAccessUpdateRequiresScopeOverlap(t *testing.T) {
	scope := func(namespace string, names ...string) apiauthorization.AuthorizationScope {
		predicates := []apiauthorization.ScopePredicate{{Attribute: apiauthorization.AttributeNamespace, Operator: apiauthorization.ScopeIn, Values: []string{namespace}}}
		if len(names) != 0 {
			predicates = append(predicates, apiauthorization.ScopePredicate{Attribute: apiauthorization.AttributeName, Operator: apiauthorization.ScopeIn, Values: names})
		}
		return apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAnyOf, AnyOf: []apiauthorization.ScopeClause{{All: predicates}}}
	}
	all := apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeAll}
	none := apiauthorization.AuthorizationScope{Kind: apiauthorization.ScopeNone}
	assistant := scope("team-a", "assistant")
	for _, test := range []struct {
		name        string
		get, update apiauthorization.AuthorizationScope
		allowed     bool
	}{
		{name: "both unrestricted", get: all, update: all, allowed: true},
		{name: "get unrestricted", get: all, update: assistant, allowed: true},
		{name: "update unrestricted", get: assistant, update: all, allowed: true},
		{name: "get denied", get: none, update: assistant},
		{name: "update denied", get: assistant, update: none},
		{name: "same name", get: assistant, update: assistant, allowed: true},
		{name: "disjoint names", get: assistant, update: scope("team-a", "other")},
		{name: "overlapping names", get: assistant, update: scope("team-a", "other", "assistant"), allowed: true},
		{name: "different namespaces", get: assistant, update: scope("team-b", "assistant")},
		{name: "namespace unrestricted", get: scope("team-a"), update: assistant, allowed: true},
		{name: "invalid common name", get: scope("team-a", "INVALID", "reader"), update: scope("team-a", "INVALID", "writer")},
	} {
		t.Run(test.name, func(t *testing.T) {
			authorizer := &testAuthorizer{scopes: map[auth.Verb]apiauthorization.AuthorizationScope{
				auth.VerbGet: test.get, auth.VerbUpdate: test.update,
			}}
			ctx := auth.AuthSessionTo(t.Context(), testSession{})
			targets := []kubeauth.ReviewTarget{{Namespace: "team-a"}, {Namespace: "team-b"}}
			results, err := kubeauth.NewAccessReviewer(authorizer).Review(ctx, auth.ResourceAgent, []auth.Verb{auth.VerbUpdate}, targets)
			require.NoError(t, err)
			assert.Equal(t, test.allowed, len(results[0].AllowedVerbs) != 0)
			assert.Equal(t, []scopeCall{
				{verb: auth.VerbUpdate, resourceType: auth.ResourceAgent},
				{verb: auth.VerbGet, resourceType: auth.ResourceAgent},
			}, authorizer.scopeCalls)
			assert.Empty(t, authorizer.checkCalls)
		})
	}
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
		for _, scopeVerb := range []auth.Verb{auth.VerbCreate, auth.VerbUpdate, auth.VerbGet} {
			t.Run(test.name+"/"+string(scopeVerb), func(t *testing.T) {
				authorizer := &testAuthorizer{
					scopes: map[auth.Verb]apiauthorization.AuthorizationScope{
						auth.VerbUpdate: {Kind: apiauthorization.ScopeAll},
						auth.VerbGet:    {Kind: apiauthorization.ScopeAll},
					},
					scopeErrs: map[auth.Verb]error{scopeVerb: test.scopeError},
				}
				authorizer.scopes[scopeVerb] = test.scope
				verb := scopeVerb
				if verb == auth.VerbGet {
					verb = auth.VerbUpdate
				}
				ctx := auth.AuthSessionTo(t.Context(), testSession{})

				_, err := kubeauth.NewAccessReviewer(authorizer).Review(
					ctx,
					auth.ResourceModelConfig,
					[]auth.Verb{verb},
					[]kubeauth.ReviewTarget{{Namespace: "team-a"}},
				)
				assert.True(t, serviceerrors.IsCode(err, test.wantCode), "error = %v", err)
			})
		}
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

type cancelingAuthorizer struct {
	testAuthorizer
	cancel context.CancelFunc
}

func (a *cancelingAuthorizer) Check(ctx context.Context, principal auth.Principal, verb auth.Verb, resource auth.Resource) error {
	a.cancel()
	return a.testAuthorizer.Check(ctx, principal, verb, resource)
}

func (a *cancelingAuthorizer) Scope(ctx context.Context, principal auth.Principal, verb auth.Verb, resourceType string) (apiauthorization.AuthorizationScope, error) {
	a.cancel()
	return a.testAuthorizer.Scope(ctx, principal, verb, resourceType)
}

func TestCheckAccessCancellation(t *testing.T) {
	for _, name := range []string{"", "assistant"} {
		for _, count := range []int{1, 2} {
			t.Run(fmt.Sprintf("name=%s/targets=%d", name, count), func(t *testing.T) {
				ctx, cancel := context.WithCancel(auth.AuthSessionTo(t.Context(), testSession{}))
				defer cancel()
				authorizer := &cancelingAuthorizer{
					testAuthorizer: testAuthorizer{scopes: map[auth.Verb]apiauthorization.AuthorizationScope{
						auth.VerbCreate: {Kind: apiauthorization.ScopeAll},
					}},
					cancel: cancel,
				}
				targets := make([]kubeauth.ReviewTarget, count)
				for i := range targets {
					targets[i] = kubeauth.ReviewTarget{Namespace: "team-a", Name: name}
				}
				reviewer := kubeauth.NewAccessReviewer(authorizer)
				results, err := reviewer.Review(ctx, auth.ResourceAgentTemplate, []auth.Verb{auth.VerbCreate}, targets)
				require.ErrorIs(t, err, context.Canceled)
				assert.Nil(t, results)
				assert.Equal(t, 1, len(authorizer.checkCalls)+len(authorizer.scopeCalls))

				_, err = reviewer.Review(ctx, auth.ResourceAgentTemplate, []auth.Verb{auth.VerbCreate}, targets)
				require.ErrorIs(t, err, context.Canceled)
				assert.Equal(t, 1, len(authorizer.checkCalls)+len(authorizer.scopeCalls))
			})
		}
	}
}

var _ auth.CollectionAuthorizer = (*cancelingAuthorizer)(nil)
