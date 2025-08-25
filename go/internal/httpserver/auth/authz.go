package auth

import (
	"context"

	"github.com/kagent-dev/kagent/go/pkg/auth"
)

type NoopAuthorizer struct{}

func (a *NoopAuthorizer) Check(ctx context.Context, principal auth.Principal, verb auth.Verb, resource auth.Resource) error {
	return nil
}

var _ auth.Authorizer = (*NoopAuthorizer)(nil)
