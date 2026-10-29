// Package authscope validates and evaluates collection scopes over named identities.
package authscope

import (
	"fmt"
	"slices"

	"github.com/kagent-dev/kagent/go/api/authorization"
)

// Matcher owns a validated scope. Its zero value denies every identity.
type Matcher struct {
	scope authorization.AuthorizationScope
}

// CompileScope accepts name predicates and the supplied location attribute.
// Adapters determine which identity facts are required even under ALL.
func CompileScope(scope authorization.AuthorizationScope, location string) (Matcher, error) {
	if location != authorization.AttributeNamespace && location != authorization.AttributeAtespace {
		return Matcher{}, fmt.Errorf("unsupported scope location %q", location)
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
				return Matcher{}, fmt.Errorf("unsupported scope attribute %q", predicate.Attribute)
			}
			if predicate.Operator != authorization.ScopeIn {
				return Matcher{}, fmt.Errorf("unsupported scope operator %q", predicate.Operator)
			}
			if len(predicate.Values) == 0 || slices.Contains(predicate.Values, "") {
				return Matcher{}, fmt.Errorf("scope predicate requires nonempty values")
			}
			scope.AnyOf[i].All[j].Values = slices.Clone(predicate.Values)
		}
	}
	return Matcher{scope: scope}, nil
}

// Matches evaluates the trusted location and name supplied by a resource adapter.
func (m Matcher) Matches(location, name string) bool {
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
			if value == "" || !slices.Contains(predicate.Values, value) {
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

// Empty reports whether no identity can match.
func (m Matcher) Empty() bool { return m.scope.Kind == "" || m.scope.Kind == authorization.ScopeNone }
