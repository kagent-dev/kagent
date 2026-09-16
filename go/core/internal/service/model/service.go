package model

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/internal/service/kubecrud"
	"github.com/kagent-dev/kagent/go/core/internal/service/secretmaterial"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	common "github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

var modelConfigGVK = v1alpha3.GroupVersion.WithKind("ModelConfig")

// modelConfigResource names ModelConfig in authorization decisions.
const modelConfigResource = "ModelConfig"

type Service struct {
	kubeClient             client.Client
	modelConfigs           *kubecrud.Service[*v1alpha3.ModelConfig, *v1alpha3.ModelConfigList]
	defaultNamespace       string
	providerModelRefresher ProviderModelRefresher
}

type ListRequest struct{}

type GetRequest struct {
	Ref types.NamespacedName
}

type CreateRequest struct {
	Ref     string
	APIKey  string
	Spec    v1alpha3.ModelConfigSpec
	Secrets []secretmaterial.Material
}

type UpdateRequest struct {
	Ref     types.NamespacedName
	APIKey  *string
	Spec    v1alpha3.ModelConfigSpec
	Secrets []secretmaterial.Material
}

type DeleteRequest struct {
	Ref types.NamespacedName
}

func NewService(kubeClient client.Client, authorizer auth.CollectionAuthorizer, defaultNamespace string, options ...ServiceOption) *Service {
	service := &Service{
		kubeClient:       kubeClient,
		modelConfigs:     kubecrud.NewService(kubeClient, authorizer, &v1alpha3.ModelConfig{}, &v1alpha3.ModelConfigList{}, modelConfigResource),
		defaultNamespace: defaultNamespace,
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) List(ctx context.Context, _ ListRequest) (*v1alpha3.ModelConfigList, error) {
	items, err := s.modelConfigs.List(ctx, "")
	if err != nil {
		return nil, err
	}
	result := &v1alpha3.ModelConfigList{Items: make([]v1alpha3.ModelConfig, len(items))}
	for index, item := range items {
		result.Items[index] = *item
	}
	return result, nil
}

func (s *Service) Get(ctx context.Context, request GetRequest) (*v1alpha3.ModelConfig, error) {
	return s.modelConfigs.Get(ctx, request.Ref)
}

func (s *Service) Create(ctx context.Context, request CreateRequest) (*v1alpha3.ModelConfig, error) {
	ref, err := common.ParseRefString(request.Ref, s.defaultNamespace)
	if err != nil {
		return nil, serviceerrors.NewInvalidArgument("Invalid Ref", err)
	}

	if err := validateAPIKeySecretRef(request.Spec.APIKeySecret, request.Spec.APIKeySecretKey, request.Spec.Provider); err != nil {
		return nil, err
	}
	if err := secretmaterial.ValidateMaterials(request.Secrets); err != nil {
		return nil, err
	}

	spec := request.Spec
	if request.APIKey != "" && spec.APIKeySecret == "" && spec.Provider != v1alpha3.ModelProviderOllama {
		spec.APIKeySecret = ref.Name
		spec.APIKeySecretKey = providerAPIKeySecretKey(spec.Provider)
	}

	modelConfig := &v1alpha3.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ref.Name,
			Namespace: ref.Namespace,
		},
		Spec: spec,
	}
	modelConfig, err = s.modelConfigs.Create(ctx, modelConfig)
	if err != nil {
		return nil, err
	}

	if err := s.createOwnedSecrets(ctx, modelConfig, request); err != nil {
		if rollbackErr := secretmaterial.RollbackOwnerOnCreateFailure(ctx, s.kubeClient, modelConfig); rollbackErr != nil {
			return nil, serviceerrors.NewInternal(
				serviceerrors.MessageOf(err),
				fmt.Errorf("%w; rollback failed: %v", err, rollbackErr),
			)
		}
		return nil, err
	}

	return modelConfig, nil
}

// createOwnedSecrets writes every Secret a new ModelConfig owns, so one failure rolls back the same way.
func (s *Service) createOwnedSecrets(ctx context.Context, modelConfig *v1alpha3.ModelConfig, request CreateRequest) error {
	spec := modelConfig.Spec
	if request.APIKey != "" && spec.Provider != v1alpha3.ModelProviderOllama {
		if err := secretmaterial.CreateOwnedOpaqueSecret(
			ctx,
			s.kubeClient,
			modelConfig,
			modelConfigGVK,
			modelConfig.Name,
			map[string]string{spec.APIKeySecretKey: request.APIKey},
		); err != nil {
			return serviceerrors.NewInternal("Failed to create API key secret", err)
		}
	}
	return secretmaterial.CreateCompanionSecrets(ctx, s.kubeClient, modelConfig, modelConfigGVK, request.Secrets)
}

