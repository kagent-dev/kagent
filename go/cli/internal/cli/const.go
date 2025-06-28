package cli

import (
	"github.com/kagent-dev/kagent/go/controller/api/v1alpha1"
	"os"
	"strings"
)

const (
	// Version is the current version of the kagent CLI
	DefaultModelProvider   = v1alpha1.OpenAI
	DefaultHelmOciRegistry = "oci://ghcr.io/kagent-dev/kagent/helm/"

	//Provider specific env variables
	OPENAI_API_KEY      = "OPENAI_API_KEY"
	ANTHROPIC_API_KEY   = "ANTHROPIC_API_KEY"
	AZUREOPENAI_API_KEY = "AZUREOPENAI_API_KEY"

	// kagent env variables
	KAGENT_DEFAULT_MODEL_PROVIDER = "KAGENT_DEFAULT_MODEL_PROVIDER"
	KAGENT_HELM_REPO              = "KAGENT_HELM_REPO"
	KAGENT_HELM_VERSION           = "KAGENT_HELM_VERSION"
	KAGENT_HELM_EXTRA_ARGS        = "KAGENT_HELM_EXTRA_ARGS"
)

// GetModelProvider returns the model provider from KAGENT_DEFAULT_MODEL_PROVIDER environment variable
func GetModelProvider() v1alpha1.ModelProvider {
	modelProvider := os.Getenv(KAGENT_DEFAULT_MODEL_PROVIDER)
	if modelProvider == "" {

		return DefaultModelProvider
	}
	switch modelProvider {
	case GetModelProviderHelmValuesKey(v1alpha1.OpenAI):
		return v1alpha1.OpenAI
	case GetModelProviderHelmValuesKey(v1alpha1.Ollama):
		return v1alpha1.Ollama
	case GetModelProviderHelmValuesKey(v1alpha1.Anthropic):
		return v1alpha1.Anthropic
	case GetModelProviderHelmValuesKey(v1alpha1.AzureOpenAI):
		return v1alpha1.AzureOpenAI
	default:
		return v1alpha1.OpenAI
	}
}

// GetModelProviderHelmValuesKey returns the helm values key for the model provider with lowercased name
func GetModelProviderHelmValuesKey(provider v1alpha1.ModelProvider) string {
	helmKey := string(provider)
	if len(helmKey) > 0 {
		helmKey = strings.ToLower(string(provider[0])) + helmKey[1:]
	}
	return helmKey
}

// GetProviderAPIKey returns API_KEY env var name from provider type
func GetProviderAPIKey(provider v1alpha1.ModelProvider) string {
	switch provider {
	case v1alpha1.OpenAI:
		return OPENAI_API_KEY
	case v1alpha1.Anthropic:
		return ANTHROPIC_API_KEY
	case v1alpha1.AzureOpenAI:
		return AZUREOPENAI_API_KEY
	default:
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
