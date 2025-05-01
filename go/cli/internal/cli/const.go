package cli

import "os"

type ProviderType string

const (
	ProviderTypeOpenai      ProviderType = "openAI"
	ProviderTypeOllama      ProviderType = "ollama"
	ProviderTypeAnthropic   ProviderType = "anthropic"
	ProviderTypeAzureOpenAI ProviderType = "azureOpenAI"

	// Version is the current version of the kagent CLI
	DefaultModelProvider   = ProviderTypeOpenai
	DefaultHelmOciRegistry = "oci://ghcr.io/kagent-dev/kagent/helm/"

	//Provider specific env variables
	OPENAI_API_KEY    = "OPENAI_API_KEY"
	ANTHROPIC_API_KEY = "ANTHROPIC_API_KEY"
	AZURE_API_KEY     = "AZURE_API_KEY"

	// kagent env variables
	KAGENT_DEFAULT_MODEL_PROVIDER = "KAGENT_DEFAULT_MODEL_PROVIDER"
	KAGENT_HELM_REPO              = "KAGENT_HELM_REPO"
	KAGENT_HELM_VERSION           = "KAGENT_HELM_VERSION"
)

// GetModelProvider returns the model provider from KAGENT_DEFAULT_MODEL_PROVIDER environment variable
func GetModelProvider() ProviderType {
	modelProvider := os.Getenv(KAGENT_DEFAULT_MODEL_PROVIDER)
	if modelProvider == "" {
		return DefaultModelProvider
	}

	switch modelProvider {
	case string(ProviderTypeOpenai):
		return ProviderTypeOpenai
	case string(ProviderTypeOllama):
		return ProviderTypeOllama
	case string(ProviderTypeAnthropic):
		return ProviderTypeAnthropic
	case string(ProviderTypeAzureOpenAI):
		return ProviderTypeAzureOpenAI
	default:
		return DefaultModelProvider
	}
}

// GetProviderAPIKey returns API_KEY env var name from provider type
func GetProviderAPIKey(provider ProviderType) string {
	switch provider {
	case ProviderTypeOpenai:
		return OPENAI_API_KEY
	case ProviderTypeAnthropic:
		return ANTHROPIC_API_KEY
	case ProviderTypeAzureOpenAI:
		return AZURE_API_KEY
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
