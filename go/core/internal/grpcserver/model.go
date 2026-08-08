package grpcserver

import (
	"context"
	"fmt"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	modelservice "github.com/kagent-dev/kagent/go/core/internal/service/model"
	"github.com/kagent-dev/kagent/go/core/internal/service/secretmaterial"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	"k8s.io/apimachinery/pkg/types"
)

const modelConfigKind = "ModelConfig"

type modelServer struct {
	apiv1alpha1.UnimplementedModelServiceServer
	service         *modelservice.Service
	maxMessageBytes int
}

func newModelServer(service *modelservice.Service, maxMessageBytes int) *modelServer {
	return &modelServer{service: service, maxMessageBytes: maxMessageBytes}
}

func (s *modelServer) ListModelConfigs(ctx context.Context, _ *apiv1alpha1.ListModelConfigsRequest) (*apiv1alpha1.ListModelConfigsResponse, error) {
	result, err := s.service.List(ctx, modelservice.ListRequest{})
	if err != nil {
		return nil, err
	}

	modelConfigs := make([]*apiv1alpha1.ModelConfig, 0, len(result.Items))
	for index := range result.Items {
		modelConfig, err := s.modelConfig(&result.Items[index])
		if err != nil {
			return nil, err
		}
		modelConfigs = append(modelConfigs, modelConfig)
	}
	return &apiv1alpha1.ListModelConfigsResponse{ModelConfigs: modelConfigs}, nil
}

func (s *modelServer) GetModelConfig(ctx context.Context, request *apiv1alpha1.GetModelConfigRequest) (*apiv1alpha1.GetModelConfigResponse, error) {
	ref, err := requiredNamespacedRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	result, err := s.service.Get(ctx, modelservice.GetRequest{Ref: ref})
	if err != nil {
		return nil, err
	}
	modelConfig, err := s.modelConfig(result)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.GetModelConfigResponse{ModelConfig: modelConfig}, nil
}

func (s *modelServer) CreateModelConfig(ctx context.Context, request *apiv1alpha1.CreateModelConfigRequest) (*apiv1alpha1.CreateModelConfigResponse, error) {
	ref, err := createRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	spec, err := s.modelConfigSpec(request.GetResource())
	if err != nil {
		return nil, err
	}
	result, err := s.service.Create(ctx, modelservice.CreateRequest{
		Ref:     ref,
		APIKey:  request.GetApiKey(),
		Spec:    spec,
		Secrets: secretMaterials(request.GetSecrets()),
	})
	if err != nil {
		return nil, err
	}
	modelConfig, err := s.modelConfig(result)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.CreateModelConfigResponse{ModelConfig: modelConfig}, nil
}

func (s *modelServer) UpdateModelConfig(ctx context.Context, request *apiv1alpha1.UpdateModelConfigRequest) (*apiv1alpha1.UpdateModelConfigResponse, error) {
	ref, err := requiredNamespacedRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	spec, err := s.modelConfigSpec(request.GetResource())
	if err != nil {
		return nil, err
	}
	result, err := s.service.Update(ctx, modelservice.UpdateRequest{
		Ref:     ref,
		APIKey:  request.ApiKey,
		Spec:    spec,
		Secrets: secretMaterials(request.GetSecrets()),
	})
	if err != nil {
		return nil, err
	}
	modelConfig, err := s.modelConfig(result)
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.UpdateModelConfigResponse{ModelConfig: modelConfig}, nil
}

func (s *modelServer) DeleteModelConfig(ctx context.Context, request *apiv1alpha1.DeleteModelConfigRequest) (*apiv1alpha1.DeleteModelConfigResponse, error) {
	ref, err := requiredNamespacedRef(request.GetRef())
	if err != nil {
		return nil, err
	}
	if _, err := s.service.Delete(ctx, modelservice.DeleteRequest{Ref: ref}); err != nil {
		return nil, err
	}
	return &apiv1alpha1.DeleteModelConfigResponse{}, nil
}

func (s *modelServer) ListSupportedModelProviders(ctx context.Context, _ *apiv1alpha1.ListSupportedModelProvidersRequest) (*apiv1alpha1.ListSupportedModelProvidersResponse, error) {
	return &apiv1alpha1.ListSupportedModelProvidersResponse{
		Providers: providerDefinitions(s.service.ListSupportedModelProviders(ctx)),
	}, nil
}

func (s *modelServer) ListSupportedMemoryProviders(ctx context.Context, _ *apiv1alpha1.ListSupportedMemoryProvidersRequest) (*apiv1alpha1.ListSupportedMemoryProvidersResponse, error) {
	return &apiv1alpha1.ListSupportedMemoryProvidersResponse{
		Providers: providerDefinitions(s.service.ListSupportedMemoryProviders(ctx)),
	}, nil
}

