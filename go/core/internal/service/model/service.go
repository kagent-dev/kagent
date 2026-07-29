package model

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/internal/service/secretmaterial"
	"github.com/kagent-dev/kagent/go/core/internal/service/serviceerrors"
	common "github.com/kagent-dev/kagent/go/core/internal/utils"
	"github.com/kagent-dev/kagent/go/core/pkg/auth"
)

var modelConfigGVK = v1alpha2.GroupVersion.WithKind("ModelConfig")

type Service struct {
	kubeClient             client.Client
	authorizer             auth.Authorizer
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
	Spec    v1alpha2.ModelConfigSpec
	Secrets []secretmaterial.Material
}

type UpdateRequest struct {
	Ref     types.NamespacedName
	APIKey  *string
	Spec    v1alpha2.ModelConfigSpec
	Secrets []secretmaterial.Material
}

type DeleteRequest struct {
	Ref types.NamespacedName
}

func NewService(kubeClient client.Client, authorizer auth.Authorizer, defaultNamespace string, options ...ServiceOption) *Service {
	service := &Service{
		kubeClient:       kubeClient,
		authorizer:       authorizer,
		defaultNamespace: defaultNamespace,
	}
	for _, option := range options {
		option(service)
	}
	return service
}

func (s *Service) List(ctx context.Context, _ ListRequest) (*v1alpha2.ModelConfigList, error) {
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: "ModelConfig"}); err != nil {
		return nil, err
	}

	modelConfigs := &v1alpha2.ModelConfigList{}
	if err := s.kubeClient.List(ctx, modelConfigs); err != nil {
		return nil, serviceerrors.NewInternal("Failed to list ModelConfigs from Kubernetes", err)
	}
	return modelConfigs, nil
}

func (s *Service) Get(ctx context.Context, request GetRequest) (*v1alpha2.ModelConfig, error) {
	if err := s.authorize(ctx, auth.VerbGet, auth.Resource{Type: "ModelConfig", Name: request.Ref.String()}); err != nil {
		return nil, err
	}

	modelConfig := &v1alpha2.ModelConfig{}
	if err := s.kubeClient.Get(ctx, request.Ref, modelConfig); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, serviceerrors.NewNotFound("ModelConfig not found", err)
		}
		return nil, serviceerrors.NewInternal("Failed to get ModelConfig", err)
	}
	return modelConfig, nil
}

