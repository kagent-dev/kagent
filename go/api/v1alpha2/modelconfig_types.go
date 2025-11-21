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
	ModelConfigConditionTypeAccepted = "Accepted"
)

// ModelProvider represents the model provider type
// +kubebuilder:validation:Enum=Anthropic;OpenAI;AzureOpenAI;Ollama;Gemini;GeminiVertexAI;AnthropicVertexAI
type ModelProvider string

const (
	ModelProviderAnthropic         ModelProvider = "Anthropic"
	ModelProviderAzureOpenAI       ModelProvider = "AzureOpenAI"
	ModelProviderOpenAI            ModelProvider = "OpenAI"
	ModelProviderOllama            ModelProvider = "Ollama"
	ModelProviderGemini            ModelProvider = "Gemini"
	ModelProviderGeminiVertexAI    ModelProvider = "GeminiVertexAI"
	ModelProviderAnthropicVertexAI ModelProvider = "AnthropicVertexAI"
)

type BaseVertexAIConfig struct {
	// The project ID
	// +required
	ProjectID string `json:"projectID"`

	// The project location
	// +required
	Location string `json:"location,omitempty"`

	// Temperature
	// +optional
	Temperature string `json:"temperature,omitempty"`

	// Top-p sampling parameter
	// +optional
	TopP string `json:"topP,omitempty"`

	// Top-k sampling parameter
	// +optional
	TopK string `json:"topK,omitempty"`

	// Stop sequences
	// +optional
	StopSequences []string `json:"stopSequences,omitempty"`
}

// GeminiVertexAIConfig contains Gemini Vertex AI-specific configuration options
type GeminiVertexAIConfig struct {
	BaseVertexAIConfig `json:",inline"`

	// Maximum output tokens
	// +optional
	MaxOutputTokens int `json:"maxOutputTokens,omitempty"`

	// Candidate count
	// +optional
	CandidateCount int `json:"candidateCount,omitempty"`

	// Response mime type
	// +optional
	ResponseMimeType string `json:"responseMimeType,omitempty"`
}

type AnthropicVertexAIConfig struct {
	BaseVertexAIConfig `json:",inline"`

	// Maximum tokens to generate
	// +optional
	MaxTokens int `json:"maxTokens,omitempty"`
}

// AnthropicConfig contains Anthropic-specific configuration options
type AnthropicConfig struct {
	// Base URL for the Anthropic API (overrides default)
	// +optional
	BaseURL string `json:"baseUrl,omitempty"`

	// Maximum tokens to generate
	// +optional
	MaxTokens int `json:"maxTokens,omitempty"`

	// Temperature for sampling
	// +optional
	Temperature string `json:"temperature,omitempty"`

	// Top-p sampling parameter
	// +optional
	TopP string `json:"topP,omitempty"`

	// Top-k sampling parameter
	// +optional
	TopK int `json:"topK,omitempty"`
}

// OpenAIConfig contains OpenAI-specific configuration options
type OpenAIConfig struct {
	// Base URL for the OpenAI API (overrides default)
	// +optional
	BaseURL string `json:"baseUrl,omitempty"`

	// Organization ID for the OpenAI API
	// +optional
	Organization string `json:"organization,omitempty"`

	// Temperature for sampling
	// +optional
	Temperature string `json:"temperature,omitempty"`

	// Maximum tokens to generate
	// +optional
	MaxTokens int `json:"maxTokens,omitempty"`

	// Top-p sampling parameter
	// +optional
	TopP string `json:"topP,omitempty"`

	// Frequency penalty
	// +optional
	FrequencyPenalty string `json:"frequencyPenalty,omitempty"`

	// Presence penalty
	// +optional
	PresencePenalty string `json:"presencePenalty,omitempty"`

	// Seed value
	// +optional
	Seed *int `json:"seed,omitempty"`

	// N value
	N *int `json:"n,omitempty"`

	// Timeout
	Timeout *int `json:"timeout,omitempty"`

	// Reasoning effort
	// +optional
	ReasoningEffort *OpenAIReasoningEffort `json:"reasoningEffort,omitempty"`
}

// OpenAIReasoningEffort represents how many reasoning tokens the model generates before producing a response.
// +kubebuilder:validation:Enum=minimal;low;medium;high
type OpenAIReasoningEffort string

