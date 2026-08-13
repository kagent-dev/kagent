package grpcserver

import (
	"context"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	prompttemplateservice "github.com/kagent-dev/kagent/go/core/internal/service/prompttemplate"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"k8s.io/apimachinery/pkg/types"
)

type promptTemplateServer struct {
	apiv1alpha1.UnimplementedPromptTemplateServiceServer
	service *prompttemplateservice.Service
}

func newPromptTemplateServer(service *prompttemplateservice.Service) *promptTemplateServer {
	return &promptTemplateServer{service: service}
}

func (s *promptTemplateServer) ListPromptTemplates(ctx context.Context, request *apiv1alpha1.ListPromptTemplatesRequest) (*apiv1alpha1.ListPromptTemplatesResponse, error) {
	result, err := s.service.List(ctx, request.GetNamespace())
	if err != nil {
		return nil, err
	}

	promptTemplates := make([]*apiv1alpha1.PromptTemplateSummary, 0, len(result))
	for _, summary := range result {
		promptTemplates = append(promptTemplates, &apiv1alpha1.PromptTemplateSummary{
			Ref: &apiv1alpha1.ResourceReference{
				Namespace: summary.Namespace,
				Name:      summary.Name,
			},
			KeyCount: int32(summary.KeyCount),
			Keys:     summary.Keys,
		})
	}
	return &apiv1alpha1.ListPromptTemplatesResponse{PromptTemplates: promptTemplates}, nil
}

func (s *promptTemplateServer) GetPromptTemplate(ctx context.Context, request *apiv1alpha1.GetPromptTemplateRequest) (*apiv1alpha1.GetPromptTemplateResponse, error) {
	ref, err := requiredPromptTemplateRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	result, err := s.service.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.GetPromptTemplateResponse{PromptTemplate: promptTemplate(result)}, nil
}

func (s *promptTemplateServer) CreatePromptTemplate(ctx context.Context, request *apiv1alpha1.CreatePromptTemplateRequest) (*apiv1alpha1.CreatePromptTemplateResponse, error) {
	ref := request.GetRef()
	result, err := s.service.Create(ctx, prompttemplateservice.CreateRequest{
		Namespace: ref.GetNamespace(),
		Name:      ref.GetName(),
		Data:      request.GetData(),
	})
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.CreatePromptTemplateResponse{PromptTemplate: promptTemplate(result)}, nil
}

func (s *promptTemplateServer) UpdatePromptTemplate(ctx context.Context, request *apiv1alpha1.UpdatePromptTemplateRequest) (*apiv1alpha1.UpdatePromptTemplateResponse, error) {
	ref, err := requiredPromptTemplateRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	result, err := s.service.Update(ctx, ref, request.GetData())
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.UpdatePromptTemplateResponse{PromptTemplate: promptTemplate(result)}, nil
}

func (s *promptTemplateServer) DeletePromptTemplate(ctx context.Context, request *apiv1alpha1.DeletePromptTemplateRequest) (*apiv1alpha1.DeletePromptTemplateResponse, error) {
	ref, err := requiredPromptTemplateRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	if err := s.service.Delete(ctx, ref); err != nil {
		return nil, err
	}
	return &apiv1alpha1.DeletePromptTemplateResponse{}, nil
}

func requiredPromptTemplateRef(ref *apiv1alpha1.ResourceReference) (types.NamespacedName, error) {
	if ref == nil || ref.GetNamespace() == "" || ref.GetName() == "" {
		return types.NamespacedName{}, serviceerrors.NewInvalidArgument("PromptTemplate namespace and name are required", nil)
	}
	return types.NamespacedName{Namespace: ref.GetNamespace(), Name: ref.GetName()}, nil
}

func promptTemplate(detail prompttemplateservice.Detail) *apiv1alpha1.PromptTemplate {
	return &apiv1alpha1.PromptTemplate{
		Ref: &apiv1alpha1.ResourceReference{
			Namespace: detail.Namespace,
			Name:      detail.Name,
		},
		Data: detail.Data,
	}
}