// Update keeps its own write because the owned Secrets must land between the
// authorized read and the retrying ModelConfig write.
func (s *Service) Update(ctx context.Context, request UpdateRequest) (*v1alpha3.ModelConfig, error) {
	modelConfig, err := s.modelConfigs.GetForUpdate(ctx, request.Ref)
	if err != nil {
		return nil, err
	}
	if err := validateAPIKeySecretRef(request.Spec.APIKeySecret, request.Spec.APIKeySecretKey, request.Spec.Provider); err != nil {
		return nil, err
	}
	if err := secretmaterial.ValidateMaterials(request.Secrets); err != nil {
		return nil, err
	}

	oldRefs := referencedSecretNames(modelConfig.Spec)
	spec := request.Spec
	if request.APIKey != nil && *request.APIKey != "" && spec.APIKeySecret == "" && spec.Provider != v1alpha3.ModelProviderOllama {
		spec.APIKeySecret = request.Ref.Name
		spec.APIKeySecretKey = providerAPIKeySecretKey(spec.Provider)
	}

	if request.APIKey != nil && *request.APIKey != "" && spec.Provider != v1alpha3.ModelProviderOllama {
		if err := secretmaterial.CreateOrUpdateOwnedOpaqueSecret(
			ctx,
			s.kubeClient,
			modelConfig,
			modelConfigGVK,
			modelConfig.Name,
			map[string]string{spec.APIKeySecretKey: *request.APIKey},
		); err != nil {
			return nil, serviceerrors.NewInternal("Failed to update API key secret", err)
		}
	}

	if err := secretmaterial.CreateCompanionSecrets(ctx, s.kubeClient, modelConfig, modelConfigGVK, request.Secrets); err != nil {
		return nil, err
	}

	if err := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &v1alpha3.ModelConfig{}
		if err := s.kubeClient.Get(ctx, request.Ref, latest); err != nil {
			return err
		}
		latest.Spec = spec
		if err := s.kubeClient.Update(ctx, latest); err != nil {
			return err
		}
		modelConfig = latest
		return nil
	}); err != nil {
		return nil, serviceerrors.NewInternal("Failed to update ModelConfig", err)
	}

	newRefs := referencedSecretNames(modelConfig.Spec)
	requestedNames := map[string]struct{}{}
	for _, material := range request.Secrets {
		requestedNames[material.Name] = struct{}{}
	}
	for name := range oldRefs {
		if _, kept := newRefs[name]; kept {
			continue
		}
		if _, kept := requestedNames[name]; kept {
			continue
		}
		if err := secretmaterial.DeleteOwnedSecret(ctx, s.kubeClient, modelConfig, modelConfigGVK, name); err != nil {
			return nil, serviceerrors.NewInternal("Failed to update ModelConfig", err)
		}
	}

	return modelConfig, nil
}

func (s *Service) Delete(ctx context.Context, request DeleteRequest) error {
	return s.modelConfigs.Delete(ctx, request.Ref)
}

func validateAPIKeySecretRef(apiKeySecret, apiKeySecretKey string, provider v1alpha3.ModelProvider) error {
	if apiKeySecret != "" && apiKeySecretKey == "" &&
		provider != v1alpha3.ModelProviderBedrock &&
		provider != v1alpha3.ModelProviderSAPAICore {
		return serviceerrors.NewInvalidArgument("apiKeySecretKey is required when apiKeySecret is set", nil)
	}
	return nil
}

func providerAPIKeySecretKey(provider v1alpha3.ModelProvider) string {
	return fmt.Sprintf("%s_API_KEY", strings.ToUpper(string(provider)))
}

func referencedSecretNames(spec v1alpha3.ModelConfigSpec) map[string]struct{} {
	references := map[string]struct{}{}
	if spec.APIKeySecret != "" {
		references[spec.APIKeySecret] = struct{}{}
	}
	if spec.TLS != nil && spec.TLS.CACertSecretRef != "" {
		references[spec.TLS.CACertSecretRef] = struct{}{}
	}
	return references
}
