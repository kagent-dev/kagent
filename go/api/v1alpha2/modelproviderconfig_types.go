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

package v1alpha2

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	// ModelProviderConfigConditionTypeReady indicates whether the model provider config is ready for use
	ModelProviderConfigConditionTypeReady = "Ready"

	// ModelProviderConfigConditionTypeSecretResolved indicates whether the model provider config's secret reference is valid
	ModelProviderConfigConditionTypeSecretResolved = "SecretResolved"

	// ModelProviderConfigConditionTypeModelsDiscovered indicates whether model discovery has succeeded
	ModelProviderConfigConditionTypeModelsDiscovered = "ModelsDiscovered"
)

// DefaultModelProviderEndpoint returns the default API endpoint for a given model provider type.
// Returns empty string if no default is defined.
func DefaultModelProviderEndpoint(providerType ModelProvider) string {
	switch providerType {
	case ModelProviderOpenAI:
		return "https://api.openai.com/v1"
	case ModelProviderAnthropic:
		return "https://api.anthropic.com"
	case ModelProviderGemini:
		return "https://generativelanguage.googleapis.com"
	case ModelProviderOllama:
		return "http://localhost:11434"
	default:
		// Azure, Bedrock, Vertex AI require user-specific endpoints
		return ""
	}
}

// SecretReference contains information to locate a secret.
type SecretReference struct {
	// Name is the name of the secret in the same namespace as the ModelProviderConfig
	// +required
	Name string `json:"name"`

	// Key is the key within the secret that contains the API key or credential.
	// If not specified and the secret contains exactly one key, that key will be used automatically.
	// +optional
	Key string `json:"key,omitempty"`
}

// ModelProviderConfigSpec defines the desired state of ModelProviderConfig.
//
// +kubebuilder:validation:XValidation:message="endpoint must be a valid URL starting with http:// or https://",rule="!has(self.endpoint) || size(self.endpoint) == 0 || self.endpoint.startsWith('http://') || self.endpoint.startsWith('https://')"
// +kubebuilder:validation:XValidation:message="secretRef is required for providers that need authentication (not Ollama)",rule="self.type == 'Ollama' || (has(self.secretRef) && has(self.secretRef.name) && size(self.secretRef.name) > 0)"
type ModelProviderConfigSpec struct {
	// Type is the model provider type (OpenAI, Anthropic, etc.)
	// +required
	// +kubebuilder:validation:Required
	Type ModelProvider `json:"type"`

	// Endpoint is the API endpoint URL for the provider.
	// If not specified, the default endpoint for the provider type will be used.
	// +optional
	// +kubebuilder:validation:Pattern=`^https?://.*`
	Endpoint string `json:"endpoint,omitempty"`

	// SecretRef references the Kubernetes Secret containing the API key.
	// Optional for providers that don't require authentication (e.g., local Ollama).
	// +optional
	SecretRef *SecretReference `json:"secretRef,omitempty"`
}

// GetEndpoint returns the endpoint, or the default endpoint if not specified.
func (p *ModelProviderConfigSpec) GetEndpoint() string {
	if p.Endpoint != "" {
		return p.Endpoint
	}
	return DefaultModelProviderEndpoint(p.Type)
}

// RequiresSecret returns true if this model provider type requires a secret for authentication.
func (p *ModelProviderConfigSpec) RequiresSecret() bool {
	return p.Type != ModelProviderOllama
}

// ModelProviderConfigStatus defines the observed state of ModelProviderConfig.
type ModelProviderConfigStatus struct {
	// ObservedGeneration reflects the generation of the most recently observed ModelProviderConfig spec
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// Conditions represent the latest available observations of the ModelProviderConfig's state
	// +optional
	// +listType=map
	// +listMapKey=type
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// DiscoveredModels is the cached list of model IDs available from this model provider
	// +optional
	DiscoveredModels []string `json:"discoveredModels,omitempty"`

	// ModelCount is the number of discovered models (for kubectl display)
	// +optional
	ModelCount int `json:"modelCount,omitempty"`

	// LastDiscoveryTime is the timestamp of the last successful model discovery
	// +optional
	LastDiscoveryTime *metav1.Time `json:"lastDiscoveryTime,omitempty"`

	// SecretHash is a hash of the referenced secret data, used to detect secret changes
	// +optional
	SecretHash string `json:"secretHash,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:categories=kagent,shortName=mprov
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type"
// +kubebuilder:printcolumn:name="Endpoint",type="string",JSONPath=".spec.endpoint"
// +kubebuilder:printcolumn:name="Models",type="integer",JSONPath=".status.modelCount"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// +kubebuilder:storageversion

// ModelProviderConfig is the Schema for the modelproviderconfigs API.
// It represents a model provider configuration with automatic model discovery.
type ModelProviderConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModelProviderConfigSpec   `json:"spec,omitempty"`
	Status ModelProviderConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ModelProviderConfigList contains a list of ModelProviderConfig.
type ModelProviderConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModelProviderConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModelProviderConfig{}, &ModelProviderConfigList{})
}
