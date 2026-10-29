package install

import (
	"github.com/kagent-dev/kagent/go/api/v1alpha3"
	"github.com/kagent-dev/kagent/go/core/pkg/env"
)

// ProviderAPIKeys returns registered credential names in priority order. The caller
// reads the environment; cloud credential providers have no API-key setting.
func ProviderAPIKeys(provider v1alpha3.ModelProvider) []env.StringVar {
	switch provider {
	case v1alpha3.ModelProviderOpenAI:
		return []env.StringVar{env.OpenAIAPIKey}
	case v1alpha3.ModelProviderAnthropic:
		return []env.StringVar{env.AnthropicAPIKey}
	case v1alpha3.ModelProviderAzureOpenAI:
		return []env.StringVar{env.AzureOpenAIAPIKey}
	case v1alpha3.ModelProviderOllama:
		return []env.StringVar{env.OllamaAPIKey}
	case v1alpha3.ModelProviderGemini:
		return []env.StringVar{env.GoogleAPIKey, env.GeminiAPIKey}
	default:
		return nil
	}
}
