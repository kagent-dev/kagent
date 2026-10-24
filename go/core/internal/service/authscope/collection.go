package authscope

import (
	"context"

	"github.com/kagent-dev/kagent/go/api/authorization"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// CollectionScope resolves the authenticated caller's list scope. Adapters must
// validate it before matching resources; lookup failures never grant access.
func CollectionScope(ctx context.Context, authorizer auth.CollectionAuthorizer, resourceType string) (authorization.AuthorizationScope, error) {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok {
		return authorization.AuthorizationScope{}, serviceerrors.NewUnauthenticated("Authentication is required", nil)
	}
	if authorizer == nil {
		return authorization.AuthorizationScope{}, serviceerrors.NewInternal("Authorization is not configured", nil)
	}
	scope, err := authorizer.Scope(ctx, session.Principal(), auth.VerbList, resourceType)
	if err != nil {
		return authorization.AuthorizationScope{}, serviceerrors.NewUnavailable("Failed to read the "+resourceType+" authorization scope", err)
	}
	return scope, nil
}
