package authscope_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kagent-dev/kagent/go/api/authorization"
	authimpl "github.com/kagent-dev/kagent/go/core/internal/httpserver/auth"
	"github.com/kagent-dev/kagent/go/core/internal/service/authscope"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type collectionAuthorizer struct {
	auth.NoopAuthorizer
	scope     authorization.AuthorizationScope
	err       error
	principal auth.Principal
	verb      auth.Verb
	resource  string
}

func (a *collectionAuthorizer) Scope(_ context.Context, principal auth.Principal, verb auth.Verb, resource string) (authorization.AuthorizationScope, error) {
	a.principal, a.verb, a.resource = principal, verb, resource
	return a.scope, a.err
}

func TestCollectionScope(t *testing.T) {
	principal := auth.Principal{ID: "reader"}
	ctx := auth.AuthSessionTo(t.Context(), &authimpl.SimpleSession{P: principal})
	for _, test := range []struct {
		name  string
		scope authorization.AuthorizationScope
		err   error
	}{
		{name: "all", scope: authorization.AuthorizationScope{Kind: authorization.ScopeAll}},
		{name: "none", scope: authorization.AuthorizationScope{Kind: authorization.ScopeNone}},
		{name: "unavailable", err: errors.New("evaluator unavailable")},
	} {
		t.Run(test.name, func(t *testing.T) {
			authorizer := &collectionAuthorizer{scope: test.scope, err: test.err}
			scope, err := authscope.CollectionScope(ctx, authorizer, "WorkerPool")
			if test.err != nil {
				require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnavailable))
				require.ErrorIs(t, err, test.err)
				require.Empty(t, scope)
			} else {
				require.NoError(t, err)
				require.Equal(t, test.scope, scope)
			}
			require.Equal(t, principal, authorizer.principal)
			require.Equal(t, auth.VerbList, authorizer.verb)
			require.Equal(t, "WorkerPool", authorizer.resource)
		})
	}
	_, err := authscope.CollectionScope(t.Context(), auth.NoopAuthorizer{}, "WorkerPool")
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeUnauthenticated))
	_, err = authscope.CollectionScope(ctx, nil, "WorkerPool")
	require.True(t, serviceerrors.IsCode(err, serviceerrors.CodeInternal))
}
