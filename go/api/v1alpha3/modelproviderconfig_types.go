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

package v1alpha3

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// ModelProviderConfigConditionTypeReady indicates whether the model provider config is ready for use
	ModelProviderConfigConditionTypeReady = "Ready"
)

// ModelProviderConfig condition reasons describe discovery and credential state.
const (
	ModelProviderConfigReasonPending          = "Pending"
	ModelProviderConfigReasonDiscovered       = "Discovered"
	ModelProviderConfigReasonSecretUnresolved = "SecretUnresolved"
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
	case ModelProviderMistral:
		return "https://api.mistral.ai/v1"
	default:
		// Azure, Bedrock, Vertex AI require user-specific endpoints
		return ""
	}
}

// SecretReference references a Kubernetes Secret that must contain exactly one data key
// holding the API key or credential.
type SecretReference struct {
	// Name is the name of the secret in the same namespace as the ModelProviderConfig.
	// +required
	// +kubebuilder:validation:MinLength=1
	Name string `json:"name"`
}

// ModelProviderConfigSpec defines the desired state of ModelProviderConfig.
//
// +kubebuilder:validation:XValidation:message="endpoint must be a valid URL starting with http:// or https://",rule="!has(self.endpoint) || size(self.endpoint) == 0 || self.endpoint.startsWith('http://') || self.endpoint.startsWith('https://')"
type ModelProviderConfigSpec struct {
	// Type is the model provider type (OpenAI, Anthropic, etc.)
	// +required
	Type ModelProvider `json:"type"`

	// Endpoint is the API endpoint URL for the provider.
	// If not specified, the default endpoint for the provider type will be used.
	// +optional
	// +kubebuilder:validation:Pattern=`^https?://[^/?#@]+(/[^?#]*)?$`
	Endpoint string `json:"endpoint,omitempty"`

	// SecretRef references the Kubernetes Secret containing the API key.
	// Omit for unauthenticated discovery. Invocation credentials are configured separately.
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

	// ModelCount is the number of discovered models (for kubectl display)
	// +optional
	ModelCount int `json:"modelCount,omitempty"`

	// LastDiscoveryTime is the timestamp of the last successful model discovery
	// +optional
	LastDiscoveryTime *metav1.Time `json:"lastDiscoveryTime,omitempty"`
}

// +genclient
// +kubebuilder:object:root=true
// +kubebuilder:storageversion
// +kubebuilder:resource:categories=kagent,shortName=mprov
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".spec.type"
// +kubebuilder:printcolumn:name="Endpoint",type="string",JSONPath=".spec.endpoint"
// +kubebuilder:printcolumn:name="Models",type="integer",JSONPath=".status.modelCount"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type=='Ready')].status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
// ModelProviderConfig is the Schema for the modelproviderconfigs API.
// It represents a model provider configuration with automatic model discovery.
type ModelProviderConfig struct {
	metav1.TypeMeta `json:",inline"`
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +optional
	Spec ModelProviderConfigSpec `json:"spec,omitempty"`
	// +optional
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
	SchemeBuilder.Register(func(s *runtime.Scheme) error {
		s.AddKnownTypes(GroupVersion, &ModelProviderConfig{}, &ModelProviderConfigList{})
		return nil
	})
}
