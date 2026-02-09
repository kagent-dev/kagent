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
	v1alpha2 "github.com/kagent-dev/kagent/go/api/v1alpha2"
)

// ProviderConfig represents a configured LLM provider instance.
// Multiple ProviderConfigs can exist for the same provider type (e.g., two OpenAI instances).
type ProviderConfig struct {
	// Name is the unique identifier for this provider instance
	Name string `yaml:"name" json:"name"`

	// Type is the provider type (OpenAI, Anthropic, etc.)
	Type v1alpha2.ModelProvider `yaml:"type" json:"type"`

	// Endpoint is the base URL for the provider API
	Endpoint string `yaml:"endpoint" json:"endpoint"`

	// SecretRef references the Kubernetes Secret containing the API key
	SecretRef SecretReference `yaml:"secretRef" json:"secretRef"`
}

// SecretReference points to a specific key within a Kubernetes Secret
type SecretReference struct {
	// Name is the name of the Secret
	Name string `yaml:"name" json:"name"`

	// Key is the key within the Secret data
	Key string `yaml:"key" json:"key"`
}

// ProviderResponse is the API response format for listing providers
type ProviderResponse struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Endpoint string `json:"endpoint"`
}

// ModelsResponse is the API response format for listing models
type ModelsResponse struct {
	Provider string   `json:"provider"`
	Models   []string `json:"models"`
}
