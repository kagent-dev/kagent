package auth

import (
	"context"

	"github.com/kagent-dev/kagent/go/pkg/auth"
)

type NoopAuthorizer struct{}

func (a *NoopAuthorizer) Check(ctx context.Context, req auth.AuthzRequest) (*auth.AuthzDecision, error) {
	return &auth.AuthzDecision{Allowed: true}, nil
}

var _ auth.Authorizer = (*NoopAuthorizer)(nil)
