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

// ReadOnlyAuthorizer allows only read (get) operations and rejects all
// mutating requests. This is useful for GitOps deployments where resources
// are managed declaratively and the UI should be view-only.
type ReadOnlyAuthorizer struct{}

func (a *ReadOnlyAuthorizer) Check(ctx context.Context, principal auth.Principal, verb auth.Verb, resource auth.Resource) error {
	if verb == auth.VerbGet {
		return nil
	}
	return fmt.Errorf("forbidden: read-only mode is enabled, %s operations on %s are not allowed", verb, resource.Type)
}

var _ auth.Authorizer = (*ReadOnlyAuthorizer)(nil)
