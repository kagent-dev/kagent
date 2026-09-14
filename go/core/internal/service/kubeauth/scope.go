package kubeauth

import (
	"fmt"
	"slices"

	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Resource builds authorization input from trusted Kubernetes metadata.
func Resource(resourceType string, object metav1.Object) auth.Resource {
	attributes := make(map[string][]string, 2)
	if object.GetNamespace() != "" {
		attributes[auth.AttributeNamespace] = []string{object.GetNamespace()}
	}
	if object.GetName() != "" {
		attributes[auth.AttributeName] = []string{object.GetName()}
	}
	return auth.Resource{
		Type: resourceType,
		Name: types.NamespacedName{
			Namespace: object.GetNamespace(),
			Name:      object.GetName(),
		}.String(),
		Attributes: attributes,
	}
}

// Matcher is a validated authorization scope that can be applied to Kubernetes objects.
// Its zero value denies every object.
type Matcher struct {
	scope auth.AuthorizationScope
}

// CompileScope validates a collection scope and returns its object matcher.
func CompileScope(scope auth.AuthorizationScope) (Matcher, error) {
	switch scope.Kind {
	case auth.ScopeAll:
		if len(scope.AnyOf) != 0 {
			return Matcher{}, fmt.Errorf("%s scope must not contain clauses", scope.Kind)
		}
	case auth.ScopeNone:
		if len(scope.AnyOf) != 0 {
			return Matcher{}, fmt.Errorf("%s scope must not contain clauses", scope.Kind)
		}
	case auth.ScopeAnyOf:
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
			if predicate.Attribute != auth.AttributeNamespace && predicate.Attribute != auth.AttributeName {
				return Matcher{}, fmt.Errorf("unsupported scope attribute %q", predicate.Attribute)
			}
			if predicate.Operator != auth.ScopeIn {
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

	return Matcher{scope: scope}, nil
}

// Matches reports whether an object belongs to the compiled scope.
func (m Matcher) Matches(object metav1.Object) bool {
	if m.scope.Kind == auth.ScopeAll {
		return true
	}
	for _, clause := range m.scope.AnyOf {
		matches := true
		for _, predicate := range clause.All {
			value := object.GetNamespace()
			if predicate.Attribute == auth.AttributeName {
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
