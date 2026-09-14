package grpcserver

import (
	"context"

	"github.com/kagent-dev/kagent/go/core/internal/service/kubeauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type resourceCapabilities struct {
	canCreate     bool
	updateMatcher kubeauth.Matcher
	deleteMatcher kubeauth.Matcher
}

type resourceScopeSource interface {
	Scope(context.Context, auth.Verb) (auth.AuthorizationScope, error)
}

func loadCollectionResourceCapabilities(
	ctx context.Context,
	source resourceScopeSource,
	withUpdate bool,
) (resourceCapabilities, error) {
	create, err := source.Scope(ctx, auth.VerbCreate)
	if err != nil {
		return resourceCapabilities{}, err
	}
	if _, err := kubeauth.CompileScope(create); err != nil {
		return resourceCapabilities{}, serviceerrors.NewPermissionDenied("Not authorized", err)
	}
	capabilities, err := loadResourceCapabilities(ctx, source, withUpdate)
	if err != nil {
		return resourceCapabilities{}, err
	}
	capabilities.canCreate = create.Kind != auth.ScopeNone
	return capabilities, nil
}

func loadResourceCapabilities(
	ctx context.Context,
	source resourceScopeSource,
	withUpdate bool,
) (resourceCapabilities, error) {
	capabilities := resourceCapabilities{}
	var err error
	if withUpdate {
		capabilities.updateMatcher, err = capabilityMatcher(ctx, source, auth.VerbUpdate)
		if err != nil {
			return resourceCapabilities{}, err
		}
	}
	capabilities.deleteMatcher, err = capabilityMatcher(ctx, source, auth.VerbDelete)
	return capabilities, err
}

func (c resourceCapabilities) canUpdate(object metav1.Object) bool {
	return c.updateMatcher.Matches(object)
}

func (c resourceCapabilities) canDelete(object metav1.Object) bool {
	return c.deleteMatcher.Matches(object)
}

func capabilityMatcher(
	ctx context.Context,
	source resourceScopeSource,
	verb auth.Verb,
) (kubeauth.Matcher, error) {
	result, err := source.Scope(ctx, verb)
	if err != nil {
		return kubeauth.Matcher{}, err
	}
	matcher, err := kubeauth.CompileScope(result)
	if err != nil {
		return kubeauth.Matcher{}, serviceerrors.NewPermissionDenied("Not authorized", err)
	}
	return matcher, nil
}
