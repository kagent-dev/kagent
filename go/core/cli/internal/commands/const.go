package commands

import (
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

const DefaultModelProvider = v1alpha3.ModelProviderOpenAI

// GetModelProvider returns the model provider from KAGENT_DEFAULT_MODEL_PROVIDER environment variable
func GetModelProvider() v1alpha3.ModelProvider {
	modelProvider := env.KagentDefaultModelProvider.Get()
	if modelProvider == "" || modelProvider == env.KagentDefaultModelProvider.DefaultValue() {
		return DefaultModelProvider
	}
	switch modelProvider {
	case GetModelProviderHelmValuesKey(v1alpha3.ModelProviderOpenAI):
		return v1alpha3.ModelProviderOpenAI
	case GetModelProviderHelmValuesKey(v1alpha3.ModelProviderOllama):
		return v1alpha3.ModelProviderOllama
	case GetModelProviderHelmValuesKey(v1alpha3.ModelProviderAnthropic):
		return v1alpha3.ModelProviderAnthropic
	case GetModelProviderHelmValuesKey(v1alpha3.ModelProviderAzureOpenAI):
		return v1alpha3.ModelProviderAzureOpenAI
	case GetModelProviderHelmValuesKey(v1alpha3.ModelProviderGemini):
		return v1alpha3.ModelProviderGemini
	case GetModelProviderHelmValuesKey(v1alpha3.ModelProviderGeminiVertexAI):
		return v1alpha3.ModelProviderGeminiVertexAI
	case GetModelProviderHelmValuesKey(v1alpha3.ModelProviderAnthropicVertexAI):
		return v1alpha3.ModelProviderAnthropicVertexAI
	case GetModelProviderHelmValuesKey(v1alpha3.ModelProviderBedrock):
		return v1alpha3.ModelProviderBedrock
	default:
		return v1alpha3.ModelProviderOpenAI
	}
}

// GetModelProviderHelmValuesKey returns the helm values key for the model provider with lowercased name
func GetModelProviderHelmValuesKey(provider v1alpha3.ModelProvider) string {
	helmKey := string(provider)
	if len(helmKey) > 0 {
		helmKey = strings.ToLower(string(provider[0])) + helmKey[1:]
	}
	return helmKey
}

// providerAPIKey selects the registered API key for a provider. Cloud credential
// providers have no API-key setting.
func providerAPIKey(provider v1alpha3.ModelProvider) (env.StringVar, bool) {
	switch provider {
	case v1alpha3.ModelProviderOpenAI:
		return env.OpenAIAPIKey, true
	case v1alpha3.ModelProviderAnthropic:
		return env.AnthropicAPIKey, true
	case v1alpha3.ModelProviderAzureOpenAI:
		return env.AzureOpenAIAPIKey, true
	case v1alpha3.ModelProviderOllama:
		return env.OllamaAPIKey, true
	case v1alpha3.ModelProviderGemini:
		if _, set := env.GoogleAPIKey.Lookup(); set {
			return env.GoogleAPIKey, true
		}
		return env.GeminiAPIKey, true
	default:
		return env.StringVar{}, false
	}
}
