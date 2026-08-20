package client

import (
	"fmt"
	"strings"

	apiv1alpha1 "github.com/kagent-dev/kagent/go/api/gen/kagent/api/v1alpha1"
	api "github.com/kagent-dev/kagent/go/api/httpapi"
	"github.com/kagent-dev/kagent/go/api/structuredobject"
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const modelConfigKind = "ModelConfig"

func (c *BaseClient) modelServiceClient() (apiv1alpha1.ModelServiceClient, error) {
	connection, err := c.grpcConnection()
	if err != nil {
		return nil, err
	}
	return apiv1alpha1.NewModelServiceClient(connection), nil
}

func (c *BaseClient) encodeModelConfig(spec v1alpha3.ModelConfigSpec) (*apiv1alpha1.StructuredObject, error) {
	resource, err := structuredobject.FromGo(
		&v1alpha3.ModelConfig{Spec: spec},
		v1alpha3.GroupVersion.String(),
		modelConfigKind,
		c.grpc.maxMessageBytes,
	)
	if err != nil {
		return nil, fmt.Errorf("encode ModelConfig resource: %w", err)
	}
	return resource, nil
}

func (c *BaseClient) decodeModelConfig(modelConfig *apiv1alpha1.ModelConfig) (*api.ModelConfigResource, error) {
	if modelConfig == nil {
		return nil, fmt.Errorf("ModelService response did not include a ModelConfig")
	}
	ref := modelConfig.GetRef()
	if ref == nil || ref.GetNamespace() == "" || ref.GetName() == "" {
		return nil, fmt.Errorf("ModelService response did not include a complete ModelConfig reference")
	}

	resource := &v1alpha3.ModelConfig{}
	if err := structuredobject.ToGo(modelConfig.GetResource(), modelConfigKind, resource, c.grpc.maxMessageBytes); err != nil {
		return nil, fmt.Errorf("decode ModelConfig resource: %w", err)
	}
	return &api.ModelConfigResource{
		Ref:    ref.GetNamespace() + "/" + ref.GetName(),
		Spec:   resource.Spec,
		Status: resource.Status,
	}, nil
}

func createModelConfigRef(ref string) (*apiv1alpha1.ResourceReference, error) {
	if ref == "" {
		return nil, status.Error(codes.InvalidArgument, "ModelConfig reference is required")
	}
	if !strings.Contains(ref, "/") {
		return &apiv1alpha1.ResourceReference{Name: ref}, nil
	}
	if strings.Count(ref, "/") != 1 {
		return nil, status.Error(codes.InvalidArgument, "ModelConfig reference must use namespace/name or name format")
	}
	namespace, name, _ := strings.Cut(ref, "/")
	if namespace == "" || name == "" {
		return nil, status.Error(codes.InvalidArgument, "ModelConfig reference must include both namespace and name")
	}
	return &apiv1alpha1.ResourceReference{Namespace: namespace, Name: name}, nil
}

func namespacedModelConfigRef(namespace, name string) *apiv1alpha1.ResourceReference {
	return &apiv1alpha1.ResourceReference{Namespace: namespace, Name: name}
}

func modelConfigSecrets(secrets []api.SecretMaterial) []*apiv1alpha1.SecretMaterial {
	result := make([]*apiv1alpha1.SecretMaterial, 0, len(secrets))
	for _, secret := range secrets {
		result = append(result, &apiv1alpha1.SecretMaterial{
			Name:  secret.Name,
			Key:   secret.Key,
			Value: secret.Value,
		})
	}
	return result
}

func providerInfo(provider *apiv1alpha1.ProviderDefinition) api.ProviderInfo {
	return api.ProviderInfo{
		Name:           provider.GetName(),
		Type:           provider.GetType(),
		RequiredParams: append([]string{}, provider.GetRequiredParams()...),
		OptionalParams: append([]string{}, provider.GetOptionalParams()...),
	}
}
