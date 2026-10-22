package grpcserver

import (
	"context"

	"k8s.io/apimachinery/pkg/types"

	apiv1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1"
	prompttemplateservice "github.com/kagent-dev/kagent/go/core/internal/service/prompttemplate"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
)

type promptTemplateServer struct {
	apiv1.UnimplementedPromptTemplateServiceServer
	service *prompttemplateservice.Service
}

func newPromptTemplateServer(service *prompttemplateservice.Service) *promptTemplateServer {
	return &promptTemplateServer{service: service}
}

func (s *promptTemplateServer) ListPromptTemplates(ctx context.Context, request *apiv1.ListPromptTemplatesRequest) (*apiv1.ListPromptTemplatesResponse, error) {
	result, err := s.service.List(ctx, request.GetNamespace())
	if err != nil {
		return nil, err
	}

	promptTemplates := make([]*apiv1.PromptTemplateSummary, 0, len(result))
	for _, summary := range result {
		promptTemplates = append(promptTemplates, &apiv1.PromptTemplateSummary{
			Ref: &apiv1.ResourceReference{
				Namespace: summary.Namespace,
				Name:      summary.Name,
			},
			KeyCount: int32(summary.KeyCount),
			Keys:     summary.Keys,
		})
	}
	return &apiv1.ListPromptTemplatesResponse{PromptTemplates: promptTemplates}, nil
}

func (s *promptTemplateServer) GetPromptTemplate(ctx context.Context, request *apiv1.GetPromptTemplateRequest) (*apiv1.GetPromptTemplateResponse, error) {
	ref, err := requiredPromptTemplateRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	result, err := s.service.Get(ctx, ref)
	if err != nil {
		return nil, err
	}
	return &apiv1.GetPromptTemplateResponse{PromptTemplate: promptTemplate(result)}, nil
}

func (s *promptTemplateServer) CreatePromptTemplate(ctx context.Context, request *apiv1.CreatePromptTemplateRequest) (*apiv1.CreatePromptTemplateResponse, error) {
	ref := request.GetRef()
	result, err := s.service.Create(ctx, prompttemplateservice.CreateRequest{
		Namespace: ref.GetNamespace(),
		Name:      ref.GetName(),
		Data:      request.GetData(),
	})
	if err != nil {
		return nil, err
	}
	return &apiv1.CreatePromptTemplateResponse{PromptTemplate: promptTemplate(result)}, nil
}

func (s *promptTemplateServer) UpdatePromptTemplate(ctx context.Context, request *apiv1.UpdatePromptTemplateRequest) (*apiv1.UpdatePromptTemplateResponse, error) {
	ref, err := requiredPromptTemplateRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	result, err := s.service.Update(ctx, ref, request.GetData())
	if err != nil {
		return nil, err
	}
	return &apiv1.UpdatePromptTemplateResponse{PromptTemplate: promptTemplate(result)}, nil
}

func (s *promptTemplateServer) DeletePromptTemplate(ctx context.Context, request *apiv1.DeletePromptTemplateRequest) (*apiv1.DeletePromptTemplateResponse, error) {
	ref, err := requiredPromptTemplateRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	if err := s.service.Delete(ctx, ref); err != nil {
		return nil, err
	}
	return &apiv1.DeletePromptTemplateResponse{}, nil
}

func requiredPromptTemplateRef(ref *apiv1.ResourceReference) (types.NamespacedName, error) {
	if ref == nil || ref.GetNamespace() == "" || ref.GetName() == "" {
		return types.NamespacedName{}, serviceerrors.NewInvalidArgument("PromptTemplate namespace and name are required", nil)
	}
	return types.NamespacedName{Namespace: ref.GetNamespace(), Name: ref.GetName()}, nil
}

func promptTemplate(detail prompttemplateservice.Detail) *apiv1.PromptTemplate {
	return &apiv1.PromptTemplate{
		Ref: &apiv1.ResourceReference{
			Namespace: detail.Namespace,
			Name:      detail.Name,
		},
		Data: detail.Data,
	}
}
