// Package agenttemplate serves CRUD over the kagent.dev/v1alpha3 AgentTemplate
// CRD, the portable-behavior half of the (Harness, AgentTemplate) pair that
// AgentInstanceService.CreateAgentInstance names.
package agenttemplate

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

// resourceType is the authorizer's name for this kind. It is deliberately not
// "Agent": an AgentTemplate is authored and shared like a template, and the
// AgentInstances created from it are what carry per-agent authorization.
const resourceType = "AgentTemplate"

type Service struct {
	kubeClient client.Client
	authorizer auth.Authorizer
}

func NewService(kubeClient client.Client, authorizer auth.Authorizer) *Service {
	return &Service{kubeClient: kubeClient, authorizer: authorizer}
}

func (s *Service) List(ctx context.Context, namespace string) ([]v1alpha3.AgentTemplate, error) {
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: resourceType}); err != nil {
		return nil, err
	}
	if namespace == "" {
		return nil, serviceerrors.NewInvalidArgument("namespace is required", nil)
	}

	list := &v1alpha3.AgentTemplateList{}
	if err := s.kubeClient.List(ctx, list, client.InNamespace(namespace)); err != nil {
		return nil, serviceerrors.NewInternal("Failed to list AgentTemplates", err)
	}
	slices.SortFunc(list.Items, func(left, right v1alpha3.AgentTemplate) int {
		return cmp.Compare(left.Name, right.Name)
	})
	return list.Items, nil
}

func (s *Service) Get(ctx context.Context, ref types.NamespacedName) (*v1alpha3.AgentTemplate, error) {
	if err := validateRef(ref); err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: resourceType, Name: ref.String()}); err != nil {
		return nil, err
	}
	return s.get(ctx, ref)
}

func (s *Service) Create(ctx context.Context, template *v1alpha3.AgentTemplate) (*v1alpha3.AgentTemplate, error) {
	if template == nil {
		return nil, serviceerrors.NewInvalidArgument("AgentTemplate resource is required", nil)
	}
	ref := types.NamespacedName{Namespace: template.Namespace, Name: template.Name}
	if err := validateNewRef(ref); err != nil {
		return nil, err
	}
	if err := s.authorize(ctx, auth.VerbCreate, auth.Resource{Type: resourceType, Name: ref.String()}); err != nil {
		return nil, err
	}

	// Status is controller-owned, so a caller that round-trips a Get into a
	// Create cannot assert readiness it has not earned.
	created := template.DeepCopy()
	created.Status = v1alpha3.AgentTemplateStatus{}
	if err := s.kubeClient.Create(ctx, created); err != nil {
		if apierrors.IsAlreadyExists(err) {
			return nil, serviceerrors.NewAlreadyExists("An AgentTemplate with this name already exists in the namespace", err)
		}
		if apierrors.IsInvalid(err) {
			return nil, serviceerrors.NewInvalidArgument("Invalid AgentTemplate", err)
		}
		return nil, serviceerrors.NewInternal("Failed to create AgentTemplate", err)
	}
	return created, nil
}

func (s *Service) Update(ctx context.Context, ref types.NamespacedName, template *v1alpha3.AgentTemplate) (*v1alpha3.AgentTemplate, error) {
	if template == nil {
		return nil, serviceerrors.NewInvalidArgument("AgentTemplate resource is required", nil)
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
	existing.Spec = *template.Spec.DeepCopy()
	if err := s.kubeClient.Update(ctx, existing); err != nil {
		if apierrors.IsInvalid(err) {
			return nil, serviceerrors.NewInvalidArgument("Invalid AgentTemplate", err)
		}
		return nil, serviceerrors.NewInternal("Failed to update AgentTemplate", err)
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
		return serviceerrors.NewInternal("Failed to delete AgentTemplate", err)
	}
	return nil
}

func (s *Service) get(ctx context.Context, ref types.NamespacedName) (*v1alpha3.AgentTemplate, error) {
	template := &v1alpha3.AgentTemplate{}
	if err := s.kubeClient.Get(ctx, ref, template); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, serviceerrors.NewNotFound("AgentTemplate not found", err)
		}
		return nil, serviceerrors.NewInternal("Failed to get AgentTemplate", err)
	}
	return template, nil
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
		return serviceerrors.NewInvalidArgument("AgentTemplate namespace and name are required", nil)
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
