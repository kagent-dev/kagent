package auth

import (
	"context"
)

type Authorizer interface {
	Check(ctx context.Context, principal Principal, verb Verb, resource Resource) error
}

type NoopAuthorizer struct{}

func (a *NoopAuthorizer) Check(ctx context.Context, principal Principal, verb Verb, resource Resource) error {
	return nil
}