// AzureOpenAIConfig contains Azure OpenAI-specific configuration options
type AzureOpenAIConfig struct {
	// Endpoint for the Azure OpenAI API
	// +required
	Endpoint string `json:"azureEndpoint,omitempty"`

	// API version for the Azure OpenAI API
	// +required
	APIVersion string `json:"apiVersion,omitempty"`

	// Deployment name for the Azure OpenAI API
	// +optional
	DeploymentName string `json:"azureDeployment,omitempty"`

	// Azure AD token for authentication
	// +optional
	AzureADToken string `json:"azureAdToken,omitempty"`

	// Azure AD token provider
	// +optional
	// TODO (peterj): We need to figure out how to implement this
	// AzureADTokenProvider interface{} `json:"azureAdTokenProvider,omitempty"`

	// Temperature for sampling
	// +optional
	Temperature string `json:"temperature,omitempty"`

	// Maximum tokens to generate
	// +optional
	MaxTokens *int `json:"maxTokens,omitempty"`

	// Top-p sampling parameter
	// +optional
	TopP string `json:"topP,omitempty"`
}

// OllamaConfig contains Ollama-specific configuration options
type OllamaConfig struct {
	// Host for the Ollama API
	// +optional
	Host string `json:"host,omitempty"`

	// Options for the Ollama API
	// +optional
	Options map[string]string `json:"options,omitempty"`
}

type GeminiConfig struct{}

// TLSConfig contains TLS/SSL configuration options for model provider connections.
// This enables agents to connect to internal LiteLLM gateways or other providers
// that use self-signed certificates or custom certificate authorities.
type TLSConfig struct {
	// DisableVerify disables SSL certificate verification entirely.
	// When false (default), SSL certificates are verified.
	// When true, SSL certificate verification is disabled.
	// WARNING: This should ONLY be used in development/testing environments.
	// Production deployments MUST use proper certificates.
	// +optional
	// +kubebuilder:default=false
	DisableVerify bool `json:"disableVerify,omitempty"`

	// CACertSecretRef is a reference to a Kubernetes Secret containing
	// CA certificate(s) in PEM format. The Secret must be in the same
	// namespace as the ModelConfig.
	// When set, the certificate will be used to verify the provider's SSL certificate.
	// This field follows the same pattern as APIKeySecret.
	// +optional
	CACertSecretRef string `json:"caCertSecretRef,omitempty"`

	// CACertSecretKey is the key within the Secret that contains the CA certificate data.
	// This field follows the same pattern as APIKeySecretKey.
	// Required when CACertSecretRef is set (unless DisableVerify is true).
	// +optional
	CACertSecretKey string `json:"caCertSecretKey,omitempty"`

	// DisableSystemCAs disables the use of system CA certificates.
	// When false (default), system CA certificates are used for verification (safe behavior).
	// When true, only the custom CA from CACertSecretRef is trusted.
	// This allows strict security policies where only corporate CAs should be trusted.
	// +optional
	// +kubebuilder:default=false
	DisableSystemCAs bool `json:"disableSystemCAs,omitempty"`
}

