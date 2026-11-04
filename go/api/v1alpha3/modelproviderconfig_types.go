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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
)

const (
	// ModelProviderConfigConditionTypeReady indicates whether the model provider config is ready for use
	ModelProviderConfigConditionTypeReady = "Ready"
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

// ModelProviderConfigSpec defines the desired state of ModelProviderConfig.
//
// +kubebuilder:validation:XValidation:message="endpoint must be a valid URL starting with http:// or https://",rule="!has(self.endpoint) || size(self.endpoint) == 0 || self.endpoint.startsWith('http://') || self.endpoint.startsWith('https://')"
type ModelProviderConfigSpec struct {
	// Type is the discovery protocol. OpenAI-compatible endpoints are supported.
	// +required
	// +kubebuilder:validation:XValidation:rule="self == 'OpenAI'",message="only OpenAI-compatible discovery is supported"
	Type ModelProvider `json:"type"`

	// Endpoint is the API endpoint URL for the provider.
	// If not specified, the default endpoint for the provider type will be used.
	// +optional
	// +kubebuilder:validation:Pattern=`^https?://[^/?#@]+(/[^?#]*)?$`
	Endpoint string `json:"endpoint,omitempty"`

	// DiscoveryAuth authenticates requests to list models. Omit for unauthenticated discovery.
	// These credentials are never used to invoke discovered models.
	// +optional
	DiscoveryAuth *ModelDiscoveryAuth `json:"discoveryAuth,omitempty"`

	// ModelConfigTemplate configures the ModelConfigs created for discovered models
	// in this ModelProviderConfig's namespace.
	// +optional
	ModelConfigTemplate *DiscoveredModelConfigTemplate `json:"modelConfigTemplate,omitempty"`
}

// ModelDiscoveryAuth supplies a Bearer token for discovery requests.
type ModelDiscoveryAuth struct {
	// SecretRef selects the discovery Bearer token from a Secret in the
	// ModelProviderConfig's namespace. The selected value must be nonempty.
	// +required
	// +kubebuilder:validation:XValidation:rule="has(self.name) && self.name != '' && self.key != ''",message="secretRef requires a name and key"
	// +kubebuilder:validation:XValidation:rule="!has(self.optional) || !self.optional",message="discovery credentials cannot be optional"
	SecretRef corev1.SecretKeySelector `json:"secretRef"`
}

// DiscoveredModelConfigTemplate supplies shared settings for discovered ModelConfigs.
type DiscoveredModelConfigTemplate struct {
	// Auth configures invocation credentials. Omit to add no explicit credentials.
	// Discovery credentials are not inherited.
	// +optional
	Auth *ModelInvocationAuth `json:"auth,omitempty"`

	// AllowedNamespaces controls which namespaces may reference discovered models
	// and their invocation credentials. Omitted policy permits only the same namespace.
	// +optional
	AllowedNamespaces *AllowedNamespaces `json:"allowedNamespaces,omitempty"`
}

// ModelInvocationAuth selects invocation credentials using ModelConfig auth semantics.
// Secret references resolve in the ModelProviderConfig and generated ModelConfig namespace.
//
// +kubebuilder:validation:AtMostOneOf=actorJwt;secretRef;tokenPassthrough
type ModelInvocationAuth struct {
	// ActorJWT authenticates as the executing Actor. Substrate mints and injects the token.
	// +optional
	ActorJWT *ActorJWT `json:"actorJwt,omitempty"`

	// SecretRef selects the invocation API key from a Secret in the
	// ModelProviderConfig and generated ModelConfig namespace.
	// +optional
	// +kubebuilder:validation:XValidation:rule="has(self.name) && self.name != '' && self.key != ''",message="secretRef requires a name and key"
	// +kubebuilder:validation:XValidation:rule="!has(self.optional) || !self.optional",message="invocation credentials cannot be optional"
	SecretRef *corev1.SecretKeySelector `json:"secretRef,omitempty"`

	// TokenPassthrough forwards the incoming A2A bearer token to the model provider.
	// +optional
	TokenPassthrough *bool `json:"tokenPassthrough,omitempty"`
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
