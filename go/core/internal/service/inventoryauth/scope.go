// Package inventoryauth matches collection scopes against trusted inventory identities.
package inventoryauth

import (
	"context"
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/authorization"
	"github.com/kagent-dev/kagent/go/core/internal/service/authscope"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// Matcher requires both location and metadata name, even for unrestricted scopes.
// A missing identity cannot safely participate in a later restrictive policy.
type Matcher struct {
	scope authscope.Matcher
}

func CompileScope(scope authorization.AuthorizationScope, location string) (Matcher, error) {
	matcher, err := authscope.CompileScope(scope, location)
	if err != nil {
		return Matcher{}, err
	}
	for _, clause := range scope.AnyOf {
		for _, predicate := range clause.All {
			for _, value := range predicate.Values {
				if strings.TrimSpace(value) == "" {
					return Matcher{}, fmt.Errorf("inventory scope predicate requires nonblank values")
				}
			}
		}
	}
	return Matcher{scope: matcher}, nil
}

func (m Matcher) Matches(location, name string) bool {
	if strings.TrimSpace(location) == "" || strings.TrimSpace(name) == "" {
		return false
	}
	return m.scope.Matches(location, name)
}

func (m Matcher) Empty() bool { return m.scope.Empty() }

// CollectionMatcher preserves scope failures rather than widening visibility.
func CollectionMatcher(ctx context.Context, authorizer auth.CollectionAuthorizer, resourceType, location string) (Matcher, error) {
	scope, err := authscope.CollectionScope(ctx, authorizer, resourceType)
	if err != nil {
		return Matcher{}, err
	}
	matcher, err := CompileScope(scope, location)
	if err != nil {
		return Matcher{}, serviceerrors.NewInternal("Failed to apply the "+resourceType+" authorization scope", err)
	}
	return matcher, nil
}
