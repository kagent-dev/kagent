package agenttemplate

import (
	"context"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type Service struct {
	*kubecrud.Service[*v1alpha3.AgentTemplate, *v1alpha3.AgentTemplateList]
}

func NewService(client client.Client, authorizer auth.Authorizer) *Service {
	return &Service{Service: kubecrud.NewService(
		client, authorizer, &v1alpha3.AgentTemplate{}, &v1alpha3.AgentTemplateList{}, "AgentTemplate",
	)}
}

func (s *Service) Create(ctx context.Context, incoming *v1alpha3.AgentTemplate) (*v1alpha3.AgentTemplate, error) {
	if incoming == nil {
		return nil, serviceerrors.NewInvalidArgument("AgentTemplate resource is required", nil)
	}
	created := incoming.DeepCopy()
	created.Status = v1alpha3.AgentTemplateStatus{}
	return s.Service.Create(ctx, created)
}

func (s *Service) Update(ctx context.Context, ref types.NamespacedName, incoming *v1alpha3.AgentTemplate) (*v1alpha3.AgentTemplate, error) {
	if incoming == nil {
		return nil, serviceerrors.NewInvalidArgument("AgentTemplate resource is required", nil)
	}
	existing, err := s.GetForUpdate(ctx, ref)
	if err != nil {
		return nil, err
	}
	existing.Spec = *incoming.Spec.DeepCopy()
	return s.SaveUpdate(ctx, existing)
}
