package auth

import (
	"context"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

type NoopAuthorizer struct{}

func (a *NoopAuthorizer) Check(ctx context.Context, principal auth.Principal, verb auth.Verb, resource auth.Resource) error {
	return nil
}

func (a *NoopAuthorizer) Scope(ctx context.Context, principal auth.Principal, verb auth.Verb, resourceType string) (auth.AuthorizationScope, error) {
	return auth.AuthorizationScope{Kind: auth.ScopeAll}, nil
}

var _ auth.CollectionAuthorizer = (*NoopAuthorizer)(nil)
