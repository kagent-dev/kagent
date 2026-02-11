/*
Copyright 2025.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package provider

import (
	"context"
	"fmt"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/internal/utils"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// Manager handles provider configuration and model discovery.
// It reads provider configs from Provider CRDs and caches discovered models in CRD status.
type Manager struct {
	client     client.Client
	namespace  string
	discoverer *ModelDiscoverer
}

// NewManager creates a new provider Manager instance.
// If namespace is empty, it uses utils.GetResourceNamespace() which reads from
// KAGENT_NAMESPACE environment variable or defaults to "kagent".
func NewManager(client client.Client, namespace string) *Manager {
	if namespace == "" {
		namespace = utils.GetResourceNamespace()
	}
	return &Manager{
		client:     client,
		namespace:  namespace,
		discoverer: NewModelDiscoverer(),
	}
}

// GetProviders returns all configured providers that are Ready.
func (m *Manager) GetProviders() []ProviderConfig {
	ctx := context.Background()
	var providerList v1alpha2.ProviderList
	if err := m.client.List(ctx, &providerList,
		client.InNamespace(m.namespace)); err != nil {
		return nil
	}

	var providers []ProviderConfig
	for _, p := range providerList.Items {
		// Only include Ready providers
		if meta.IsStatusConditionTrue(p.Status.Conditions,
			v1alpha2.ProviderConditionTypeReady) {
			config := ProviderConfig{
				Name:     p.Name,
				Type:     p.Spec.Type,
				Endpoint: p.Spec.GetEndpoint(),
			}
			// Only set SecretRef if it's specified
			if p.Spec.SecretRef != nil {
				config.SecretRef = SecretReference{
					Name: p.Spec.SecretRef.Name,
					Key:  p.Spec.SecretRef.Key,
				}
			}
			providers = append(providers, config)
		}
	}

	return providers
}

// GetModels returns models for a provider from the cached status or performs direct discovery.
// Models are cached in Provider.Status.DiscoveredModels by the Provider controller.
// If forceRefresh is true, performs direct discovery and updates the provider status.
func (m *Manager) GetModels(ctx context.Context, providerName string, forceRefresh bool) ([]string, error) {
	logger := log.FromContext(ctx).WithName("provider-manager")

	provider := &v1alpha2.Provider{}
	if err := m.client.Get(ctx, types.NamespacedName{
		Name: providerName, Namespace: m.namespace,
	}, provider); err != nil {
		return nil, fmt.Errorf("provider %s not found: %w", providerName, err)
	}

	// If force refresh, perform direct discovery
	if forceRefresh {
		logger.Info("Performing direct model discovery", "provider", providerName)

		// Get API key from secret if required
		apiKey, err := m.getAPIKey(ctx, provider)
		if err != nil {
			return nil, fmt.Errorf("failed to get API key: %w", err)
		}

		// Discover models directly
		endpoint := provider.Spec.GetEndpoint()
		models, err := m.discoverer.DiscoverModels(ctx, provider.Spec.Type, endpoint, apiKey)
		if err != nil {
			return nil, fmt.Errorf("model discovery failed: %w", err)
		}

		logger.Info("Model discovery completed", "provider", providerName, "count", len(models))
		return models, nil
	}

	// Return cached models from status
	if len(provider.Status.DiscoveredModels) > 0 {
		return provider.Status.DiscoveredModels, nil
	}

	// No models discovered - provide helpful message
	return nil, fmt.Errorf("no models discovered for provider %s, try refreshing", providerName)
}

// getAPIKey retrieves the API key from the secret referenced by the provider.
// Returns empty string for providers that don't require authentication (e.g., Ollama).
func (m *Manager) getAPIKey(ctx context.Context, provider *v1alpha2.Provider) (string, error) {
	// Providers like Ollama don't require authentication
	if !provider.Spec.RequiresSecret() {
		return "", nil
	}

	if provider.Spec.SecretRef == nil {
		return "", fmt.Errorf("provider %s requires a secret but none is configured", provider.Name)
	}

	secret := &corev1.Secret{}
	secretName := types.NamespacedName{
		Namespace: provider.Namespace,
		Name:      provider.Spec.SecretRef.Name,
	}

	if err := m.client.Get(ctx, secretName, secret); err != nil {
		return "", fmt.Errorf("failed to get secret %s: %w", provider.Spec.SecretRef.Name, err)
	}

	apiKey, ok := secret.Data[provider.Spec.SecretRef.Key]
	if !ok || len(apiKey) == 0 {
		return "", fmt.Errorf("secret %s missing key %s", provider.Spec.SecretRef.Name, provider.Spec.SecretRef.Key)
	}

	return string(apiKey), nil
}
