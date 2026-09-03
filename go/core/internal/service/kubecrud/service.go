package kubecrud

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/kagent-dev/kagent/go/core/internal/service/kubeauth"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Object interface {
	client.Object
	comparable
}

type Service[T Object, L client.ObjectList] struct {
	client     client.Client
	object     T
	list       L
	authorizer auth.CollectionAuthorizer
	resource   string
}

func NewService[T Object, L client.ObjectList](
	client client.Client,
	authorizer auth.CollectionAuthorizer,
	object T,
	list L,
	resource string,
) *Service[T, L] {
	return &Service[T, L]{
		client: client, object: object, list: list, authorizer: authorizer, resource: resource,
	}
}

func (s *Service[T, L]) List(ctx context.Context, namespace string) ([]T, error) {
	if namespace == "" {
		return nil, serviceerrors.NewInvalidArgument("namespace is required", nil)
	}
	scope, err := s.scope(ctx)
	if err != nil {
		return nil, err
	}
	matches, err := kubeauth.ScopeMatcher(scope)
	if err != nil {
		return nil, serviceerrors.NewPermissionDenied("Not authorized", err)
	}
	list := s.list.DeepCopyObject().(L)
	if err := s.client.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil, serviceerrors.NewInternal("Failed to list "+s.resource+"s", err)
	}
	items := make([]T, 0)
	if err := meta.EachListItem(list, func(item runtime.Object) error {
		object := item.(T)
		if matches(object) {
			items = append(items, object)
		}
		return nil
	}); err != nil {
		return nil, serviceerrors.NewInternal("Failed to read "+s.resource+" list", err)
	}
	slices.SortFunc(items, func(left, right T) int { return cmp.Compare(left.GetName(), right.GetName()) })
	return items, nil
}

func (s *Service[T, L]) Get(ctx context.Context, ref types.NamespacedName) (T, error) {
	var zero T
	if err := s.validateRef(ref); err != nil {
		return zero, err
	}
	object, err := s.get(ctx, ref)
	if err != nil {
		return zero, err
	}
	if err := s.authorize(ctx, auth.VerbGet, kubeauth.Resource(s.resource, object)); err != nil {
		return zero, err
	}
	return object, nil
}

// Create persists an object already prepared by the resource-specific service.
func (s *Service[T, L]) Create(ctx context.Context, object T) (T, error) {
	var zero T
	if object == zero {
		return zero, serviceerrors.NewInvalidArgument(s.resource+" resource is required", nil)
	}
	ref := types.NamespacedName{Namespace: object.GetNamespace(), Name: object.GetName()}
	if err := s.validateNewRef(ref); err != nil {
		return zero, err
	}
	if err := s.authorize(ctx, auth.VerbCreate, kubeauth.Resource(s.resource, object)); err != nil {
		return zero, err
	}
	if err := s.client.Create(ctx, object); err != nil {
		switch {
		case apierrors.IsAlreadyExists(err):
			return zero, serviceerrors.NewAlreadyExists(s.resource+" already exists", err)
		case apierrors.IsInvalid(err):
			return zero, serviceerrors.NewInvalidArgument("Invalid "+s.resource, err)
		default:
			return zero, serviceerrors.NewInternal("Failed to create "+s.resource, err)
		}
	}
	return object, nil
}

// Update loads the stored object, applies the resource-specific change, and persists it.
func (s *Service[T, L]) Update(ctx context.Context, ref types.NamespacedName, apply func(T)) (T, error) {
	var zero T
	if err := s.validateRef(ref); err != nil {
		return zero, err
	}
	object, err := s.get(ctx, ref)
	if err != nil {
		return zero, err
	}
	if err := s.authorize(ctx, auth.VerbUpdate, kubeauth.Resource(s.resource, object)); err != nil {
		return zero, err
	}
	apply(object)
	if err := s.authorize(ctx, auth.VerbUpdate, kubeauth.Resource(s.resource, object)); err != nil {
		return zero, err
	}
	if err := s.client.Update(ctx, object); err != nil {
		if apierrors.IsInvalid(err) {
			return zero, serviceerrors.NewInvalidArgument("Invalid "+s.resource, err)
		}
		return zero, serviceerrors.NewInternal("Failed to update "+s.resource, err)
	}
	return object, nil
}

func (s *Service[T, L]) Delete(ctx context.Context, ref types.NamespacedName) error {
	if err := s.validateRef(ref); err != nil {
		return err
	}
	object, err := s.get(ctx, ref)
	if err != nil {
		return err
	}
	if err := s.authorize(ctx, auth.VerbDelete, kubeauth.Resource(s.resource, object)); err != nil {
		return err
	}
	if err := s.client.Delete(ctx, object); err != nil {
		return serviceerrors.NewInternal("Failed to delete "+s.resource, err)
	}
	return nil
}

func (s *Service[T, L]) scope(ctx context.Context) (auth.AuthorizationScope, error) {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok || session == nil {
		return auth.AuthorizationScope{}, serviceerrors.NewUnauthenticated("Failed to get authenticated principal", fmt.Errorf("no session found"))
	}
	scope, err := s.authorizer.Scope(ctx, session.Principal(), auth.VerbList, s.resource)
	if err != nil {
		return auth.AuthorizationScope{}, serviceerrors.NewPermissionDenied("Not authorized", err)
	}
	return scope, nil
}

func (s *Service[T, L]) get(ctx context.Context, ref types.NamespacedName) (T, error) {
	var zero T
	object := s.object.DeepCopyObject().(T)
	if err := s.client.Get(ctx, ref, object); err != nil {
		if apierrors.IsNotFound(err) {
			return zero, serviceerrors.NewNotFound(s.resource+" not found", err)
		}
		return zero, serviceerrors.NewInternal("Failed to get "+s.resource, err)
	}
	return object, nil
}

func (s *Service[T, L]) authorize(ctx context.Context, verb auth.Verb, resource auth.Resource) error {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok || session == nil {
		return serviceerrors.NewUnauthenticated("Failed to get authenticated principal", fmt.Errorf("no session found"))
	}
	if err := s.authorizer.Check(ctx, session.Principal(), verb, resource); err != nil {
		return serviceerrors.NewPermissionDenied("Not authorized", err)
	}
	return nil
}

func (s *Service[T, L]) validateRef(ref types.NamespacedName) error {
	if ref.Namespace == "" || ref.Name == "" {
		return serviceerrors.NewInvalidArgument(s.resource+" namespace and name are required", nil)
	}
	return nil
}

func (s *Service[T, L]) validateNewRef(ref types.NamespacedName) error {
	if err := s.validateRef(ref); err != nil {
		return err
	}
	if len(utilvalidation.IsDNS1123Subdomain(ref.Namespace)) > 0 {
		return serviceerrors.NewInvalidArgument("namespace must be a valid DNS subdomain", nil)
	}
	if len(utilvalidation.IsDNS1123Subdomain(ref.Name)) > 0 {
		return serviceerrors.NewInvalidArgument("name must be a valid DNS subdomain", nil)
	}
	return nil
}
