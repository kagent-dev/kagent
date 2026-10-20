// Package inventoryauth matches collection scopes against trusted inventory identities.
package inventoryauth

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/kagent-dev/kagent/go/api/authorization"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// Matcher requires both location and metadata name, even for unrestricted scopes.
// A missing identity cannot safely participate in a later restrictive policy.
type Matcher struct {
	scope authorization.AuthorizationScope
}

func CompileScope(scope authorization.AuthorizationScope, location string) (Matcher, error) {
	if location != authorization.AttributeAtespace && location != authorization.AttributeNamespace {
		return Matcher{}, fmt.Errorf("unsupported inventory location %q", location)
	}
	switch scope.Kind {
	case authorization.ScopeAll, authorization.ScopeNone:
		if len(scope.AnyOf) != 0 {
			return Matcher{}, fmt.Errorf("%s scope must not contain clauses", scope.Kind)
		}
	case authorization.ScopeAnyOf:
		if len(scope.AnyOf) == 0 {
			return Matcher{}, fmt.Errorf("scope requires at least one clause")
		}
	default:
		return Matcher{}, fmt.Errorf("unsupported scope kind %q", scope.Kind)
	}
	scope.AnyOf = slices.Clone(scope.AnyOf)
	for i, clause := range scope.AnyOf {
		if len(clause.All) == 0 {
			return Matcher{}, fmt.Errorf("scope clause requires predicates")
		}
		scope.AnyOf[i].All = slices.Clone(clause.All)
		for j, predicate := range clause.All {
			if predicate.Attribute != location && predicate.Attribute != authorization.AttributeName {
				return Matcher{}, fmt.Errorf("unsupported inventory attribute %q", predicate.Attribute)
			}
			if predicate.Operator != authorization.ScopeIn {
				return Matcher{}, fmt.Errorf("unsupported scope operator %q", predicate.Operator)
			}
			if len(predicate.Values) == 0 || slices.ContainsFunc(predicate.Values, func(value string) bool { return strings.TrimSpace(value) == "" }) {
				return Matcher{}, fmt.Errorf("scope predicate requires nonempty values")
			}
			scope.AnyOf[i].All[j].Values = slices.Clone(predicate.Values)
		}
	}
	return Matcher{scope: scope}, nil
}

func (m Matcher) Matches(location, name string) bool {
	if strings.TrimSpace(location) == "" || strings.TrimSpace(name) == "" {
		return false
	}
	if m.scope.Kind == authorization.ScopeAll {
		return true
	}
	for _, clause := range m.scope.AnyOf {
		matches := true
		for _, predicate := range clause.All {
			value := location
			if predicate.Attribute == authorization.AttributeName {
				value = name
			}
			if !slices.Contains(predicate.Values, value) {
				matches = false
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

func (m Matcher) Empty() bool { return m.scope.Kind == "" || m.scope.Kind == authorization.ScopeNone }

// CollectionMatcher preserves scope failures rather than widening visibility.
func CollectionMatcher(ctx context.Context, authorizer auth.CollectionAuthorizer, resourceType, location string) (Matcher, error) {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok {
		return Matcher{}, serviceerrors.NewUnauthenticated("Authentication is required", nil)
	}
	if authorizer == nil {
		return Matcher{}, serviceerrors.NewInternal("Authorization is not configured", nil)
	}
	scope, err := authorizer.Scope(ctx, session.Principal(), auth.VerbList, resourceType)
	if err != nil {
		return Matcher{}, serviceerrors.NewUnavailable("Failed to read the "+resourceType+" authorization scope", err)
	}
	matcher, err := CompileScope(scope, location)
	if err != nil {
		return Matcher{}, serviceerrors.NewInternal("Failed to apply the "+resourceType+" authorization scope", err)
	}
	return matcher, nil
}
