package kubeauth

import (
	"fmt"
	"slices"

	apiauthorization "github.com/kagent-dev/kagent/go/api/authorization"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
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

// MatchesAnyName reports whether the scope contains any valid Kubernetes
// resource name in namespace.
func (m Matcher) MatchesAnyName(namespace string) bool {
	if len(utilvalidation.IsDNS1123Label(namespace)) != 0 {
		return false
	}
	if m.scope.Kind == apiauthorization.ScopeAll {
		return true
	}
	for _, clause := range m.scope.AnyOf {
		names := []string{"x"}
		for _, predicate := range clause.All {
			if predicate.Attribute == apiauthorization.AttributeName {
				names = predicate.Values
				break
			}
		}
		for _, name := range names {
			if len(utilvalidation.IsDNS1123Subdomain(name)) != 0 {
				continue
			}
			object := &metav1.PartialObjectMetadata{ObjectMeta: metav1.ObjectMeta{Namespace: namespace, Name: name}}
			if m.Matches(object) {
				return true
			}
		}
	}
	return false
}
