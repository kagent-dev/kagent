// Package harness serves CRUD over the kagent.dev/v1alpha3 Harness CRD, the
// runtime and infrastructure half of the (Harness, AgentTemplate) pair that
// AgentInstanceService.CreateAgentInstance names.
//
// Harness is not AgentHarness. The agent service's GetAgentHarness /
// CreateAgentHarness / DeleteAgentHarness operate on the AgentHarness CRD — a
// single agent bound to an external ACP backend — and share nothing with this
// kind beyond a substring. A Harness is a reusable runtime that admits many
// AgentTemplates by label selector; go/core/v2/controller/collections.go pairs
// the two. Keeping them in separate packages is what stops the next reader
// from wiring one service into the other's RPCs.
package harness

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	utilvalidation "k8s.io/apimachinery/pkg/util/validation"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const resourceType = "Harness"

type Service struct {
	kubeClient client.Client
	authorizer auth.Authorizer
}

func NewService(kubeClient client.Client, authorizer auth.Authorizer) *Service {
	return &Service{kubeClient: kubeClient, authorizer: authorizer}
}

func (s *Service) List(ctx context.Context, namespace string) ([]v1alpha3.Harness, error) {
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: resourceType}); err != nil {
		return nil, err
	}
	if namespace == "" {
		return nil, serviceerrors.NewInvalidArgument("namespace is required", nil)
	}

	list := &v1alpha3.HarnessList{}
	if err := s.kubeClient.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil, serviceerrors.NewInternal("Failed to list Harnesses", err)
	}
	slices.SortFunc(list.Items, func(left, right v1alpha3.Harness) int {
		return cmp.Compare(left.Name, right.Name)
	})
	return list.Items, nil
}

func (s *Service) Get(ctx context.Context, ref types.NamespacedName) (*v1alpha3.Harness, error) {
	if err := validateRef(ref); err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: resourceType, Name: ref.String()}); err != nil {
		return nil, err
	}
	return s.get(ctx, ref)
}

func (s *Service) Create(ctx context.Context, incoming *v1alpha3.Harness) (*v1alpha3.Harness, error) {
	if incoming == nil {
		return nil, serviceerrors.NewInvalidArgument("Harness resource is required", nil)
	}
	ref := types.NamespacedName{Namespace: incoming.Namespace, Name: incoming.Name}
	if err := validateNewRef(ref); err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, auth.VerbCreate, auth.Resource{Type: resourceType, Name: ref.String()}); err != nil {
		return nil, err
	}

	// Status carries the controller's capability record, which is proven for a
	// pinned adapter and image rather than declared. A caller must not be able
	// to seed it by round-tripping a Get into a Create.
	created := incoming.DeepCopy()
	created.Status = v1alpha3.HarnessStatus{}
	if err := s.kubeClient.Create(ctx, created); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil, serviceerrors.NewAlreadyExists("A Harness with this name already exists in the namespace", err)
		}
		if apierrors.IsInvalid(err) {
			return nil, serviceerrors.NewInvalidArgument("Invalid Harness", err)
		}
		return nil, serviceerrors.NewInternal("Failed to create Harness", err)
	}
	return created, nil
}

func (s *Service) Update(ctx context.Context, ref types.NamespacedName, incoming *v1alpha3.Harness) (*v1alpha3.Harness, error) {
	if incoming == nil {
		return nil, serviceerrors.NewInvalidArgument("Harness resource is required", nil)
	}
	if err := validateRef(ref); err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, auth.VerbUpdate, auth.Resource{Type: resourceType, Name: ref.String()}); err != nil {
		return nil, err
	}

	// The spec is applied onto the stored object rather than the incoming one
	// being written wholesale: that keeps the caller's stale resourceVersion,
	// labels and controller-written status from silently overwriting the live
	// object, so an update is a spec change and nothing else.
	existing, err := s.get(ctx, ref)
	if err != nil {
		return nil, err
	}
	existing.Spec = *incoming.Spec.DeepCopy()
	if err := s.kubeClient.Update(ctx, existing); err != nil {
		if apierrors.IsInvalid(err) {
			return nil, serviceerrors.NewInvalidArgument("Invalid Harness", err)
		}
		return nil, serviceerrors.NewInternal("Failed to update Harness", err)
	}
	return existing, nil
}

func (s *Service) Delete(ctx context.Context, ref types.NamespacedName) error {
	if err := validateRef(ref); err != nil {
		return err
	}
	if err := s.authorize(ctx, auth.VerbDelete, auth.Resource{Type: resourceType, Name: ref.String()}); err != nil {
		return err
	}

	existing, err := s.get(ctx, ref)
	if err != nil {
		return err
	}
	if err := s.kubeClient.Delete(ctx, existing); err != nil {
		return serviceerrors.NewInternal("Failed to delete Harness", err)
	}
	return nil
}

func (s *Service) get(ctx context.Context, ref types.NamespacedName) (*v1alpha3.Harness, error) {
	result := &v1alpha3.Harness{}
	if err := s.kubeClient.Get(ctx, ref, result); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, serviceerrors.NewNotFound("Harness not found", err)
		}
		return nil, serviceerrors.NewInternal("Failed to get Harness", err)
	}
	return result, nil
}

func (s *Service) authorize(ctx context.Context, verb auth.Verb, resource auth.Resource) error {
	session, ok := auth.AuthSessionFrom(ctx)
	if !ok || session == nil {
		return serviceerrors.NewUnauthenticated("Failed to get authenticated principal", fmt.Errorf("no session found"))
	}
	if err := s.authorizer.Check(ctx, session.Principal(), verb, resource); err != nil {
		return serviceerrors.NewPermissionDenied("Not authorized", err)
	}
	return nil
}

func validateRef(ref types.NamespacedName) error {
	if ref.Namespace == "" || ref.Name == "" {
		return serviceerrors.NewInvalidArgument("Harness namespace and name are required", nil)
	}
	return nil
}

// validateNewRef additionally rejects names the apiserver would reject, so a
// create fails with an actionable message rather than a wrapped 422.
func validateNewRef(ref types.NamespacedName) error {
	if err := validateRef(ref); err != nil {
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
