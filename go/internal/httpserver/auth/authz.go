package auth

import (
	"context"
)

func DefaultAuthorizer() Authorizer {
	return &NothingAuthorizer{}
}

type Authorizer interface {
	Check(ctx context.Context, principal Principal, verb Verb, resource Resource) error
}

type NothingAuthorizer struct{}

func (a *NothingAuthorizer) Check(ctx context.Context, principal Principal, verb Verb, resource Resource) error {
	return nil
}
