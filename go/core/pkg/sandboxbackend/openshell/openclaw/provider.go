package openclaw

import (
	"fmt"
	"strings"

	"github.com/kagent-dev/kagent/go/api/v1alpha2"
)

func bootstrapProviderBaseURL(mc *v1alpha2.ModelConfig) string {
	switch mc.Spec.Provider {
	case v1alpha2.ModelProviderOpenAI:
		if mc.Spec.OpenAI != nil && strings.TrimSpace(mc.Spec.OpenAI.BaseURL) != "" {
			return strings.TrimSpace(mc.Spec.OpenAI.BaseURL)
		}
	case v1alpha2.ModelProviderAnthropic:
		if mc.Spec.Anthropic != nil && strings.TrimSpace(mc.Spec.Anthropic.BaseURL) != "" {
			return strings.TrimSpace(mc.Spec.Anthropic.BaseURL)
		}
	case v1alpha2.ModelProviderAzureOpenAI:
		if mc.Spec.AzureOpenAI != nil && strings.TrimSpace(mc.Spec.AzureOpenAI.Endpoint) != "" {
			return strings.TrimSpace(mc.Spec.AzureOpenAI.Endpoint)
		}
	case v1alpha2.ModelProviderOllama:
		if mc.Spec.Ollama != nil && strings.TrimSpace(mc.Spec.Ollama.Host) != "" {
			return strings.TrimSpace(mc.Spec.Ollama.Host)
		}
	case v1alpha2.ModelProviderSAPAICore:
		if mc.Spec.SAPAICore != nil && strings.TrimSpace(mc.Spec.SAPAICore.BaseURL) != "" {
			return strings.TrimSpace(mc.Spec.SAPAICore.BaseURL)
		}
	}
	return DefaultInferenceBaseURL
}

func providerAuth(mc *v1alpha2.ModelConfig) string {
	if mc.Spec.Provider == v1alpha2.ModelProviderBedrock {
		return "aws-sdk"
	}
	return "api-key"
}

func providerAPI(mc *v1alpha2.ModelConfig) (string, error) {
	switch mc.Spec.Provider {
	case v1alpha2.ModelProviderOpenAI:
		return "openai-completions", nil
	case v1alpha2.ModelProviderAnthropic:
		return "anthropic-messages", nil
	case v1alpha2.ModelProviderAzureOpenAI:
		return "azure-openai-responses", nil
	case v1alpha2.ModelProviderOllama:
		return "ollama", nil
	case v1alpha2.ModelProviderGemini, v1alpha2.ModelProviderGeminiVertexAI:
		return "google-generative-ai", nil
	case v1alpha2.ModelProviderAnthropicVertexAI:
		return "anthropic-messages", nil
	case v1alpha2.ModelProviderBedrock:
		return "bedrock-converse-stream", nil
	case v1alpha2.ModelProviderSAPAICore:
		return "", fmt.Errorf("model provider SAPAICore is not supported for OpenClaw sandbox JSON bootstrap")
	default:
		return "", fmt.Errorf("model provider %q is not supported for OpenClaw sandbox JSON bootstrap yet", mc.Spec.Provider)
	}
}
