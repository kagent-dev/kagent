package auth

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/pkg/auth"
)

type NoopAuthorizer struct{}

func (a *NoopAuthorizer) Check(ctx context.Context, principal auth.Principal, verb auth.Verb, resource auth.Resource) error {
	return nil
}

var _ auth.Authorizer = (*NoopAuthorizer)(nil)

type ReadOnlyAuthorizer struct{}

func (a *ReadOnlyAuthorizer) Check(ctx context.Context, principal auth.Principal, verb auth.Verb, resource auth.Resource) error {
	if verb != auth.VerbGet {
		return fmt.Errorf("read-only mode: %s operations are not permitted", verb)
	}
	return nil
}

var _ auth.Authorizer = (*ReadOnlyAuthorizer)(nil)
