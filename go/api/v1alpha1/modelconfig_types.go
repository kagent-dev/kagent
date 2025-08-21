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

package v1alpha1

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
}

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

// ModelConfigSpec defines the desired state of ModelConfig.
//
// +kubebuilder:validation:XValidation:message="provider.openAI must be nil if the provider is not OpenAI",rule="!(has(self.openAI) && self.provider != 'OpenAI')"
// +kubebuilder:validation:XValidation:message="provider.anthropic must be nil if the provider is not Anthropic",rule="!(has(self.anthropic) && self.provider != 'Anthropic')"
// +kubebuilder:validation:XValidation:message="provider.azureOpenAI must be nil if the provider is not AzureOpenAI",rule="!(has(self.azureOpenAI) && self.provider != 'AzureOpenAI')"
// +kubebuilder:validation:XValidation:message="provider.ollama must be nil if the provider is not Ollama",rule="!(has(self.ollama) && self.provider != 'Ollama')"
// +kubebuilder:validation:XValidation:message="provider.gemini must be nil if the provider is not Gemini",rule="!(has(self.gemini) && self.provider != 'Gemini')"
// +kubebuilder:validation:XValidation:message="provider.geminiVertexAI must be nil if the provider is not GeminiVertexAI",rule="!(has(self.geminiVertexAI) && self.provider != 'GeminiVertexAI')"
// +kubebuilder:validation:XValidation:message="provider.anthropicVertexAI must be nil if the provider is not AnthropicVertexAI",rule="!(has(self.anthropicVertexAI) && self.provider != 'AnthropicVertexAI')"
type ModelConfigSpec struct {
	Model string `json:"model"`

	// The provider of the model
	// +kubebuilder:default=OpenAI
	Provider ModelProvider `json:"provider"`

	// The reference to the secret that contains the API key. Must be a reference to the name of a secret in the same namespace as the referencing ModelConfig
	// +optional
	APIKeySecretRef string `json:"apiKeySecretRef"`

	// The key in the secret that contains the API key
	// +optional
	APIKeySecretKey string `json:"apiKeySecretKey"`

	// +optional
	DefaultHeaders map[string]string `json:"defaultHeaders,omitempty"`

	// ModelInfo contains information about the model.
	// This field is required if the model is not one of the
	// pre-defined autogen models. That list can be found here:
	// +optional
	ModelInfo *ModelInfo `json:"modelInfo,omitempty"`

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
}

// Model Configurations
// This had to be created because the autogen_api.ModelInfo JSON tags are not
// compatible with the kubernetes api.
type ModelInfo struct {
	// +optional
	Vision bool `json:"vision"`
	// +optional
	FunctionCalling bool `json:"functionCalling"`
	// +optional
	JSONOutput bool `json:"jsonOutput"`
	// +optional
	Family string `json:"family"`
	// +optional
	StructuredOutput bool `json:"structuredOutput"`
	// +optional
	MultipleSystemMessages bool `json:"multipleSystemMessages"`
}

// ModelConfigStatus defines the observed state of ModelConfig.
type ModelConfigStatus struct {
	Conditions         []metav1.Condition `json:"conditions"`
	ObservedGeneration int64              `json:"observedGeneration"`
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:categories=kagent
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Provider",type="string",JSONPath=".spec.provider"
// +kubebuilder:printcolumn:name="Model",type="string",JSONPath=".spec.model"
// +kubebuilder:resource:shortName=mc

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