// ModelConfigSpec defines the desired state of ModelConfig.
//
// +kubebuilder:validation:XValidation:message="provider.openAI must be nil if the provider is not OpenAI",rule="!(has(self.openAI) && self.provider != 'OpenAI')"
// +kubebuilder:validation:XValidation:message="provider.anthropic must be nil if the provider is not Anthropic",rule="!(has(self.anthropic) && self.provider != 'Anthropic')"
// +kubebuilder:validation:XValidation:message="provider.azureOpenAI must be nil if the provider is not AzureOpenAI",rule="!(has(self.azureOpenAI) && self.provider != 'AzureOpenAI')"
// +kubebuilder:validation:XValidation:message="provider.ollama must be nil if the provider is not Ollama",rule="!(has(self.ollama) && self.provider != 'Ollama')"
// +kubebuilder:validation:XValidation:message="provider.gemini must be nil if the provider is not Gemini",rule="!(has(self.gemini) && self.provider != 'Gemini')"
// +kubebuilder:validation:XValidation:message="provider.geminiVertexAI must be nil if the provider is not GeminiVertexAI",rule="!(has(self.geminiVertexAI) && self.provider != 'GeminiVertexAI')"
// +kubebuilder:validation:XValidation:message="provider.anthropicVertexAI must be nil if the provider is not AnthropicVertexAI",rule="!(has(self.anthropicVertexAI) && self.provider != 'AnthropicVertexAI')"
// +kubebuilder:validation:XValidation:message="apiKeySecret must be set if apiKeySecretKey is set",rule="!(has(self.apiKeySecretKey) && !has(self.apiKeySecret))"
// +kubebuilder:validation:XValidation:message="apiKeySecretKey must be set if apiKeySecret is set",rule="!(has(self.apiKeySecret) && !has(self.apiKeySecretKey))"
// +kubebuilder:validation:XValidation:message="caCertSecretKey requires caCertSecretRef",rule="!(has(self.tls) && has(self.tls.caCertSecretKey) && size(self.tls.caCertSecretKey) > 0 && (!has(self.tls.caCertSecretRef) || size(self.tls.caCertSecretRef) == 0))"
// +kubebuilder:validation:XValidation:message="caCertSecretKey requires caCertSecretRef (unless disableVerify is true)",rule="!(has(self.tls) && (!has(self.tls.disableVerify) || !self.tls.disableVerify) && has(self.tls.caCertSecretKey) && size(self.tls.caCertSecretKey) > 0 && (!has(self.tls.caCertSecretRef) || size(self.tls.caCertSecretRef) == 0))"
// +kubebuilder:validation:XValidation:message="caCertSecretRef requires caCertSecretKey (unless disableVerify is true)",rule="!(has(self.tls) && (!has(self.tls.disableVerify) || !self.tls.disableVerify) && has(self.tls.caCertSecretRef) && size(self.tls.caCertSecretRef) > 0 && (!has(self.tls.caCertSecretKey) || size(self.tls.caCertSecretKey) == 0))"
type ModelConfigSpec struct {
	Model string `json:"model"`

	// The name of the secret that contains the API key. Must be a reference to the name of a secret in the same namespace as the referencing ModelConfig
	// +optional
	APIKeySecret string `json:"apiKeySecret"`

	// The key in the secret that contains the API key
	// +optional
	APIKeySecretKey string `json:"apiKeySecretKey"`

	// +optional
	DefaultHeaders map[string]string `json:"defaultHeaders,omitempty"`

	// The provider of the model
	// +kubebuilder:default=OpenAI
	Provider ModelProvider `json:"provider"`

	// OpenAI-specific configuration
	// +optional
	OpenAI *OpenAIConfig `json:"openAI,omitempty"`

	// Anthropic-specific configuration
	// +optional
	Anthropic *AnthropicConfig `json:"anthropic,omitempty"`

	// Azure OpenAI-specific configuration
	// +optional
	AzureOpenAI *AzureOpenAIConfig `json:"azureOpenAI,omitempty"`

	// Ollama-specific configuration
	// +optional
	Ollama *OllamaConfig `json:"ollama,omitempty"`

	// Gemini-specific configuration
	// +optional
	Gemini *GeminiConfig `json:"gemini,omitempty"`

	// Gemini Vertex AI-specific configuration
	// +optional
	GeminiVertexAI *GeminiVertexAIConfig `json:"geminiVertexAI,omitempty"`

	// Anthropic-specific configuration
	// +optional
	AnthropicVertexAI *AnthropicVertexAIConfig `json:"anthropicVertexAI,omitempty"`

	// TLS configuration for provider connections.
	// Enables agents to connect to internal LiteLLM gateways or other providers
	// that use self-signed certificates or custom certificate authorities.
	// +optional
	TLS *TLSConfig `json:"tls,omitempty"`
}

// ModelConfigStatus defines the observed state of ModelConfig.
type ModelConfigStatus struct {
	Conditions         []metav1.Condition `json:"conditions"`
	ObservedGeneration int64              `json:"observedGeneration"`
	// The secret hash stores a hash of any secrets required by the model config (i.e. api key, tls cert) to ensure agents referencing this model config detect changes to these secrets and restart if necessary.
	SecretHash string `json:"secretHash,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:categories=kagent,shortName=mc
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Provider",type="string",JSONPath=".spec.provider"
// +kubebuilder:printcolumn:name="Model",type="string",JSONPath=".spec.model"
// +kubebuilder:storageversion

// ModelConfig is the Schema for the modelconfigs API.
type ModelConfig struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ModelConfigSpec   `json:"spec,omitempty"`
	Status ModelConfigStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// ModelConfigList contains a list of ModelConfig.
type ModelConfigList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ModelConfig `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ModelConfig{}, &ModelConfigList{})
}
