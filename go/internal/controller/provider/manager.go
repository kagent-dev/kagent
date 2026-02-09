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
	"time"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// DefaultNamespace is the default namespace for kagent resources
	DefaultNamespace = "kagent"
)

// Manager handles provider configuration and model discovery.
// It reads provider configs from Provider CRDs and caches discovered models in CRD status.
type Manager struct {
	client    client.Client
	namespace string
}

// NewManager creates a new provider Manager instance.
func NewManager(client client.Client, namespace string) *Manager {
	if namespace == "" {
		namespace = DefaultNamespace
	}

	return &Manager{
		client:    client,
		namespace: namespace,
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
			providers = append(providers, ProviderConfig{
				Name:     p.Name,
				Type:     p.Spec.Type,
				Endpoint: p.Spec.Endpoint,
				SecretRef: SecretReference{
					Name: p.Spec.SecretRef.Name,
					Key:  p.Spec.SecretRef.Key,
				},
			})
		}
	}

	return providers
}

// GetModels returns models for a provider from the cached status or triggers discovery.
// Models are cached in Provider.Status.DiscoveredModels by the Provider controller.
// If forceRefresh is true, sets the force-discovery annotation to trigger controller reconciliation.
func (m *Manager) GetModels(ctx context.Context, providerName string, forceRefresh bool) ([]string, error) {
	logger := log.FromContext(ctx).WithName("provider-manager")

	provider := &v1alpha2.Provider{}
	if err := m.client.Get(ctx, types.NamespacedName{
		Name: providerName, Namespace: m.namespace,
	}, provider); err != nil {
		return nil, fmt.Errorf("provider %s not found: %w", providerName, err)
	}

	// Force refresh by setting annotation
	if forceRefresh {
		if provider.Annotations == nil {
			provider.Annotations = make(map[string]string)
		}
		provider.Annotations[v1alpha2.ProviderAnnotationForceDiscovery] = "true"
		if err := m.client.Update(ctx, provider); err != nil {
			return nil, fmt.Errorf("failed to trigger discovery: %w", err)
		}

		// Wait for controller to refresh (with timeout)
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(500 * time.Millisecond)
			if err := m.client.Get(ctx, client.ObjectKeyFromObject(provider), provider); err == nil {
				// Check if annotation cleared (refresh done)
				if provider.Annotations[v1alpha2.ProviderAnnotationForceDiscovery] != "true" {
					logger.Info("Model discovery completed", "provider", providerName)
					break
				}
			}
		}

		// Re-fetch provider to get updated status
		if err := m.client.Get(ctx, client.ObjectKeyFromObject(provider), provider); err != nil {
			return nil, fmt.Errorf("failed to get updated provider: %w", err)
		}
	}

	// Return cached models from status
	if len(provider.Status.DiscoveredModels) > 0 {
		return provider.Status.DiscoveredModels, nil
	}

	// No models discovered - provide helpful message
	if forceRefresh {
		return nil, fmt.Errorf("no models discovered for provider %s", providerName)
	}
	return nil, fmt.Errorf("no models discovered for provider %s, try refreshing", providerName)
}

// ClearCache is a no-op for CRD-based implementation.
// Models are cached in Provider.Status.DiscoveredModels and cleared by the controller.
func (m *Manager) ClearCache(providerName string) {
	// No-op - cache is managed by Provider controller in CRD status
}

// HasProviders returns true if any Ready providers are configured.
func (m *Manager) HasProviders() bool {
	providers := m.GetProviders()
	return len(providers) > 0
}
