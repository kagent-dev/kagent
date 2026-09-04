package grpcserver

import (
	"context"

	"github.com/kagent-dev/kagent/go/core/internal/service/kubeauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type resourceCapabilities struct {
	canCreate bool
	canUpdate func(metav1.Object) bool
	canDelete func(metav1.Object) bool
}

func loadResourceCapabilities(
	ctx context.Context,
	scope func(context.Context, auth.Verb) (auth.AuthorizationScope, error),
	withUpdate bool,
) (resourceCapabilities, error) {
	create, err := scope(ctx, auth.VerbCreate)
	if err != nil {
		return resourceCapabilities{}, err
	}
	if _, err := kubeauth.ScopeMatcher(create); err != nil {
		return resourceCapabilities{}, serviceerrors.NewPermissionDenied("Not authorized", err)
	}
	capabilities := resourceCapabilities{canCreate: create.Kind != auth.ScopeNone}
	if withUpdate {
		capabilities.canUpdate, err = capabilityMatcher(ctx, scope, auth.VerbUpdate)
		if err != nil {
			return resourceCapabilities{}, err
		}
	}
	capabilities.canDelete, err = capabilityMatcher(ctx, scope, auth.VerbDelete)
	return capabilities, err
}

func capabilityMatcher(
	ctx context.Context,
	scope func(context.Context, auth.Verb) (auth.AuthorizationScope, error),
	verb auth.Verb,
) (func(metav1.Object) bool, error) {
	result, err := scope(ctx, verb)
	if err != nil {
		return nil, err
	}
	matches, err := kubeauth.ScopeMatcher(result)
	if err != nil {
		return nil, serviceerrors.NewPermissionDenied("Not authorized", err)
	}
	return matches, nil
}
