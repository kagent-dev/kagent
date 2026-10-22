package kubeauth

import (
	"context"
	"fmt"
	"slices"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiauthorization "github.com/kagent-dev/kagent/go/api/authorization"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// Matcher is a validated authorization scope that can be applied to Kubernetes objects.
// Its zero value denies every object.
type Matcher struct {
	scope apiauthorization.AuthorizationScope
}

// CompileScope validates a collection scope and returns its object matcher.
func CompileScope(scope apiauthorization.AuthorizationScope) (Matcher, error) {
	switch scope.Kind {
	case apiauthorization.ScopeAll:
		if len(scope.AnyOf) != 0 {
			return Matcher{}, fmt.Errorf("%s scope must not contain clauses", scope.Kind)
		}
	case apiauthorization.ScopeNone:
		if len(scope.AnyOf) != 0 {
			return Matcher{}, fmt.Errorf("%s scope must not contain clauses", scope.Kind)
		}
	case apiauthorization.ScopeAnyOf:
		if len(scope.AnyOf) == 0 {
			return Matcher{}, fmt.Errorf("%s scope requires at least one clause", scope.Kind)
		}
	default:
		return Matcher{}, fmt.Errorf("unsupported scope kind %q", scope.Kind)
	}

	for clauseIndex, clause := range scope.AnyOf {
		if len(clause.All) == 0 {
			return Matcher{}, fmt.Errorf("scope clause %d requires at least one predicate", clauseIndex)
		}
		for predicateIndex, predicate := range clause.All {
			if predicate.Attribute != apiauthorization.AttributeNamespace && predicate.Attribute != apiauthorization.AttributeName {
				return Matcher{}, fmt.Errorf("unsupported scope attribute %q", predicate.Attribute)
			}
			if predicate.Operator != apiauthorization.ScopeIn {
				return Matcher{}, fmt.Errorf("unsupported scope operator %q", predicate.Operator)
			}
			if len(predicate.Values) == 0 {
				return Matcher{}, fmt.Errorf("scope predicate %d.%d requires at least one value", clauseIndex, predicateIndex)
			}
			if slices.Contains(predicate.Values, "") {
				return Matcher{}, fmt.Errorf("scope predicate %d.%d contains an empty value", clauseIndex, predicateIndex)
			}
		}
	}
	scope.AnyOf = slices.Clone(scope.AnyOf)
	for clauseIndex := range scope.AnyOf {
		scope.AnyOf[clauseIndex].All = slices.Clone(scope.AnyOf[clauseIndex].All)
		for predicateIndex := range scope.AnyOf[clauseIndex].All {
			predicate := &scope.AnyOf[clauseIndex].All[predicateIndex]
			predicate.Values = slices.Clone(predicate.Values)
		}
	}

	return Matcher{scope: scope}, nil
}

// Matches reports whether an object belongs to the compiled scope.
func (m Matcher) Matches(object metav1.Object) bool {
	if m.scope.Kind == apiauthorization.ScopeAll {
		return true
	}
	for _, clause := range m.scope.AnyOf {
		matches := true
		for _, predicate := range clause.All {
			value := object.GetNamespace()
			if predicate.Attribute == apiauthorization.AttributeName {
				value = object.GetName()
			}
			matches = value != "" && slices.Contains(predicate.Values, value)
			if !matches {
				break
			}
		}
		if matches {
			return true
		}
	}
	return false
}

// CollectionMatcher resolves and validates a caller's Kubernetes object scope.
// Custom collection services use the same authentication and failure handling so
// a policy lookup failure cannot silently become an unrestricted collection.
func CollectionMatcher(ctx context.Context, authorizer auth.CollectionAuthorizer, resourceType string) (Matcher, error) {
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
	matcher, err := CompileScope(scope)
	if err != nil {
		return Matcher{}, serviceerrors.NewInternal("Failed to apply the "+resourceType+" authorization scope", err)
	}
	return matcher, nil
}

// Empty reports whether no object can match. Services can avoid reading a denied
// collection, so its storage availability does not gate an independent section.
func (m Matcher) Empty() bool {
	return m.scope.Kind == "" || m.scope.Kind == apiauthorization.ScopeNone
}
