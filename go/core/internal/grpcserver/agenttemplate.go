package grpcserver

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"k8s.io/apimachinery/pkg/types"
)

const agentTemplateKind = "AgentTemplate"

type agentTemplateServer struct {
	apiv1alpha1.UnimplementedAgentTemplateServiceServer
	service         *kubecrud.Service[*v1alpha3.AgentTemplate, *v1alpha3.AgentTemplateList]
	maxMessageBytes int
}

func newAgentTemplateServer(service *kubecrud.Service[*v1alpha3.AgentTemplate, *v1alpha3.AgentTemplateList], maxMessageBytes int) *agentTemplateServer {
	return &agentTemplateServer{service: service, maxMessageBytes: maxMessageBytes}
}

func (s *agentTemplateServer) ListAgentTemplates(ctx context.Context, request *apiv1alpha1.ListAgentTemplatesRequest) (*apiv1alpha1.ListAgentTemplatesResponse, error) {
	items, err := s.service.List(ctx, request.GetNamespace())
	if err != nil {
		return nil, err
	}
	templates := make([]*apiv1alpha1.AgentTemplate, 0, len(items))
	for _, item := range items {
		template, err := s.agentTemplate(item)
		if err != nil {
			return nil, err
		}
		templates = append(templates, template)
	}
	return &apiv1alpha1.ListAgentTemplatesResponse{AgentTemplates: templates}, nil
}

func (s *agentTemplateServer) GetAgentTemplate(ctx context.Context, request *apiv1alpha1.GetAgentTemplateRequest) (*apiv1alpha1.GetAgentTemplateResponse, error) {
	ref, err := requiredAgentTemplateRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	result, err := s.service.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	template, err := s.agentTemplate(result)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.GetAgentTemplateResponse{AgentTemplate: template}, nil
}

func (s *agentTemplateServer) CreateAgentTemplate(ctx context.Context, request *apiv1alpha1.CreateAgentTemplateRequest) (*apiv1alpha1.CreateAgentTemplateResponse, error) {
	incoming := &v1alpha3.AgentTemplate{}
	if err := s.decodeResource(request.GetRef(), request.GetResource(), incoming); err != nil {
		return nil, err
	}
	incoming.Status = v1alpha3.AgentTemplateStatus{}
	result, err := s.service.Create(ctx, incoming)
	if err != nil {
		return nil, err
	}
	template, err := s.agentTemplate(result)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.CreateAgentTemplateResponse{AgentTemplate: template}, nil
}

func (s *agentTemplateServer) UpdateAgentTemplate(ctx context.Context, request *apiv1alpha1.UpdateAgentTemplateRequest) (*apiv1alpha1.UpdateAgentTemplateResponse, error) {
	ref, err := requiredAgentTemplateRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	incoming := &v1alpha3.AgentTemplate{}
	if err := s.decodeResource(request.GetRef(), request.GetResource(), incoming); err != nil {
		return nil, err
	}
	existing, err := s.service.GetForUpdate(ctx, ref)
	if err != nil {
		return nil, err
	}
	existing.Spec = *incoming.Spec.DeepCopy()
	result, err := s.service.SaveUpdate(ctx, existing)
	if err != nil {
		return nil, err
	}
	template, err := s.agentTemplate(result)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.UpdateAgentTemplateResponse{AgentTemplate: template}, nil
}

func (s *agentTemplateServer) DeleteAgentTemplate(ctx context.Context, request *apiv1alpha1.DeleteAgentTemplateRequest) (*apiv1alpha1.DeleteAgentTemplateResponse, error) {
	ref, err := requiredAgentTemplateRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	if err := s.service.Delete(ctx, ref); err != nil {
		return nil, err
	}
	return &apiv1alpha1.DeleteAgentTemplateResponse{}, nil
}

func (s *agentTemplateServer) agentTemplate(template *v1alpha3.AgentTemplate) (*apiv1alpha1.AgentTemplate, error) {
	resource, err := structuredobject.FromGo(template, v1alpha3.GroupVersion.String(), agentTemplateKind, s.maxMessageBytes)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to encode AgentTemplate resource", err)
	}
	admitting := make([]string, 0, len(template.Status.Harnesses))
	for _, status := range template.Status.Harnesses {
		admitting = append(admitting, status.Harness)
	}
	return &apiv1alpha1.AgentTemplate{
		Ref: &apiv1alpha1.ResourceReference{Namespace: template.Namespace, Name: template.Name},
		// The model config lives in the template's own namespace: the CRD's
		// reference is name-only and same-namespace by construction.
		ModelConfigRef:     &apiv1alpha1.ResourceReference{Namespace: template.Namespace, Name: template.Spec.ModelConfig.Name},
		Resource:           resource,
		Description:        template.Spec.Description,
		AdmittingHarnesses: admitting,
	}, nil
}

// decodeResource reads the CR out of the request and forces its metadata to
// agree with the ref, so a payload naming a different object cannot be used to
// write outside the namespace the caller was authorized against.
func (s *agentTemplateServer) decodeResource(ref *apiv1alpha1.ResourceReference, resource *apiv1alpha1.StructuredObject, destination *v1alpha3.AgentTemplate) error {
	if ref == nil || ref.GetNamespace() == "" || ref.GetName() == "" {
		return serviceerrors.NewInvalidArgument("AgentTemplate namespace and name are required", nil)
	}
	if err := structuredobject.ToGo(resource, agentTemplateKind, destination, s.maxMessageBytes); err != nil {
		return serviceerrors.NewInvalidArgument("Invalid AgentTemplate resource", err)
	}
	if destination.GetName() != "" && destination.GetName() != ref.GetName() {
		return serviceerrors.NewInvalidArgument("AgentTemplate reference does not match resource metadata", nil)
	}
	if destination.GetNamespace() != "" && destination.GetNamespace() != ref.GetNamespace() {
		return serviceerrors.NewInvalidArgument("AgentTemplate reference does not match resource metadata", nil)
	}
	destination.SetName(ref.GetName())
	destination.SetNamespace(ref.GetNamespace())
	return nil
}

func requiredAgentTemplateRef(ref *apiv1alpha1.ResourceReference) (types.NamespacedName, error) {
	if ref == nil || ref.GetNamespace() == "" || ref.GetName() == "" {
		return types.NamespacedName{}, serviceerrors.NewInvalidArgument("AgentTemplate namespace and name are required", nil)
	}
	return types.NamespacedName{Namespace: ref.GetNamespace(), Name: ref.GetName()}, nil
}
