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

// ScopeMatcher validates a collection scope and returns its object predicate.
func ScopeMatcher(scope auth.AuthorizationScope) (func(metav1.Object) bool, error) {
	switch scope.Kind {
	case auth.ScopeAll:
		if len(scope.AnyOf) != 0 {
			return nil, fmt.Errorf("%s scope must not contain clauses", scope.Kind)
		}
		return func(metav1.Object) bool { return true }, nil
	case auth.ScopeNone:
		if len(scope.AnyOf) != 0 {
			return nil, fmt.Errorf("%s scope must not contain clauses", scope.Kind)
		}
		return func(metav1.Object) bool { return false }, nil
	case auth.ScopeAnyOf:
		if len(scope.AnyOf) == 0 {
			return nil, fmt.Errorf("%s scope requires at least one clause", scope.Kind)
		}
	default:
		return nil, fmt.Errorf("unsupported scope kind %q", scope.Kind)
	}

	for clauseIndex, clause := range scope.AnyOf {
		if len(clause.All) == 0 {
			return nil, fmt.Errorf("scope clause %d requires at least one predicate", clauseIndex)
		}
		for predicateIndex, predicate := range clause.All {
			if predicate.Attribute != auth.AttributeNamespace && predicate.Attribute != auth.AttributeName {
				return nil, fmt.Errorf("unsupported scope attribute %q", predicate.Attribute)
			}
			switch predicate.Operator {
			case auth.ScopeIn:
				if len(predicate.Values) == 0 {
					return nil, fmt.Errorf("scope predicate %d.%d requires at least one value", clauseIndex, predicateIndex)
				}
				if slices.Contains(predicate.Values, "") {
					return nil, fmt.Errorf("scope predicate %d.%d contains an empty value", clauseIndex, predicateIndex)
				}
			case auth.ScopeMissing:
				if len(predicate.Values) != 0 {
					return nil, fmt.Errorf("scope predicate %d.%d must not contain values", clauseIndex, predicateIndex)
				}
			default:
				return nil, fmt.Errorf("unsupported scope operator %q", predicate.Operator)
			}
		}
	}

	return func(object metav1.Object) bool {
		for _, clause := range scope.AnyOf {
			matches := true
			for _, predicate := range clause.All {
				value := object.GetNamespace()
				if predicate.Attribute == auth.AttributeName {
					value = object.GetName()
				}
				switch predicate.Operator {
				case auth.ScopeIn:
					matches = value != "" && slices.Contains(predicate.Values, value)
				case auth.ScopeMissing:
					matches = value == ""
				}
				if !matches {
					break
				}
			}
			if matches {
				return true
			}
		}
		return false
	}, nil
}