func (s *modelServer) ListConfiguredProviders(ctx context.Context, _ *apiv1alpha1.ListConfiguredProvidersRequest) (*apiv1alpha1.ListConfiguredProvidersResponse, error) {
	result, err := s.service.ListConfiguredProviders(ctx)
	if err != nil {
		return nil, err
	}
	providers := make([]*apiv1alpha1.ConfiguredProvider, 0, len(result))
	for _, provider := range result {
		providers = append(providers, &apiv1alpha1.ConfiguredProvider{
			Name:     provider.Name,
			Type:     provider.Type,
			Endpoint: provider.Endpoint,
		})
	}
	return &apiv1alpha1.ListConfiguredProvidersResponse{Providers: providers}, nil
}

func (s *modelServer) ListProviderModels(ctx context.Context, request *apiv1alpha1.ListProviderModelsRequest) (*apiv1alpha1.ListProviderModelsResponse, error) {
	result, err := s.service.GetProviderModels(ctx, modelservice.GetProviderModelsRequest{
		Name:    request.GetProviderName(),
		Refresh: request.GetRefresh(),
	})
	if err != nil {
		return nil, err
	}
	return &apiv1alpha1.ListProviderModelsResponse{
		Provider: result.Provider,
		Models:   result.Models,
	}, nil
}

func (s *modelServer) ListSupportedModels(ctx context.Context, _ *apiv1alpha1.ListSupportedModelsRequest) (*apiv1alpha1.ListSupportedModelsResponse, error) {
	catalog := s.service.ListSupportedModels(ctx)
	definitions := s.service.ListSupportedModelProviders(ctx)
	providers := make([]*apiv1alpha1.ProviderModels, 0, len(definitions))
	for _, definition := range definitions {
		models := catalog[v1alpha2.ModelProvider(definition.Name)]
		providerModels := &apiv1alpha1.ProviderModels{
			Provider: definition.Name,
			Models:   make([]*apiv1alpha1.ModelInfo, 0, len(models)),
		}
		for _, model := range models {
			providerModels.Models = append(providerModels.Models, &apiv1alpha1.ModelInfo{
				Name:            model.Name,
				FunctionCalling: model.FunctionCalling,
			})
		}
		providers = append(providers, providerModels)
	}
	return &apiv1alpha1.ListSupportedModelsResponse{Providers: providers}, nil
}

func (s *modelServer) modelConfig(modelConfig *v1alpha2.ModelConfig) (*apiv1alpha1.ModelConfig, error) {
	resource, err := structuredobject.FromGo(
		modelConfig,
		v1alpha2.GroupVersion.String(),
		modelConfigKind,
		s.maxMessageBytes,
	)
	if err != nil {
		return nil, serviceerrors.NewInternal("Failed to encode ModelConfig", err)
	}
	return &apiv1alpha1.ModelConfig{
		Ref: &apiv1alpha1.ResourceReference{
			Namespace: modelConfig.Namespace,
			Name:      modelConfig.Name,
		},
		Resource: resource,
	}, nil
}

func (s *modelServer) modelConfigSpec(resource *apiv1alpha1.StructuredObject) (v1alpha2.ModelConfigSpec, error) {
	modelConfig := &v1alpha2.ModelConfig{}
	if err := structuredobject.ToGo(resource, modelConfigKind, modelConfig, s.maxMessageBytes); err != nil {
		return v1alpha2.ModelConfigSpec{}, serviceerrors.NewInvalidArgument("Invalid ModelConfig resource", err)
	}
	return modelConfig.Spec, nil
}

func requiredNamespacedRef(ref *apiv1alpha1.ResourceReference) (types.NamespacedName, error) {
	if ref == nil || ref.GetNamespace() == "" || ref.GetName() == "" {
		return types.NamespacedName{}, serviceerrors.NewInvalidArgument("ModelConfig namespace and name are required", nil)
	}
	return types.NamespacedName{Namespace: ref.GetNamespace(), Name: ref.GetName()}, nil
}

func createRef(ref *apiv1alpha1.ResourceReference) (string, error) {
	if ref == nil || ref.GetName() == "" {
		return "", serviceerrors.NewInvalidArgument("ModelConfig name is required", nil)
	}
	if ref.GetNamespace() == "" {
		return ref.GetName(), nil
	}
	return fmt.Sprintf("%s/%s", ref.GetNamespace(), ref.GetName()), nil
}

func secretMaterials(materials []*apiv1alpha1.SecretMaterial) []secretmaterial.Material {
	result := make([]secretmaterial.Material, 0, len(materials))
	for _, material := range materials {
		result = append(result, secretmaterial.Material{
			Name:  material.GetName(),
			Key:   material.GetKey(),
			Value: material.GetValue(),
		})
	}
	return result
}

func providerDefinitions(definitions []modelservice.ProviderDefinition) []*apiv1alpha1.ProviderDefinition {
	result := make([]*apiv1alpha1.ProviderDefinition, 0, len(definitions))
	for _, definition := range definitions {
		result = append(result, &apiv1alpha1.ProviderDefinition{
			Name:           definition.Name,
			Type:           definition.Type,
			RequiredParams: definition.RequiredParams,
			OptionalParams: definition.OptionalParams,
		})
	}
	return result
}