func (s *Service) Create(ctx context.Context, request CreateRequest) (*v1alpha2.ModelConfig, error) {
	ref, err := common.ParseRefString(request.Ref, s.defaultNamespace)
	if err != nil {
		return nil, serviceerrors.NewInvalidArgument("Invalid Ref", err)
	}

	if err := s.authorize(ctx, auth.VerbCreate, auth.Resource{Type: "ModelConfig", Name: ref.String()}); err != nil {
		return nil, err
	}

	if err := validateAPIKeySecretRef(request.Spec.APIKeySecret, request.Spec.APIKeySecretKey, request.Spec.Provider); err != nil {
		return nil, err
	}
	if err := secretmaterial.ValidateMaterials(request.Secrets); err != nil {
		return nil, err
	}

	existingConfig := &v1alpha2.ModelConfig{}
	if err := s.kubeClient.Get(ctx, ref, existingConfig); err == nil {
		return nil, serviceerrors.NewAlreadyExists("ModelConfig already exists", nil)
	} else if !apierrors.IsNotFound(err) {
		return nil, serviceerrors.NewInternal("Failed to check if ModelConfig exists", err)
	}

	spec := request.Spec
	if request.APIKey != "" && spec.APIKeySecret == "" && spec.Provider != v1alpha2.ModelProviderOllama {
		spec.APIKeySecret = ref.Name
		spec.APIKeySecretKey = providerAPIKeySecretKey(spec.Provider)
	}

	modelConfig := &v1alpha2.ModelConfig{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ref.Name,
			Namespace: ref.Namespace,
		},
		Spec: spec,
	}

	if err := s.kubeClient.Create(ctx, modelConfig); err != nil {
		return nil, serviceerrors.NewInternal("Failed to create ModelConfig", err)
	}

	if request.APIKey != "" && spec.Provider != v1alpha2.ModelProviderOllama {
		if err := secretmaterial.CreateOwnedOpaqueSecret(
			ctx,
			s.kubeClient,
			modelConfig,
			modelConfigGVK,
			modelConfig.Name,
			map[string]string{spec.APIKeySecretKey: request.APIKey},
		); err != nil {
			return nil, serviceerrors.NewInternal("Failed to create ModelConfig", err)
		}
	}

	if err := secretmaterial.CreateCompanionSecrets(ctx, s.kubeClient, modelConfig, modelConfigGVK, request.Secrets); err != nil {
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

func (s *Service) Update(ctx context.Context, request UpdateRequest) (*v1alpha2.ModelConfig, error) {
	if err := s.authorize(ctx, auth.VerbUpdate, auth.Resource{Type: "ModelConfig", Name: request.Ref.String()}); err != nil {
		return nil, err
	}

	if err := validateAPIKeySecretRef(request.Spec.APIKeySecret, request.Spec.APIKeySecretKey, request.Spec.Provider); err != nil {
		return nil, err
	}
	if err := secretmaterial.ValidateMaterials(request.Secrets); err != nil {
		return nil, err
	}

	modelConfig := &v1alpha2.ModelConfig{}
	if err := s.kubeClient.Get(ctx, request.Ref, modelConfig); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, serviceerrors.NewNotFound("ModelConfig not found", err)
		}
		return nil, serviceerrors.NewInternal("Failed to get ModelConfig", err)
	}

	oldRefs := referencedSecretNames(modelConfig.Spec)
	spec := request.Spec
	if request.APIKey != nil && *request.APIKey != "" && spec.APIKeySecret == "" && spec.Provider != v1alpha2.ModelProviderOllama {
		spec.APIKeySecret = request.Ref.Name
		spec.APIKeySecretKey = providerAPIKeySecretKey(spec.Provider)
	}

	if request.APIKey != nil && *request.APIKey != "" && spec.Provider != v1alpha2.ModelProviderOllama {
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

	modelConfig.Spec = spec
	if err := s.kubeClient.Update(ctx, modelConfig); err != nil {
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

func (s *Service) Delete(ctx context.Context, request DeleteRequest) (*v1alpha2.ModelConfig, error) {
	if err := s.authorize(ctx, auth.VerbDelete, auth.Resource{Type: "ModelConfig", Name: request.Ref.String()}); err != nil {
		return nil, err
	}

	modelConfig := &v1alpha2.ModelConfig{}
	if err := s.kubeClient.Get(ctx, request.Ref, modelConfig); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, serviceerrors.NewNotFound("ModelConfig not found", err)
		}
		return nil, serviceerrors.NewInternal("Failed to get ModelConfig", err)
	}

	if err := s.kubeClient.Delete(ctx, modelConfig); err != nil {
		return nil, serviceerrors.NewInternal("Failed to delete ModelConfig", err)
	}
	return modelConfig, nil
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

func validateAPIKeySecretRef(apiKeySecret, apiKeySecretKey string, provider v1alpha2.ModelProvider) error {
	if apiKeySecret != "" && apiKeySecretKey == "" &&
		provider != v1alpha2.ModelProviderBedrock &&
		provider != v1alpha2.ModelProviderSAPAICore {
		return serviceerrors.NewInvalidArgument("apiKeySecretKey is required when apiKeySecret is set", nil)
	}
	return nil
}

func providerAPIKeySecretKey(provider v1alpha2.ModelProvider) string {
	return fmt.Sprintf("%s_API_KEY", strings.ToUpper(string(provider)))
}

func referencedSecretNames(spec v1alpha2.ModelConfigSpec) map[string]struct{} {
	references := map[string]struct{}{}
	if spec.APIKeySecret != "" {
		references[spec.APIKeySecret] = struct{}{}
	}
	if spec.TLS != nil && spec.TLS.CACertSecretRef != "" {
		references[spec.TLS.CACertSecretRef] = struct{}{}
	}
	return references
}
