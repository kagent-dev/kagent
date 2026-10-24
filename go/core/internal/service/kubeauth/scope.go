package kubeauth

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	apiauthorization "github.com/kagent-dev/kagent/go/api/authorization"
	"github.com/kagent-dev/kagent/go/core/internal/service/authscope"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

// Matcher is a validated authorization scope that can be applied to Kubernetes objects.
// Its zero value denies every object.
type Matcher struct {
	scope authscope.Matcher
}

// CompileScope validates a collection scope and returns its object matcher.
func CompileScope(scope apiauthorization.AuthorizationScope) (Matcher, error) {
	matcher, err := authscope.CompileScope(scope, apiauthorization.AttributeNamespace)
	return Matcher{scope: matcher}, err
}

// Matches reports whether an object belongs to the compiled scope.
func (m Matcher) Matches(object metav1.Object) bool {
	if object == nil {
		return m.scope.Matches("", "")
	}
	return m.scope.Matches(object.GetNamespace(), object.GetName())
}

// CollectionMatcher resolves and validates a caller's Kubernetes object scope.
// Custom collection services use the same authentication and failure handling so
// a policy lookup failure cannot silently become an unrestricted collection.
func CollectionMatcher(ctx context.Context, authorizer auth.CollectionAuthorizer, resourceType string) (Matcher, error) {
	scope, err := authscope.CollectionScope(ctx, authorizer, resourceType)
	if err != nil {
		return Matcher{}, err
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
	return m.scope.Empty()
}
