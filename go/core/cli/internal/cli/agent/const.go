package cli

import (
	"os"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

const (
	DefaultModelProvider   = v1alpha2.ModelProviderOpenAI
	DefaultHelmOciRegistry = "oci://ghcr.io/kagent-dev/kagent/helm/"
)

// GetModelProvider returns the model provider from KAGENT_DEFAULT_MODEL_PROVIDER environment variable
func GetModelProvider() v1alpha2.ModelProvider {
	modelProvider := env.KagentDefaultModelProvider.Get()
	if modelProvider == "" || modelProvider == env.KagentDefaultModelProvider.DefaultValue() {
		return DefaultModelProvider
	}
	switch modelProvider {
	case GetModelProviderHelmValuesKey(v1alpha2.ModelProviderOpenAI):
		return v1alpha2.ModelProviderOpenAI
	case GetModelProviderHelmValuesKey(v1alpha2.ModelProviderOllama):
		return v1alpha2.ModelProviderOllama
	case GetModelProviderHelmValuesKey(v1alpha2.ModelProviderAnthropic):
		return v1alpha2.ModelProviderAnthropic
	case GetModelProviderHelmValuesKey(v1alpha2.ModelProviderAzureOpenAI):
		return v1alpha2.ModelProviderAzureOpenAI
	case GetModelProviderHelmValuesKey(v1alpha2.ModelProviderGemini):
		return v1alpha2.ModelProviderGemini
	case GetModelProviderHelmValuesKey(v1alpha2.ModelProviderGeminiVertexAI):
		return v1alpha2.ModelProviderGeminiVertexAI
	case GetModelProviderHelmValuesKey(v1alpha2.ModelProviderAnthropicVertexAI):
		return v1alpha2.ModelProviderAnthropicVertexAI
	case GetModelProviderHelmValuesKey(v1alpha2.ModelProviderBedrock):
		return v1alpha2.ModelProviderBedrock
	default:
		return v1alpha2.ModelProviderOpenAI
	}
}

// GetModelProviderHelmValuesKey returns the helm values key for the model provider with lowercased name
func GetModelProviderHelmValuesKey(provider v1alpha2.ModelProvider) string {
	helmKey := string(provider)
	if len(helmKey) > 0 {
		helmKey = strings.ToLower(string(provider[0])) + helmKey[1:]
	}
	return helmKey
}

// GetProviderAPIKey returns the env var name for the provider's API key.
// Returns "" for providers that use cloud credentials instead of an API key
// (Ollama, Bedrock, GeminiVertexAI, AnthropicVertexAI).
func GetProviderAPIKey(provider v1alpha2.ModelProvider) string {
	switch provider {
	case v1alpha2.ModelProviderOpenAI:
		return env.OpenAIAPIKey.Name()
	case v1alpha2.ModelProviderAnthropic:
		return env.AnthropicAPIKey.Name()
	case v1alpha2.ModelProviderAzureOpenAI:
		return env.AzureOpenAIAPIKey.Name()
	case v1alpha2.ModelProviderGemini:
		// Prefer GOOGLE_API_KEY, fall back to GEMINI_API_KEY to match the
		// runtime behaviour in go/adk/pkg/agent/agent.go.
		if _, ok := os.LookupEnv(env.GoogleAPIKey.Name()); ok {
			return env.GoogleAPIKey.Name()
		}
		return "GEMINI_API_KEY"
	default:
		// Ollama, Bedrock, GeminiVertexAI, AnthropicVertexAI use cloud
		// credentials rather than a simple API key, so no check is needed.
		return ""
	}
}

// GetEnvVarWithDefault returns the value of the environment variable if it exists, otherwise returns the default value
func GetEnvVarWithDefault(envVar, defaultValue string) string {
	if value, exists := os.LookupEnv(envVar); exists {
		return value
	}
	return defaultValue
}
