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
	"testing"

	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestNewManager(t *testing.T) {
	tests := []struct {
		name          string
		namespace     string
		wantNamespace string
	}{
		{
			name:          "with default namespace",
			namespace:     "",
			wantNamespace: DefaultNamespace,
		},
		{
			name:          "with custom namespace",
			namespace:     "custom-ns",
			wantNamespace: "custom-ns",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1alpha2.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)
			client := fake.NewClientBuilder().WithScheme(scheme).Build()

			m := NewManager(client, tt.namespace)

			if m.namespace != tt.wantNamespace {
				t.Errorf("namespace = %v, want %v", m.namespace, tt.wantNamespace)
			}
			if m.client == nil {
				t.Error("client should be initialized")
			}
		})
	}
}

func TestGetProviders(t *testing.T) {
	tests := []struct {
		name      string
		providers []*v1alpha2.Provider
		wantCount int
		wantNames []string
	}{
		{
			name:      "no providers",
			providers: []*v1alpha2.Provider{},
			wantCount: 0,
			wantNames: []string{},
		},
		{
			name: "single ready provider",
			providers: []*v1alpha2.Provider{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "openai-prod",
						Namespace: DefaultNamespace,
					},
					Spec: v1alpha2.ProviderSpec{
						Type:     v1alpha2.ModelProviderOpenAI,
						Endpoint: "https://api.openai.com/v1",
						SecretRef: v1alpha2.SecretReference{
							Name: "openai-secret",
							Key:  "apiKey",
						},
					},
					Status: v1alpha2.ProviderStatus{
						Conditions: []metav1.Condition{
							{
								Type:   v1alpha2.ProviderConditionTypeReady,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			wantCount: 1,
			wantNames: []string{"openai-prod"},
		},
		{
			name: "mixed ready and not ready providers",
			providers: []*v1alpha2.Provider{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "openai-prod",
						Namespace: DefaultNamespace,
					},
					Spec: v1alpha2.ProviderSpec{
						Type:     v1alpha2.ModelProviderOpenAI,
						Endpoint: "https://api.openai.com/v1",
						SecretRef: v1alpha2.SecretReference{
							Name: "openai-secret",
							Key:  "apiKey",
						},
					},
					Status: v1alpha2.ProviderStatus{
						Conditions: []metav1.Condition{
							{
								Type:   v1alpha2.ProviderConditionTypeReady,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "anthropic-prod",
						Namespace: DefaultNamespace,
					},
					Spec: v1alpha2.ProviderSpec{
						Type:     v1alpha2.ModelProviderAnthropic,
						Endpoint: "https://api.anthropic.com",
						SecretRef: v1alpha2.SecretReference{
							Name: "anthropic-secret",
							Key:  "apiKey",
						},
					},
					Status: v1alpha2.ProviderStatus{
						Conditions: []metav1.Condition{
							{
								Type:   v1alpha2.ProviderConditionTypeReady,
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
			},
			wantCount: 1, // Only ready provider should be returned
			wantNames: []string{"openai-prod"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1alpha2.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			objs := make([]runtime.Object, len(tt.providers))
			for i, p := range tt.providers {
				objs[i] = p
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objs...).
				Build()

			m := NewManager(client, DefaultNamespace)
			providers := m.GetProviders()

			if len(providers) != tt.wantCount {
				t.Errorf("GetProviders() returned %d providers, want %d", len(providers), tt.wantCount)
			}

			for _, wantName := range tt.wantNames {
				found := false
				for _, p := range providers {
					if p.Name == wantName {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected provider %s not found in results", wantName)
				}
			}
		})
	}
}

func TestGetModels(t *testing.T) {
	tests := []struct {
		name         string
		provider     *v1alpha2.Provider
		forceRefresh bool
		wantModels   []string
		wantErr      bool
	}{
		{
			name: "provider with cached models",
			provider: &v1alpha2.Provider{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "openai-prod",
					Namespace: DefaultNamespace,
				},
				Spec: v1alpha2.ProviderSpec{
					Type:     v1alpha2.ModelProviderOpenAI,
					Endpoint: "https://api.openai.com/v1",
					SecretRef: v1alpha2.SecretReference{
						Name: "openai-secret",
						Key:  "apiKey",
					},
				},
				Status: v1alpha2.ProviderStatus{
					DiscoveredModels: []string{"gpt-4", "gpt-3.5-turbo"},
					Conditions: []metav1.Condition{
						{
							Type:   v1alpha2.ProviderConditionTypeReady,
							Status: metav1.ConditionTrue,
						},
					},
				},
			},
			forceRefresh: false,
			wantModels:   []string{"gpt-4", "gpt-3.5-turbo"},
			wantErr:      false,
		},
		{
			name: "provider not found",
			provider: &v1alpha2.Provider{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "different-provider",
					Namespace: DefaultNamespace,
				},
			},
			forceRefresh: false,
			wantModels:   nil,
			wantErr:      true,
		},
		{
			name: "provider without models",
			provider: &v1alpha2.Provider{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "empty-provider",
					Namespace: DefaultNamespace,
				},
				Spec: v1alpha2.ProviderSpec{
					Type:     v1alpha2.ModelProviderOpenAI,
					Endpoint: "https://api.openai.com/v1",
					SecretRef: v1alpha2.SecretReference{
						Name: "openai-secret",
						Key:  "apiKey",
					},
				},
				Status: v1alpha2.ProviderStatus{
					DiscoveredModels: []string{},
				},
			},
			forceRefresh: false,
			wantModels:   nil,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1alpha2.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(tt.provider).
				Build()

			m := NewManager(client, DefaultNamespace)

			// Use the provider name from the test case
			providerName := "openai-prod"
			switch tt.name {
			case "provider not found":
				providerName = "nonexistent-provider"
			case "provider without models":
				providerName = "empty-provider"
			}

			models, err := m.GetModels(context.Background(), providerName, tt.forceRefresh)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetModels() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && len(models) != len(tt.wantModels) {
				t.Errorf("GetModels() returned %d models, want %d", len(models), len(tt.wantModels))
			}
		})
	}
}

func TestHasProviders(t *testing.T) {
	tests := []struct {
		name      string
		providers []*v1alpha2.Provider
		want      bool
	}{
		{
			name:      "no providers",
			providers: []*v1alpha2.Provider{},
			want:      false,
		},
		{
			name: "has ready provider",
			providers: []*v1alpha2.Provider{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "openai-prod",
						Namespace: DefaultNamespace,
					},
					Spec: v1alpha2.ProviderSpec{
						Type:     v1alpha2.ModelProviderOpenAI,
						Endpoint: "https://api.openai.com/v1",
						SecretRef: v1alpha2.SecretReference{
							Name: "openai-secret",
							Key:  "apiKey",
						},
					},
					Status: v1alpha2.ProviderStatus{
						Conditions: []metav1.Condition{
							{
								Type:   v1alpha2.ProviderConditionTypeReady,
								Status: metav1.ConditionTrue,
							},
						},
					},
				},
			},
			want: true,
		},
		{
			name: "has only not ready provider",
			providers: []*v1alpha2.Provider{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "anthropic-prod",
						Namespace: DefaultNamespace,
					},
					Spec: v1alpha2.ProviderSpec{
						Type:     v1alpha2.ModelProviderAnthropic,
						Endpoint: "https://api.anthropic.com",
						SecretRef: v1alpha2.SecretReference{
							Name: "anthropic-secret",
							Key:  "apiKey",
						},
					},
					Status: v1alpha2.ProviderStatus{
						Conditions: []metav1.Condition{
							{
								Type:   v1alpha2.ProviderConditionTypeReady,
								Status: metav1.ConditionFalse,
							},
						},
					},
				},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = v1alpha2.AddToScheme(scheme)
			_ = corev1.AddToScheme(scheme)

			objs := make([]runtime.Object, len(tt.providers))
			for i, p := range tt.providers {
				objs[i] = p
			}

			client := fake.NewClientBuilder().
				WithScheme(scheme).
				WithRuntimeObjects(objs...).
				Build()

			m := NewManager(client, DefaultNamespace)

			if got := m.HasProviders(); got != tt.want {
				t.Errorf("HasProviders() = %v, want %v", got, tt.want)
			}
		})
	}
}
